package client

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/micro/micro/v3/service/context/metadata"
	"github.com/micro/micro/v3/service/logger"
	"github.com/micro/micro/v3/service/router"
)

type lookupDiagnosticRouter struct {
	router.Router
	query router.LookupOptions
	calls int
}

func (r *lookupDiagnosticRouter) Lookup(_ string, opts ...router.LookupOption) ([]router.Route, error) {
	r.calls++
	r.query = router.NewLookup(opts...)
	return nil, router.ErrRouteNotFound
}
func (*lookupDiagnosticRouter) Options() router.Options { return router.Options{Id: "caller-router"} }
func (*lookupDiagnosticRouter) String() string          { return "diagnostic-test" }

func TestLookupFailureCorrelationAndAddressBypass(t *testing.T) {
	var out bytes.Buffer
	previous := logger.DefaultLogger
	logger.DefaultLogger = logger.NewLogger(logger.WithOutput(&out), logger.WithLevel(logger.WarnLevel))
	t.Cleanup(func() { logger.DefaultLogger = previous })
	ctx := metadata.NewContext(context.Background(), metadata.Metadata{"Micro-Trace-Id": "trace-one", "Authorization": "secret-value"})
	r := &lookupDiagnosticRouter{}
	opts := CallOptions{Router: r, Network: "micro"}
	WithServerUid("websocket-one")(&opts)
	req := &defaultRequest{service: "websocket", endpoint: "Session.Send"}
	_, err := LookupRoute(ctx, req, opts)
	if err == nil || !strings.Contains(err.Error(), "service websocket: route not found") {
		t.Fatalf("error contract changed: %v", err)
	}
	if r.query.Context != ctx || r.query.Uid != "websocket-one" || r.query.Network != "micro" {
		t.Fatalf("query changed: %+v", r.query)
	}
	for _, want := range []string{"trace-one", "Session.Send", "websocket-one", "caller-router", "route_not_found=true"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in %s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "secret-value") {
		t.Fatal("authentication metadata logged")
	}
	out.Reset()
	opts.Address = []string{"proxy:8081"}
	routes, err := LookupRoute(ctx, req, opts)
	if err != nil || len(routes) != 1 || routes[0] != "proxy:8081" || r.calls != 1 || out.Len() != 0 {
		t.Fatalf("address bypass changed: %v %v", routes, err)
	}
}
