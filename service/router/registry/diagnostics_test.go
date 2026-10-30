package registry

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/micro/micro/v3/service/context/metadata"
	"github.com/micro/micro/v3/service/logger"
	"github.com/micro/micro/v3/service/registry"
	"github.com/micro/micro/v3/service/router"
)

type diagnosticRegistry struct {
	registry.Registry
	services []*registry.Service
	err      error
	calls    int
}

func (r *diagnosticRegistry) GetService(string, ...registry.GetOption) ([]*registry.Service, error) {
	r.calls++
	return r.services, r.err
}

func TestLookupMissDiagnosticsPreserveRouting(t *testing.T) {
	for _, source := range []string{"cache_filter", "registry_filter", "registry_not_found"} {
		t.Run(source, func(t *testing.T) {
			var out bytes.Buffer
			previous := logger.DefaultLogger
			logger.DefaultLogger = logger.NewLogger(logger.WithOutput(&out), logger.WithLevel(logger.WarnLevel))
			t.Cleanup(func() { logger.DefaultLogger = previous })
			reg := &diagnosticRegistry{services: []*registry.Service{{
				Name: "websocket", Metadata: map[string]string{"domain": "micro"},
				Nodes: []*registry.Node{{Id: "other", Address: "127.0.0.1:9000", Metadata: map[string]string{"credential": "private-value"}}},
			}}}
			r := NewRouter(router.Registry(reg), router.Id("test-router")).(*registryRouter)
			if source == "cache_filter" {
				if err := r.table.Create(router.Route{Service: "websocket", Address: "127.0.0.1:9000", Network: "micro", Link: router.DefaultLink, Metadata: map[string]string{"uid": "other", "credential": "private-value"}}); err != nil {
					t.Fatal(err)
				}
			}
			if source == "registry_not_found" {
				reg.err = registry.ErrNotFound
			}
			ctx := metadata.NewContext(context.Background(), metadata.Metadata{"Micro-Trace-Id": "trace-one", "Authorization": "private-value"})
			routes, err := r.Lookup("websocket", router.LookupUid("wanted"), router.LookupContext(ctx))
			if err != router.ErrRouteNotFound || routes != nil {
				t.Fatalf("routes=%v err=%v", routes, err)
			}
			for _, want := range []string{"Route lookup miss", source, "wanted", "trace-one", "test-router", "candidate_count"} {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("missing %q in %s", want, out.String())
				}
			}
			if strings.Contains(out.String(), "private-value") {
				t.Fatalf("private metadata logged: %s", out.String())
			}
			if source == "cache_filter" && reg.calls != 0 {
				t.Fatal("diagnostics introduced a registry lookup")
			}
			if source != "cache_filter" && reg.calls != 1 {
				t.Fatalf("registry calls=%d", reg.calls)
			}
			if source != "registry_not_found" {
				out.Reset()
				routes, err = r.Lookup("websocket", router.LookupUid("other"), router.LookupContext(ctx))
				if err != nil || len(routes) != 1 {
					t.Fatalf("successful route changed: %v %v", routes, err)
				}
				if out.Len() != 0 {
					t.Fatalf("successful lookup logged: %s", out.String())
				}
			}
		})
	}
}

func TestLookupMissCandidateLimit(t *testing.T) {
	var out bytes.Buffer
	previous := logger.DefaultLogger
	logger.DefaultLogger = logger.NewLogger(logger.WithOutput(&out), logger.WithLevel(logger.WarnLevel))
	t.Cleanup(func() { logger.DefaultLogger = previous })
	r := &registryRouter{options: router.Options{Id: "test"}}
	candidates := make([]router.Route, 20)
	for i := range candidates {
		candidates[i].Address = "candidate-address"
	}
	r.logLookupMiss("websocket", router.NewLookup(), "cache_filter", candidates, 0)
	if n := strings.Count(out.String(), "candidate-address"); n != 8 {
		t.Fatalf("logged %d candidates", n)
	}
	if !strings.Contains(out.String(), "candidates_truncated=true") {
		t.Fatalf("missing truncation: %s", out.String())
	}
}
