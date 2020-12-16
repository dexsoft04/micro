package http

import (
	"crypto/tls"
	"fmt"
	maddr "github.com/micro/micro/v3/internal/addr"
	"github.com/micro/micro/v3/internal/backoff"
	mnet "github.com/micro/micro/v3/internal/net"
	mls "github.com/micro/micro/v3/internal/tls"
	"github.com/micro/micro/v3/service/logger"
	"github.com/micro/micro/v3/service/registry"
	"github.com/micro/micro/v3/service/server"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type service struct {
	opts server.Options

	mux *http.ServeMux
	srv *registry.Service

	sync.RWMutex
	running bool
	static  bool
	exit    chan chan error
}

func (s *service) Init(opts ...server.Option) error {
	s.Lock()

	for _, o := range opts {
		o(&s.opts)
	}
	srv := s.genSrv()
	srv.Endpoints = s.srv.Endpoints
	s.srv = srv
	s.Unlock()

	return nil
}

// Options returns the options for the given service
func (s *service) Options() server.Options {
	return s.opts
}

func (s *service) Handle(handler server.Handler) error {
	panic("implement me")
}

func (s *service) NewHandler(i interface{}, option ...server.HandlerOption) server.Handler {
	panic("implement me")
}

func (s *service) NewSubscriber(s2 string, i interface{}, option ...server.SubscriberOption) server.Subscriber {
	panic("implement me")
}

func (s *service) Subscribe(subscriber server.Subscriber) error {
	panic("implement me")
}

func (s *service) Start() error {
	s.Lock()
	defer s.Unlock()

	if s.running {
		return nil
	}

	l, err := s.listen("tcp", s.opts.Address)
	if err != nil {
		return err
	}

	s.opts.Address = l.Addr().String()
	srv := s.genSrv()
	srv.Endpoints = s.srv.Endpoints
	s.srv = srv

	var h http.Handler

	h = s.mux
	var r sync.Once

	// register the html dir
	r.Do(func() {
		// static dir
		static, _ := s.opts.Context.Value(staticDirKey{}).(string)
		if static[0] != '/' {
			dir, _ := os.Getwd()
			static = filepath.Join(dir, static)
		}

		// set static if no / handler is registered
		if s.static {
			_, err := os.Stat(static)
			if err == nil {
				if logger.V(logger.InfoLevel, logger.DefaultLogger) {
					logger.Infof("Enabling static file serving from %s", static)
				}
				s.mux.Handle("/", http.FileServer(http.Dir(static)))
			}
		}
	})


	var httpSrv *http.Server

	httpSrv = &http.Server{}


	httpSrv.Handler = h

	go httpSrv.Serve(l)



	s.exit = make(chan chan error, 1)
	s.running = true

	go func() {
		ch := <-s.exit
		ch <- l.Close()
	}()

	if logger.V(logger.InfoLevel, logger.DefaultLogger) {
		logger.Infof("Listening on %v", l.Addr().String())
	}

	// announce self to the world
	if err := s.register(); err != nil {
		if logger.V(logger.ErrorLevel, logger.DefaultLogger) {
			logger.Errorf("Server register error: %v", err)
		}
	}

	go func() {
		t := new(time.Ticker)

		// only process if it exists
		if s.opts.RegisterInterval > time.Duration(0) {
			// new ticker
			t = time.NewTicker(s.opts.RegisterInterval)
		}

		// return error chan
		var ch chan error

	Loop:
		for {
			select {
			// register self on interval
			case <-t.C:
				if err := s.register(); err != nil {
					if logger.V(logger.ErrorLevel, logger.DefaultLogger) {
						logger.Error("Server register error: ", err)
					}
				}
			// wait for exit
			case ch = <-s.exit:
				break Loop
			}
		}

		// deregister self
		if err := s.deregister(); err != nil {
			if logger.V(logger.ErrorLevel, logger.DefaultLogger) {
				logger.Error("Server deregister error: ", err)
			}
		}

		// close transport
		ch <- nil

	}()

	return nil
}

func (s *service) Stop() error {
	s.Lock()
	defer s.Unlock()

	if !s.running {
		return nil
	}

	ch := make(chan error, 1)
	s.exit <- ch
	s.running = false

	if logger.V(logger.InfoLevel, logger.DefaultLogger) {
		logger.Info("Stopping")
	}


	return <-ch
}

func (s *service) String() string {
	return "http"
}

func newService(opts ...server.Option) server.Server {
	options := newOptions(opts...)
	s := &service{
		opts:   options,
		mux:    http.NewServeMux(),
		static: true,
	}
	s.srv = s.genSrv()
	return s
}

func (s *service) genSrv() *registry.Service {
	var host string
	var port string
	var err error

	// default host:port
	if len(s.opts.Address) > 0 {
		host, port, err = net.SplitHostPort(s.opts.Address)
		if err != nil {
			logger.Fatal(err)
		}
	}

	// check the advertise address first
	// if it exists then use it, otherwise
	// use the address
	if len(s.opts.Advertise) > 0 {
		host, port, err = net.SplitHostPort(s.opts.Advertise)
		if err != nil {
			logger.Fatal(err)
		}
	}

	addr, err := maddr.Extract(host)
	if err != nil {
		logger.Fatal(err)
	}

	if strings.Count(addr, ":") > 0 {
		addr = "[" + addr + "]"
	}

	return &registry.Service{
		Name:    s.opts.Name,
		Version: s.opts.Version,
		Nodes: []*registry.Node{{
			Id:       s.opts.Id,
			Address:  fmt.Sprintf("%s:%s", addr, port),
			Metadata: s.opts.Metadata,
		}},
	}
}

func (s *service) run(exit chan bool) {
	s.RLock()
	if s.opts.RegisterInterval <= time.Duration(0) {
		s.RUnlock()
		return
	}

	t := time.NewTicker(s.opts.RegisterInterval)
	s.RUnlock()

	for {
		select {
		case <-t.C:
			s.register()
		case <-exit:
			t.Stop()
			return
		}
	}
}

func (s *service) register() error {
	s.Lock()
	defer s.Unlock()

	if s.srv == nil {
		return nil
	}
	// default to service registry
	r := s.opts.Registry
	// switch to option if specified
	if r == nil {
		return nil
	}

	// service node need modify, node address maybe changed
	srv := s.genSrv()
	srv.Endpoints = s.srv.Endpoints
	s.srv = srv

	var regErr error

	// try three times if necessary
	for i := 0; i < 3; i++ {
		// attempt to register
		if err := r.Register(s.srv, registry.RegisterTTL(s.opts.RegisterTTL),
			registry.RegisterDomain(s.opts.Namespace)); err != nil {
			// set the error
			regErr = err
			// backoff then retry
			time.Sleep(backoff.Do(i + 1))
			continue
		}
		// success so nil error
		regErr = nil
		break
	}

	return regErr
}

func (s *service) deregister() error {
	s.Lock()
	defer s.Unlock()

	if s.srv == nil {
		return nil
	}
	// default to service registry
	r := s.opts.Registry
	// switch to option if specified
	if r == nil {
		return nil
	}
	return r.Deregister(s.srv, registry.DeregisterDomain(s.opts.Namespace))
}

func (s *service) stop() error {
	s.Lock()
	defer s.Unlock()

	if !s.running {
		return nil
	}


	ch := make(chan error, 1)
	s.exit <- ch
	s.running = false

	if logger.V(logger.InfoLevel, logger.DefaultLogger) {
		logger.Info("Stopping")
	}


	return <-ch
}


func (s *service) Handle(pattern string, handler http.Handler) {
	var seen bool
	s.RLock()
	for _, ep := range s.srv.Endpoints {
		if ep.Name == pattern {
			seen = true
			break
		}
	}
	s.RUnlock()

	// if its unseen then add an endpoint
	if !seen {
		s.Lock()
		s.srv.Endpoints = append(s.srv.Endpoints, &registry.Endpoint{
			Name: pattern,
		})
		s.Unlock()
	}

	// disable static serving
	if pattern == "/" {
		s.Lock()
		s.static = false
		s.Unlock()
	}

	// register the handler
	s.mux.Handle(pattern, handler)
}

func (s *service) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {

	var seen bool
	s.RLock()
	for _, ep := range s.srv.Endpoints {
		if ep.Name == pattern {
			seen = true
			break
		}
	}
	s.RUnlock()

	if !seen {
		s.Lock()
		s.srv.Endpoints = append(s.srv.Endpoints, &registry.Endpoint{
			Name: pattern,
		})
		s.Unlock()
	}

	// disable static serving
	if pattern == "/" {
		s.Lock()
		s.static = false
		s.Unlock()
	}

	s.mux.HandleFunc(pattern, handler)
}

func (s *service) listen(network, addr string) (net.Listener, error) {
	var l net.Listener
	var err error

	// TODO: support use of listen options
	if s.opts.TLSConfig != nil {
		config := s.opts.TLSConfig

		fn := func(addr string) (net.Listener, error) {
			if config == nil {
				hosts := []string{addr}

				// check if its a valid host:port
				if host, _, err := net.SplitHostPort(addr); err == nil {
					if len(host) == 0 {
						hosts = maddr.IPs()
					} else {
						hosts = []string{host}
					}
				}

				// generate a certificate
				cert, err := mls.Certificate(hosts...)
				if err != nil {
					return nil, err
				}
				config = &tls.Config{Certificates: []tls.Certificate{cert}}
			}
			return tls.Listen(network, addr, config)
		}

		l, err = mnet.Listen(addr, fn)
	} else {
		fn := func(addr string) (net.Listener, error) {
			return net.Listen(network, addr)
		}

		l, err = mnet.Listen(addr, fn)
	}

	if err != nil {
		return nil, err
	}

	return l, nil
}
