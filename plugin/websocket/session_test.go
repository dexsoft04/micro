package websocket

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ws "github.com/gorilla/websocket"
)

func TestSessionCloseIsConcurrentSafe(t *testing.T) {
	initialSessionCount := atomic.LoadInt64(&SessionCount)
	sessionCh := make(chan *Session, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		sessionCh <- NewSession(conn, "")
	}))
	defer server.Close()

	conn, _, err := ws.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	var session *Session
	select {
	case session = <-sessionCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for websocket session")
	}

	const callers = 32
	start := make(chan struct{})
	panicCh := make(chan interface{}, callers)
	var wg sync.WaitGroup
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			defer func() {
				if recovered := recover(); recovered != nil {
					panicCh <- recovered
				}
			}()
			<-start
			_ = session.Close()
		}()
	}
	close(start)
	wg.Wait()

	select {
	case recovered := <-panicCh:
		t.Fatalf("concurrent close panicked: %v", recovered)
	default:
	}
	if got := atomic.LoadInt64(&SessionCount); got != initialSessionCount {
		t.Fatalf("unexpected session count after close: got %d, want %d", got, initialSessionCount)
	}
	if got := GetSessionBySID(session.SID()); got != nil {
		t.Fatal("closed session is still registered")
	}
}
