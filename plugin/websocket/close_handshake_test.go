package websocket

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ws "github.com/gorilla/websocket"
	"github.com/micro/micro/v3/service/network/transport"
)

func TestDrainAfterPeerClosePreservesApplicationWriteFailures(t *testing.T) {
	for _, mode := range []string{"close_control_only", "accepted_application_frame", "broken_transport"} {
		t.Run(mode, func(t *testing.T) {
			ready := make(chan *Session, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, err := upgrader.Upgrade(w, r, nil)
				if err == nil {
					ready <- NewSession(c, "")
				}
			}))
			defer srv.Close()
			c, _, err := ws.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			s := <-ready
			defer s.Close()
			// Recv invokes Gorilla's real default close handler, which replies
			// with Close and makes subsequent writes return ErrCloseSent.
			if mode == "broken_transport" {
				if err = s.conn.UnderlyingConn().Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				if err = c.WriteControl(ws.CloseMessage, ws.FormatCloseMessage(ws.CloseNormalClosure, "done"), time.Now().Add(time.Second)); err != nil {
					t.Fatal(err)
				}
				if err = s.Recv(&transport.Message{}); !ws.IsCloseError(err, ws.CloseNormalClosure) {
					t.Fatalf("peer close: %v", err)
				}
			}
			if mode == "accepted_application_frame" {
				if err = s.Send(&transport.Message{Body: []byte("accepted-before-drain")}); err != nil {
					t.Fatalf("enqueue: %v", err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			err = s.drain(ctx)
			if mode == "accepted_application_frame" && !errors.Is(err, ws.ErrCloseSent) {
				t.Fatalf("application write failure was lost: %v", err)
			}
			if mode == "close_control_only" && err != nil {
				t.Fatalf("completed close handshake treated as failed flush: %v", err)
			}
			if mode == "broken_transport" && (err == nil || errors.Is(err, ws.ErrCloseSent)) {
				t.Fatalf("transport write failure was lost: %v", err)
			}
			select {
			case <-s.closeDone:
			case <-ctx.Done():
				t.Fatal("close callbacks not completed")
			}
		})
	}
}

func TestShutdownAcceptsCompletedPeerClose(t *testing.T) {
	var callbacks atomic.Int32
	callbackOK := make(chan bool, 1)
	h, _, c := shutdownFixture(t, func(context.Context, interface{}) error {
		t.Error("unexpected application RPC")
		return nil
	}, func(s *Session) {
		callbacks.Add(1)
		callbackOK <- GetSessionBySID(s.SID()) == nil
	})
	pong := make(chan struct{})
	c.SetPongHandler(func(string) error { close(pong); return nil })
	readDone := make(chan error, 1)
	go func() { _, _, err := c.ReadMessage(); readDone <- err }()
	if err := c.WriteControl(ws.PingMessage, nil, time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-pong:
	case <-time.After(time.Second):
		t.Fatal("server reader did not process ping")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.Shutdown(ctx) }()
	for !h.stopping() {
		select {
		case <-ctx.Done():
			t.Fatal("shutdown did not start")
		case <-time.After(time.Millisecond):
		}
	}
	if err := c.WriteControl(ws.CloseMessage, ws.FormatCloseMessage(ws.CloseNormalClosure, "done"), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("normal peer close failed shutdown: %v", err)
	}
	if err := <-readDone; !ws.IsCloseError(err, ws.CloseNormalClosure) {
		t.Fatalf("close handshake: %v", err)
	}
	if callbacks.Load() != 1 || !<-callbackOK {
		t.Fatal("close callback or SID cleanup changed")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cleanupFailures != 0 {
		t.Fatalf("cleanup failures = %d", h.cleanupFailures)
	}
}
