package web

import (
	"crypto/tls"
	"fmt"
	micro "github.com/micro/micro/v3/service"
	"github.com/micro/micro/v3/service/logger"
	"github.com/micro/micro/v3/service/registry"
	maddr "github.com/micro/micro/v3/util/addr"
	"github.com/micro/micro/v3/util/backoff"
	mnet "github.com/micro/micro/v3/util/net"
	mls "github.com/micro/micro/v3/util/tls"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type service struct {
	opts Options

	mux *http.ServeMux
	srv *registry.Service

	sync.RWMutex
	running bool
	static  bool
	exit    chan chan error
}

func newService(opts ...Option) Service {
	options := newOptions(opts...)
	s := &service{
		opts:   options,
		mux:    http.NewServeMux(),
		static: true,
	}
	s.srv = s.genSrv()

	s.Init(opts...)

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
	r := registry.DefaultRegistry
	// switch to option if specified
	if s.opts.Registry != nil {
		r = s.opts.Registry
	}

	// service node need modify, node address maybe changed
	srv := s.genSrv()
	srv.Endpoints = s.srv.Endpoints
	s.srv = srv

	// use RegisterCheck func before register
	if err := s.opts.RegisterCheck(s.opts.Context); err != nil {
		if logger.V(logger.ErrorLevel, logger.DefaultLogger) {
			logger.Errorf("Server %s-%s register check error: %s", s.opts.Name, s.opts.Id, err)
		}
		return err
	}

	var regErr error

	// try three times if necessary
	for i := 0; i < 3; i++ {
		// attempt to register
		if err := r.Register(s.srv, registry.RegisterTTL(s.opts.RegisterTTL)); err != nil {
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
	r := registry.DefaultRegistry
	// switch to option if specified
	if s.opts.Registry != nil {
		r = s.opts.Registry
	}
	return r.Deregister(s.srv)
}

func (s *service) start() error {
	s.Lock()
	defer s.Unlock()

	if s.running {
		return nil
	}

	for _, fn := range s.opts.BeforeStart {
		if err := fn(); err != nil {
			return err
		}
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

	if s.opts.Handler != nil {
		h = s.opts.Handler
	} else {
		h = s.mux
		var r sync.Once

		// register the html dir
		r.Do(func() {
			// static dir
			static := s.opts.StaticDir
			if s.opts.StaticDir[0] != '/' {
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
	}

	var httpSrv *http.Server
	if s.opts.Server != nil {
		httpSrv = s.opts.Server
	} else {
		httpSrv = &http.Server{}
	}

	httpSrv.Handler = h

	go httpSrv.Serve(l)

	for _, fn := range s.opts.AfterStart {
		if err := fn(); err != nil {
			return err
		}
	}

	s.exit = make(chan chan error, 1)
	s.running = true

	go func() {
		ch := <-s.exit
		ch <- l.Close()
	}()

	if logger.V(logger.InfoLevel, logger.DefaultLogger) {
		logger.Infof("Listening on %v", l.Addr().String())
	}
	return nil
}

func (s *service) stop() error {
	s.Lock()
	defer s.Unlock()

	if !s.running {
		return nil
	}

	for _, fn := range s.opts.BeforeStop {
		if err := fn(); err != nil {
			return err
		}
	}

	ch := make(chan error, 1)
	s.exit <- ch
	s.running = false

	if logger.V(logger.InfoLevel, logger.DefaultLogger) {
		logger.Info("Stopping")
	}

	for _, fn := range s.opts.AfterStop {
		if err := fn(); err != nil {
			if chErr := <-ch; chErr != nil {
				return chErr
			}
			return err
		}
	}

	return <-ch
}

func (s *service) Client() *http.Client {
	return nil
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

func (s *service) Init(opts ...Option) error {
	s.Lock()

	for _, o := range opts {
		o(&s.opts)
	}

	if s.opts.Service == nil {
		s.opts.Service = micro.New(micro.Name(s.opts.Name))
	}

	srv := s.genSrv()
	srv.Endpoints = s.srv.Endpoints
	s.srv = srv
	s.Unlock()

	return nil
}

func (s *service) Run() error {
	// generate an auth account
	//srvID := s.opts.Service.Server().Options().Id
	//srvName := s.Options().Name
	//if err := authutil.Generate(srvID, srvName, s.opts.Service.Options().Auth); err != nil {
	//	return err
	//}

	if err := s.start(); err != nil {
		return err
	}

	if err := s.register(); err != nil {
		return err
	}

	// start reg loop
	ex := make(chan bool)
	go s.run(ex)

	ch := make(chan os.Signal, 1)
	if s.opts.Signal {
		signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGKILL)
	}

	select {
	// wait on kill signal
	case sig := <-ch:
		if logger.V(logger.InfoLevel, logger.DefaultLogger) {
			logger.Infof("Received signal %s", sig)
		}
	// wait on context cancel
	case <-s.opts.Context.Done():
		if logger.V(logger.InfoLevel, logger.DefaultLogger) {
			logger.Info("Received context shutdown")
		}
	}

	// exit reg loop
	close(ex)

	if err := s.deregister(); err != nil {
		return err
	}

	return s.stop()
}

// Options returns the options for the given service
func (s *service) Options() Options {
	return s.opts
}

func (s *service) listen(network, addr string) (net.Listener, error) {
	var l net.Listener
	var err error

	// TODO: support use of listen options
	if s.opts.Secure || s.opts.TLSConfig != nil {
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
