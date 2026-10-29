package rpc_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	_ "github.com/micro/micro/v3/proto/errors"
	"github.com/micro/micro/v3/service/api/handler"
	"github.com/micro/micro/v3/service/api/handler/rpc"
	"github.com/micro/micro/v3/service/api/resolver"
	"github.com/micro/micro/v3/service/api/router"
	registryrouter "github.com/micro/micro/v3/service/api/router/registry"
	apiserver "github.com/micro/micro/v3/service/api/server"
	brokermemory "github.com/micro/micro/v3/service/broker/memory"
	"github.com/micro/micro/v3/service/client"
	grpcclient "github.com/micro/micro/v3/service/client/grpc"
	"github.com/micro/micro/v3/service/context/metadata"
	microerrors "github.com/micro/micro/v3/service/errors"
	"github.com/micro/micro/v3/service/registry"
	"github.com/micro/micro/v3/service/registry/memory"
	"github.com/micro/micro/v3/service/server"
	grpcserver "github.com/micro/micro/v3/service/server/grpc"
)

type SocketFixture struct {
	entered chan metadata.Metadata
	closed  chan struct{}
	mu      sync.Mutex
	frames  []int
}

func (f *SocketFixture) handle(ctx context.Context, stream server.Stream, welcome, ignoreInput, fail bool) error {
	md, _ := metadata.FromContext(ctx)
	f.entered <- md
	defer close(f.closed)
	if welcome {
		raw := json.RawMessage(`{"welcome":true}`)
		if err := stream.Send(&raw); err != nil {
			return err
		}
	}
	if ignoreInput {
		// A half-close is insufficient: the gateway must cancel the RPC context.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
			return microerrors.Timeout("fixture", "gateway did not cancel")
		}
	}
	if fail {
		for i := 0; i < 16; i++ {
			raw := json.RawMessage(`{"welcome":true}`)
			if err := stream.Send(&raw); err != nil {
				return err
			}
		}
		return microerrors.InternalServerError("fixture", "stream failed")
	}
	for {
		var frame struct {
			Seq int `json:"seq"`
		}
		if err := stream.Recv(&frame); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		f.mu.Lock()
		f.frames = append(f.frames, frame.Seq)
		f.mu.Unlock()
		if err := stream.Send(&frame); err != nil {
			return err
		}
	}
}
func (f *SocketFixture) Welcome(ctx context.Context, s server.Stream) error {
	return f.handle(ctx, s, true, false, false)
}
func (f *SocketFixture) Echo(ctx context.Context, s server.Stream) error {
	return f.handle(ctx, s, false, false, false)
}
func (f *SocketFixture) Wait(ctx context.Context, s server.Stream) error {
	return f.handle(ctx, s, false, true, false)
}
func (f *SocketFixture) Fail(ctx context.Context, s server.Stream) error {
	return f.handle(ctx, s, false, false, true)
}
func (f *SocketFixture) FiniteWelcome(ctx context.Context, s server.Stream) error {
	md, _ := metadata.FromContext(ctx)
	f.entered <- md
	defer close(f.closed)
	raw := json.RawMessage(`{"welcome":true}`)
	return s.Send(&raw)
}
func (f *SocketFixture) FiniteFrames(ctx context.Context, s server.Stream) error {
	md, _ := metadata.FromContext(ctx)
	f.entered <- md
	defer close(f.closed)
	for seq := 1; seq <= 64; seq++ {
		if err := s.Send(&struct {
			Seq int `json:"seq"`
		}{seq}); err != nil {
			return err
		}
	}
	return nil
}

func socketBackend(t *testing.T) (*SocketFixture, registry.Registry, client.Client) {
	t.Helper()
	f := &SocketFixture{entered: make(chan metadata.Metadata, 1), closed: make(chan struct{})}
	reg := memory.NewRegistry()
	s := grpcserver.NewServer(server.Name("fixture"), server.Address("127.0.0.1:0"), server.Registry(reg), server.Broker(brokermemory.NewBroker()))
	if err := s.Handle(s.NewHandler(f)); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Stop() })
	if err := s.(interface{ Register() error }).Register(); err != nil {
		t.Fatal(err)
	}
	return f, reg, grpcclient.NewClient(client.Retries(0), client.RequestTimeout(3*time.Second))
}

func socketGateway(t *testing.T, reg registry.Registry, c client.Client) (string, <-chan struct{}) {
	t.Helper()
	rt := registryrouter.NewRouter(router.WithRegistry(reg), router.WithHandler("rpc"), router.WithResolver(apiserver.NewResolver(resolver.WithHandler("rpc"))))
	t.Cleanup(func() { rt.Close() })
	h := rpc.NewHandler(handler.WithClient(c), handler.WithRouter(rt))
	returned := make(chan struct{}, 1)
	g := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { returned <- struct{}{} }()
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(g.Close)
	return "ws" + strings.TrimPrefix(g.URL, "http") + "/fixture/socketFixture/", returned
}

func socketDial(t *testing.T, base, method string) *websocket.Conn {
	t.Helper()
	c, _, err := (&websocket.Dialer{HandshakeTimeout: 2 * time.Second}).Dial(base+method,
		http.Header{"Content-Type": {"application/json"}, "Micro-Namespace": {"micro"}, "Authorization": {"Bearer synthetic-fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	c.SetWriteDeadline(time.Now().Add(3 * time.Second))
	return c
}

func awaitClosed(t *testing.T, ch <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not finish", name)
	}
}
func awaitEntry(t *testing.T, f *SocketFixture) {
	t.Helper()
	select {
	case md := <-f.entered:
		for _, key := range []string{"Connection", "Upgrade", "Sec-Websocket-Extensions", "Sec-Websocket-Version", "Sec-Websocket-Key"} {
			if value, _ := md.Get(key); value != "" {
				t.Fatalf("handshake metadata leaked: %s", key)
			}
		}
		if ns, _ := md.Get("Micro-Namespace"); ns != "micro" {
			t.Fatal("namespace metadata lost")
		}
		if auth, _ := md.Get("Authorization"); auth != "Bearer synthetic-fixture" {
			t.Fatal("authorization metadata lost")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("backend did not start before client frame")
	}
}

func TestWebsocketServerFirstAndExactFrames(t *testing.T) {
	f, reg, c := socketBackend(t)
	url, returned := socketGateway(t, reg, c)
	ws := socketDial(t, url, "welcome")
	// No client data has been sent; welcome must arrive from the backend first.
	typ, welcome, err := ws.ReadMessage()
	if err != nil || typ != websocket.TextMessage || string(welcome) != `{"welcome":true}` {
		t.Fatalf("server-first response: type=%d body=%q err=%v", typ, welcome, err)
	}
	awaitEntry(t, f)
	f.mu.Lock()
	before := len(f.frames)
	f.mu.Unlock()
	if before != 0 {
		t.Fatalf("gateway fabricated %d frames", before)
	}
	for _, seq := range []int{1, 2} {
		if err := ws.WriteJSON(map[string]int{"seq": seq}); err != nil {
			t.Fatal(err)
		}
		var response struct{ Seq int }
		if err := ws.ReadJSON(&response); err != nil || response.Seq != seq {
			t.Fatalf("echo sequence: %+v, error=%v", response, err)
		}
	}
	ws.Close()
	awaitClosed(t, f.closed, "backend")
	awaitClosed(t, returned, "gateway handler")
	f.mu.Lock()
	defer f.mu.Unlock()
	if !reflect.DeepEqual(f.frames, []int{1, 2}) {
		t.Fatalf("duplicated or changed client frames: %v", f.frames)
	}
}

func TestWebsocketCloseBeforeSendCancelsBackend(t *testing.T) {
	f, reg, c := socketBackend(t)
	url, returned := socketGateway(t, reg, c)
	ws := socketDial(t, url, "wait")
	awaitEntry(t, f)
	ws.Close()
	awaitClosed(t, f.closed, "backend cancellation")
	awaitClosed(t, returned, "gateway handler")
}

func TestWebsocketFiniteBackendDrainsBeforeNormalClose(t *testing.T) {
	for _, method := range []string{"finiteWelcome", "finiteFrames"} {
		t.Run(method, func(t *testing.T) {
			f, reg, c := socketBackend(t)
			url, returned := socketGateway(t, reg, c)
			ws := socketDial(t, url, method)
			awaitEntry(t, f)
			// Both streams are server-first: the client never sends a data frame.
			if method == "finiteWelcome" {
				typ, payload, err := ws.ReadMessage()
				if err != nil || typ != websocket.TextMessage || string(payload) != `{"welcome":true}` {
					t.Fatalf("finite welcome lost: type=%d body=%q err=%v", typ, payload, err)
				}
			} else {
				for seq := 1; seq <= 64; seq++ {
					var response struct{ Seq int }
					if err := ws.ReadJSON(&response); err != nil || response.Seq != seq {
						t.Fatalf("finite frame %d lost/reordered: %+v err=%v", seq, response, err)
					}
				}
			}
			if _, _, err := ws.ReadMessage(); !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				t.Fatalf("expected normal close after all frames, got %v", err)
			}
			awaitClosed(t, f.closed, "finite backend")
			awaitClosed(t, returned, "finite gateway handler")
		})
	}
}

func TestWebsocketBackendFailureStopsAllLoops(t *testing.T) {
	f, reg, c := socketBackend(t)
	url, returned := socketGateway(t, reg, c)
	ws := socketDial(t, url, "fail")
	awaitEntry(t, f)
	for i := 0; i < 32; i++ {
		_, _, err := ws.ReadMessage()
		if err != nil {
			if e, ok := err.(net.Error); ok && e.Timeout() {
				t.Fatal("backend failure left websocket open")
			}
			break
		}
	}
	awaitClosed(t, f.closed, "failed backend")
	awaitClosed(t, returned, "failed gateway handler")
}

type failedStreamClient struct {
	client.Client
	ctx chan context.Context
}

func (c *failedStreamClient) Stream(ctx context.Context, _ client.Request, _ ...client.CallOption) (client.Stream, error) {
	c.ctx <- ctx
	return nil, microerrors.InternalServerError("fixture", "stream creation failed")
}
func TestWebsocketStreamCreationFailureClosesConnection(t *testing.T) {
	_, reg, c := socketBackend(t)
	failed := &failedStreamClient{Client: c, ctx: make(chan context.Context, 1)}
	url, returned := socketGateway(t, reg, failed)
	ws := socketDial(t, url, "echo")
	if _, _, err := ws.ReadMessage(); err == nil {
		t.Fatal("failed stream left websocket readable")
	} else if e, ok := err.(net.Error); ok && e.Timeout() {
		t.Fatal("failed stream left websocket open")
	}
	awaitClosed(t, returned, "stream creation failure handler")
	select {
	case ctx := <-failed.ctx:
		awaitClosed(t, ctx.Done(), "failed stream context")
	default:
		t.Fatal("backend stream was not attempted")
	}
}
