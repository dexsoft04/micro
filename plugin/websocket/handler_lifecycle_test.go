package websocket

import (
	ws "github.com/gorilla/websocket"
	"github.com/micro/micro/v3/service/server"
	grpcserver "github.com/micro/micro/v3/service/server/grpc"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandlerOwnsConnectionLifetime(t *testing.T) {
	old := server.DefaultServer
	server.DefaultServer = grpcserver.NewServer()
	defer func() { server.DefaultServer = old }()
	returned := make(chan struct{})
	h := NewHandler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r); close(returned) }))
	defer srv.Close()
	c, _, err := ws.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	select {
	case <-returned:
		t.Fatal("handler returned while its websocket was still open")
	case <-time.After(30 * time.Millisecond):
	}
	c.Close()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("handler did not finish after close")
	}
}
