package websocket

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ws "github.com/gorilla/websocket"
	"github.com/micro/micro/v3/service/network/transport"
)

func TestSessionStatusUsesSnapshotAndDeletesEmptyValues(t *testing.T) {
	session := &Session{status: map[string]string{"Mcb-Openid": "player-1"}}
	snapshot := session.GetStatus()

	session.UpdateStatus(map[string]string{
		"Mcb-Openid":  "",
		"Mcb-Gamegid": "game-1",
	})

	if got := snapshot["Mcb-Openid"]; got != "player-1" {
		t.Fatalf("snapshot changed after update: got %q", got)
	}
	current := session.GetStatus()
	if _, ok := current["Mcb-Openid"]; ok {
		t.Fatal("empty status value was not deleted")
	}
	if got := current["Mcb-Gamegid"]; got != "game-1" {
		t.Fatalf("unexpected current status: %q", got)
	}
}

func TestSessionStatusConcurrentAccess(t *testing.T) {
	session := &Session{status: make(map[string]string)}
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				session.UpdateStatus(map[string]string{"status": "active"})
				for range session.GetStatus() {
				}
			}
		}()
	}
	wg.Wait()
}

func TestSessionSendWaitsForQueueAndStopsWhenClosed(t *testing.T) {
	session := &Session{
		send:   make(chan *transport.Message, 1),
		closed: make(chan bool),
	}
	session.send <- &transport.Message{}

	result := make(chan error, 1)
	go func() {
		result <- session.Send(&transport.Message{})
	}()
	select {
	case err := <-result:
		t.Fatalf("send returned while queue was full: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	<-session.send
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("unexpected send error after queue drained: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("send did not resume after queue drained")
	}

	go func() {
		result <- session.Send(&transport.Message{})
	}()
	select {
	case err := <-result:
		t.Fatalf("send returned while queue was full: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	session.isClosed.Store(true)
	close(session.closed)
	select {
	case err := <-result:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("unexpected closed session error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked send did not stop after session closed")
	}
}

func TestSessionReadLimit(t *testing.T) {
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

	session := <-sessionCh
	defer session.Close()
	errCh := make(chan error, 1)
	go func() {
		errCh <- session.Recv(&transport.Message{})
	}()
	if err := conn.WriteMessage(ws.BinaryMessage, make([]byte, int(defaultMaxMessageSize)+1)); err != nil {
		t.Fatalf("write oversized message: %v", err)
	}
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("oversized message was accepted")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for oversized message rejection")
	}
}

func TestSessionCloseIsConcurrentSafe(t *testing.T) {
	initialSessionCount := atomic.LoadInt64(&SessionCount)
	sessionCloseLocker.Lock()
	previousCallbacks := sessionCloseCallbacks
	sessionCloseCallbacks = nil
	sessionCloseLocker.Unlock()
	t.Cleanup(func() {
		sessionCloseLocker.Lock()
		sessionCloseCallbacks = previousCallbacks
		sessionCloseLocker.Unlock()
	})

	callbackState := make(chan bool, 1)
	callbackRelease := make(chan struct{})
	callbackDone := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-callbackRelease:
		default:
			close(callbackRelease)
		}
	})
	OnSessionClose(func(session *Session) {
		defer close(callbackDone)
		_, registered := sessionsBySID.Load(session.SID())
		callbackState <- session.isClosed.Load() && !registered
		<-callbackRelease
	})

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
	select {
	case closedBeforeCallback := <-callbackState:
		if !closedBeforeCallback {
			t.Fatal("close callback ran before the session was marked closed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for close callback")
	}
	close(callbackRelease)
	select {
	case <-callbackDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for close callback completion")
	}
}
