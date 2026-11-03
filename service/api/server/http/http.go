// Copyright 2020 Asim Aslam
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Original source: github.com/micro/go-micro/v3/api/server/http/http.go

// Package http provides a http server with features; acme, cors, etc
package http

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/handlers"
	"github.com/micro/micro/v3/service/api"
	"github.com/micro/micro/v3/service/logger"
)

type httpServer struct {
	mux  *http.ServeMux
	opts api.Options

	mtx         sync.RWMutex
	address     string
	server      *http.Server
	cancel      context.CancelFunc
	stopDone    chan struct{}
	stopErr     error
	active      sync.WaitGroup
	connections map[net.Conn]http.ConnState
}

type connectionKey struct{}

func NewServer(address string, opts ...api.Option) api.Server {
	var options api.Options
	for _, o := range opts {
		o(&options)
	}

	return &httpServer{
		opts:    options,
		mux:     http.NewServeMux(),
		address: address,
	}
}

func (s *httpServer) Address() string {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	return s.address
}

func (s *httpServer) Init(opts ...api.Option) error {
	for _, o := range opts {
		o(&s.opts)
	}
	return nil
}

func (s *httpServer) Handle(path string, handler http.Handler) {
	// TODO: move this stuff out to one place with ServeHTTP

	// apply the wrappers, e.g. auth
	for _, wrapper := range s.opts.Wrappers {
		handler = wrapper(handler)
	}

	// wrap with cors
	if s.opts.EnableCORS {
		handler = CombinedCORSHandler(handler)
	}

	// wrap with logger
	handler = handlers.CombinedLoggingHandler(os.Stdout, handler)

	s.mux.Handle(path, handler)
}

func (s *httpServer) Start() error {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if s.server != nil {
		return fmt.Errorf("HTTP server already started")
	}
	var l net.Listener
	var err error

	if s.opts.EnableACME && s.opts.ACMEProvider != nil {
		// should we check the address to make sure its using :443?
		l, err = s.opts.ACMEProvider.Listen(s.opts.ACMEHosts...)
	} else if s.opts.EnableTLS && s.opts.TLSConfig != nil {
		l, err = tls.Listen("tcp", s.address, s.opts.TLSConfig)
	} else {
		// otherwise plain listen
		l, err = net.Listen("tcp", s.address)
	}
	if err != nil {
		return err
	}

	if logger.V(logger.InfoLevel, logger.DefaultLogger) {
		logger.Infof("HTTP Server Listening on %s", l.Addr().String())
	}

	s.address = l.Addr().String()
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.connections = make(map[net.Conn]http.ConnState)
	s.server = &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.active.Add(1)
			defer s.active.Done()
			defer func() {
				// API websocket handlers own the connection until they return.
				// net/http does not send StateClosed after a hijack.
				c, _ := r.Context().Value(connectionKey{}).(net.Conn)
				s.mtx.Lock()
				if s.connections[c] == http.StateHijacked {
					delete(s.connections, c)
				}
				s.mtx.Unlock()
			}()
			s.mux.ServeHTTP(w, r)
		}),
		BaseContext: func(net.Listener) context.Context { return ctx },
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			return context.WithValue(ctx, connectionKey{}, c)
		},
		ConnState: func(c net.Conn, state http.ConnState) {
			s.mtx.Lock()
			defer s.mtx.Unlock()
			if state == http.StateClosed {
				delete(s.connections, c)
			} else {
				s.connections[c] = state
			}
		},
	}
	server := s.server

	go func() {
		if err := server.Serve(l); err != nil && err != http.ErrServerClosed {
			logger.Errorf("HTTP server serve failed: %v", err)
		}
	}()

	return nil
}

func (s *httpServer) Stop() error {
	s.mtx.Lock()
	if s.server == nil {
		s.mtx.Unlock()
		return nil
	}
	if s.stopDone != nil {
		done := s.stopDone
		s.mtx.Unlock()
		<-done
		return s.stopErr
	}
	s.stopDone = make(chan struct{})
	server, cancel := s.server, s.cancel
	timeout := s.opts.ShutdownTimeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	s.mtx.Unlock()

	logger.Infof("HTTP server drain started timeout=%s", timeout)
	ctx, stop := context.WithTimeout(context.Background(), timeout)
	defer stop()
	err := server.Shutdown(ctx)
	if err == nil {
		// Shutdown does not wait for hijacked connections. Their handlers still
		// belong to this server and share the same bounded drain deadline.
		done := make(chan struct{})
		go func() { s.active.Wait(); close(done) }()
		select {
		case <-done:
		case <-ctx.Done():
			err = ctx.Err()
		}
	}
	cancel()
	if err != nil {
		_ = server.Close()
		logger.Warnf("HTTP server drain timed out: %v", err)
	}
	// Close also leaves hijacked connections alone. Close them outside mtx.
	s.mtx.Lock()
	var hijacked []net.Conn
	for c, state := range s.connections {
		if state == http.StateHijacked {
			hijacked = append(hijacked, c)
		}
	}
	s.mtx.Unlock()
	for _, c := range hijacked {
		_ = c.Close()
	}
	s.mtx.Lock()
	s.stopErr = err
	close(s.stopDone)
	s.mtx.Unlock()
	if err == nil {
		logger.Info("HTTP server drain completed")
	}
	return err
}

func (s *httpServer) String() string {
	return "http"
}
