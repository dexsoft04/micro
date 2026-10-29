package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/micro/micro/v3/service/registry"
	"github.com/micro/micro/v3/service/router"
)

type probeTestClient struct {
	Client
	call func(CallOptions) error
}

func (c probeTestClient) Call(_ context.Context, _ Request, _ interface{}, options ...CallOption) error {
	var o CallOptions
	for _, option := range options {
		option(&o)
	}
	return c.call(o)
}

type probeTestRegistry struct {
	registry.Registry
	snapshot *registry.Snapshot
}

func (r probeTestRegistry) Snapshot(context.Context, registry.SnapshotQuery) (*registry.Snapshot, error) {
	return r.snapshot, nil
}

type probeTestRouter struct {
	router.Router
	source registry.Registry
}

func (r probeTestRouter) Options() router.Options { return router.Options{Registry: r.source} }

func TestProbeInstanceRequiresTransportProof(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req := &defaultRequest{service: "game", endpoint: "Status.Check"}
	tests := []struct {
		name string
		call func(CallOptions) error
		want bool
	}{
		{"ignored options", func(CallOptions) error { return nil }, false},
		{"wrong instance", func(o CallOptions) error { return ConfirmInstanceProbe(o, "1", "games", "game-other") }, false},
		{"wrong domain", func(o CallOptions) error { return ConfirmInstanceProbe(o, "1", "other", "game-one") }, false},
		{"old protocol", func(o CallOptions) error { return ConfirmInstanceProbe(o, "", "games", "game-one") }, false},
		{"verified", func(o CallOptions) error { return ConfirmInstanceProbe(o, "1", "games", "game-one") }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ProbeInstance(ctx, probeTestClient{call: tt.call}, req, nil, "games", "game-one")
			if (err == nil) != tt.want {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestProbeInstanceRequiresAuthoritativeDiscovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	p := &instanceProbe{domain: "games", uid: "game-one"}
	snapshot := &registry.Snapshot{Revision: "test", Services: []*registry.Service{{Name: "game", Metadata: map[string]string{"domain": "games"}, Nodes: []*registry.Node{
		{Id: "game-other", Address: "other"}, {Id: "game-one", Address: "correct"},
	}}}}
	opts := CallOptions{Context: context.WithValue(ctx, instanceProbeKey{}, p), Router: probeTestRouter{source: probeTestRegistry{snapshot: snapshot}}}
	routes, err := LookupInstanceProbeRoutes(ctx, &defaultRequest{service: "game"}, opts)
	if err != nil || len(routes) != 1 || routes[0] != "correct" {
		t.Fatalf("routes=%v err=%v", routes, err)
	}
	opts.Address = []string{"proxy"}
	if _, err := LookupInstanceProbeRoutes(ctx, &defaultRequest{service: "game"}, opts); !errors.Is(err, ErrInstanceProbeUnsupported) {
		t.Fatalf("address override accepted: %v", err)
	}
	opts.Address = nil
	opts.Router = probeTestRouter{}
	if _, err := LookupInstanceProbeRoutes(ctx, &defaultRequest{service: "game"}, opts); err == nil {
		t.Fatal("router without authoritative discovery accepted")
	}
}
