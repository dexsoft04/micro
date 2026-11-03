package http

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/micro/micro/v3/service/api"
)

// A successful Stop must not let the caller exit while a request is active.
func TestStopWaitsForInflight(t *testing.T) {
	s := NewServer("127.0.0.1:0")
	entered, release := make(chan struct{}), make(chan struct{})
	s.Handle("/slow", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		w.Write([]byte("committed-once"))
	}))
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	result := make(chan error, 1)
	go func() {
		r, err := client.Get("http://" + s.Address() + "/slow")
		if err == nil {
			defer r.Body.Close()
			var body []byte
			body, err = io.ReadAll(r.Body)
			if string(body) != "committed-once" {
				t.Errorf("incomplete response: %q", body)
			}
		}
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not enter")
	}
	stopped := make(chan error, 1)
	go func() { stopped <- s.Stop() }()
	early := false
	select {
	case err := <-stopped:
		early = true
		t.Errorf("Stop returned before in-flight request completed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if !early {
		select {
		case err := <-stopped:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("Stop did not finish")
		}
	}
}

func TestStopDeadlineCancelsStream(t *testing.T) {
	s := NewServer("127.0.0.1:0", api.ShutdownTimeout(100*time.Millisecond))
	entered, canceled := make(chan struct{}), make(chan struct{})
	s.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("first\n"))
		w.(http.Flusher).Flush()
		close(entered)
		<-r.Context().Done()
		close(canceled)
	}))
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	r, err := http.Get("http://" + s.Address())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	<-entered
	start := time.Now()
	if err := s.Stop(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("unbounded stop")
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("stream context not canceled")
	}
	if err := s.Stop(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("repeated Stop: %v", err)
	}
}

func TestStopWebsocket(t *testing.T) {
	for _, finite := range []bool{true, false} {
		name := "deadline"
		if finite {
			name = "finite"
		}
		t.Run(name, func(t *testing.T) {
			s := NewServer("127.0.0.1:0", api.ShutdownTimeout(300*time.Millisecond)).(*httpServer)
			entered, release, completed := make(chan struct{}), make(chan struct{}), make(chan struct{})
			s.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				u := websocket.Upgrader{}
				c, err := u.Upgrade(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer close(completed)
				defer c.Close()
				close(entered)
				if finite {
					<-release
					c.WriteMessage(websocket.TextMessage, []byte("last"))
				} else {
					c.ReadMessage()
				}
			}))
			if err := s.Start(); err != nil {
				t.Fatal(err)
			}
			c, _, err := websocket.DefaultDialer.Dial("ws://"+s.Address(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			<-entered
			stopped := make(chan error, 1)
			go func() { stopped <- s.Stop() }()
			select {
			case err := <-stopped:
				t.Fatalf("early stop: %v", err)
			case <-time.After(50 * time.Millisecond):
			}
			if finite {
				close(release)
				_, b, err := c.ReadMessage()
				if err != nil || string(b) != "last" {
					t.Fatalf("lost final message: %q %v", b, err)
				}
			}
			select {
			case err := <-stopped:
				if finite && err != nil || !finite && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("Stop: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("unbounded websocket stop")
			}
			select {
			case <-completed:
			case <-time.After(time.Second):
				t.Fatal("websocket not closed")
			}
			s.mtx.RLock()
			remaining := len(s.connections)
			s.mtx.RUnlock()
			// The outer handler finishes immediately after completed is closed.
			if remaining > 1 {
				t.Fatalf("retained connections: %d", remaining)
			}
		})
	}
}

func TestStopConcurrentAndTLS(t *testing.T) {
	certServer := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	certs := certServer.TLS.Certificates
	certServer.Close()
	s := NewServer("127.0.0.1:0", api.EnableTLS(true), api.TLSConfig(&tls.Config{Certificates: certs}))
	s.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil {
			t.Error("TLS state missing")
		}
		io.WriteString(w, "ok")
	}))
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} // Local test certificate only.
	defer tr.CloseIdleConnections()
	c := &http.Client{Transport: tr, Timeout: time.Second}
	for i := 0; i < 3; i++ {
		r, err := c.Get("https://" + s.Address())
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(r.Body)
		r.Body.Close()
		if err != nil || strings.TrimSpace(string(b)) != "ok" {
			t.Fatalf("response: %q %v", b, err)
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Stop(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if r, err := c.Get("https://" + s.Address()); err == nil {
		r.Body.Close()
		t.Fatal("accepted request after Stop")
	}
}
