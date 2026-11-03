package websocket

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	ws "github.com/gorilla/websocket"
	pb "github.com/micro/micro/v3/proto/transport"
	"github.com/micro/micro/v3/service/client"
	"github.com/micro/micro/v3/service/context/metadata"
	"github.com/micro/micro/v3/service/network/transport"
	"github.com/micro/micro/v3/service/server"
	grpcserver "github.com/micro/micro/v3/service/server/grpc"
	"github.com/micro/micro/v3/util/codec/bytes"
)

type shutdownClient struct {
	testClient
	call func(context.Context, interface{}) error
}

func (c *shutdownClient) Call(ctx context.Context, _ client.Request, rsp interface{}, _ ...client.CallOption) error {
	return c.call(ctx, rsp)
}

func shutdownFixture(t *testing.T, call func(context.Context, interface{}) error, callback func(*Session)) (*wsHandler, *httptest.Server, *ws.Conn) {
	t.Helper()
	old := client.DefaultClient
	oldServer := server.DefaultServer
	server.DefaultServer = grpcserver.NewServer()
	client.DefaultClient = &shutdownClient{call: call}
	sessionCloseLocker.Lock()
	callbacks := sessionCloseCallbacks
	sessionCloseCallbacks = nil
	sessionCloseLocker.Unlock()
	if callback != nil {
		OnSessionClose(callback)
	}
	h := NewHandler().(*wsHandler)
	srv := httptest.NewServer(h)
	conn, _, err := ws.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		conn.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = h.Shutdown(ctx)
		srv.Close()
		client.DefaultClient = old
		server.DefaultServer = oldServer
		sessionCloseLocker.Lock()
		sessionCloseCallbacks = callbacks
		sessionCloseLocker.Unlock()
	})
	return h, srv, conn
}

func sendShutdownRequest(t *testing.T, c *ws.Conn) {
	t.Helper()
	b, err := proto.Marshal(&pb.Message{Header: map[string]string{"Micro-Service": "game", "Micro-Endpoint": "Game.Slow", "Micro-Id": "drain-1", "Content-Type": "application/protobuf"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = c.WriteMessage(ws.BinaryMessage, b); err != nil {
		t.Fatal(err)
	}
}

func TestShutdownCompletesReplyBeforeCloseAndWaitsCallbacks(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	callbackEntered, callbackRelease := make(chan struct{}), make(chan struct{})
	var sid string
	h, srv, c := shutdownFixture(t, func(ctx context.Context, rsp interface{}) error {
		sid, _ = metadata.Get(ctx, "micro-ws-session-id")
		close(entered)
		<-release
		rsp.(*bytes.Frame).Data = []byte("committed-once")
		return nil
	}, func(s *Session) {
		if s.SID() == sid {
			close(callbackEntered)
			<-callbackRelease
		}
	})
	sendShutdownRequest(t, c)
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.Shutdown(ctx) }()
	for !h.stopping() {
		time.Sleep(time.Millisecond)
	}
	other, rsp, err := ws.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if other != nil {
		other.Close()
	}
	if err == nil || rsp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("new connection accepted during drain: %v", err)
	}
	close(release)
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, b, err := c.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var reply pb.Message
	if err = proto.Unmarshal(b, &reply); err != nil {
		t.Fatal(err)
	}
	if string(reply.Body) != "committed-once" || reply.Header["Micro-Id"] != "drain-1" {
		t.Fatalf("lost final reply: %v", reply)
	}
	_, _, err = c.ReadMessage()
	if !ws.IsCloseError(err, ws.CloseGoingAway) {
		t.Fatalf("missing graceful close: %v", err)
	}
	<-callbackEntered
	select {
	case err := <-done:
		t.Fatalf("shutdown returned before callback: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(callbackRelease)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if sid == "" || GetSessionBySID(sid) != nil {
		t.Fatal("session was not removed")
	}
	// A fresh gateway accepts a new session after the old gateway retires.
	nextHandler := NewHandler()
	next := httptest.NewServer(nextHandler)
	defer next.Close()
	reconnected, _, err := ws.DefaultDialer.Dial("ws"+strings.TrimPrefix(next.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	reconnected.Close()
	if err = nextHandler.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestShutdownDeadlineCancelsInflightRPC(t *testing.T) {
	entered, canceled := make(chan struct{}), make(chan struct{})
	var closed atomic.Int32
	h, _, c := shutdownFixture(t, func(ctx context.Context, _ interface{}) error {
		close(entered)
		<-ctx.Done()
		close(canceled)
		return ctx.Err()
	}, func(*Session) { closed.Add(1) })
	sendShutdownRequest(t, c)
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := h.Shutdown(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unexpected shutdown result: %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("cooperative RPC did not finish promptly")
	}
	select {
	case <-canceled:
	default:
		t.Fatal("RPC context was not canceled")
	}
	if closed.Load() != 1 {
		t.Fatalf("close callback count=%d", closed.Load())
	}
}

func TestSessionDrainFlushesQueuedFrames(t *testing.T) {
	ready := make(chan *Session, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		ready <- NewSession(c, "")
	}))
	defer srv.Close()
	c, _, err := ws.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s := <-ready
	for i := 0; i < 100; i++ {
		if err = s.Send(&transport.Message{Body: []byte{byte(i)}}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.drain(ctx) }()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	for i := 0; i < 100; i++ {
		_, b, err := c.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		var m pb.Message
		if err = proto.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		if len(m.Body) != 1 || m.Body[0] != byte(i) {
			t.Fatalf("frame %d lost or reordered", i)
		}
	}
	_, _, err = c.ReadMessage()
	if !ws.IsCloseError(err, ws.CloseGoingAway) {
		t.Fatalf("close frame: %v", err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if err = s.Send(&transport.Message{}); err == nil {
		t.Fatal("send accepted after drain")
	}
	<-s.closeDone
}

func TestShutdownReportsBlockedCloseCallback(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	callbackEntered, callbackRelease := make(chan struct{}), make(chan struct{})
	h, _, c := shutdownFixture(t, func(_ context.Context, rsp interface{}) error {
		close(entered)
		<-release
		rsp.(*bytes.Frame).Data = []byte("ok")
		return nil
	}, func(*Session) { close(callbackEntered); <-callbackRelease })
	sendShutdownRequest(t, c)
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.Shutdown(ctx) }()
	for !h.stopping() {
		time.Sleep(time.Millisecond)
	}
	close(release)
	<-callbackEntered
	select {
	case err := <-done:
		close(callbackRelease)
		if err == nil {
			t.Fatal("incomplete close callback reported as successful shutdown")
		}
	case <-ctx.Done():
		close(callbackRelease)
		t.Fatal("callback wait exceeded cleanup budget")
	}
}

func TestConcurrentShutdownAndHandshake(t *testing.T) {
	h, srv, c := shutdownFixture(t, func(context.Context, interface{}) error { return nil }, nil)
	c.Close()
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, _, err := ws.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
			if err == nil {
				conn.Close()
			}
		}()
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := h.Shutdown(ctx); err != nil {
				t.Errorf("concurrent shutdown: %v", err)
			}
		}()
	}
	wg.Wait()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.sessions) != 0 {
		t.Fatalf("sessions leaked: %d", len(h.sessions))
	}
}

func TestShutdownSlowQueueUsesRemainingDrainBudget(t *testing.T) {
	entered := make(chan *Session, 1)
	release := make(chan struct{})
	h, _, c := shutdownFixture(t, func(ctx context.Context, rsp interface{}) error {
		sid, _ := metadata.Get(ctx, "micro-ws-session-id")
		entered <- GetSessionBySID(sid)
		<-release
		rsp.(*bytes.Frame).Data = []byte("final-rpc-response")
		return nil
	}, nil)
	sendShutdownRequest(t, c)
	s := <-entered
	if err := c.UnderlyingConn().(*net.TCPConn).SetReadBuffer(64 * 1024); err != nil {
		t.Fatal(err)
	}
	if err := s.conn.UnderlyingConn().(*net.TCPConn).SetWriteBuffer(16 * 1024); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		body := make([]byte, 512*1024)
		body[0] = byte(i)
		if err := s.Send(&transport.Message{Body: body}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	done := make(chan error, 1)
	started := time.Now()
	go func() { done <- h.Shutdown(ctx) }()
	for !h.stopping() {
		time.Sleep(time.Millisecond)
	}
	close(release)
	c.SetReadDeadline(time.Now().Add(14 * time.Second))
	for i := 0; i < 20; i++ {
		_, b, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("queued frame %d: %v", i, err)
		}
		var m pb.Message
		if err = proto.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		if len(m.Body) != 512*1024 || m.Body[0] != byte(i) {
			t.Fatalf("queue ordering at frame %d", i)
		}
		time.Sleep(300 * time.Millisecond)
	}
	_, b, err := c.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var m pb.Message
	if err = proto.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if string(m.Body) != "final-rpc-response" {
		t.Fatal("final RPC response lost behind queue")
	}
	_, _, err = c.ReadMessage()
	if !ws.IsCloseError(err, ws.CloseGoingAway) {
		t.Fatalf("close frame: %v", err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if time.Since(started) < 5*time.Second {
		t.Fatal("fixture did not exercise more than five seconds")
	}
}
