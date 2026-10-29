package grpc

import (
	"context"
	stderrors "errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/micro/micro/v3/service/client"
	pb "github.com/micro/micro/v3/service/client/grpc/proto"
	merrors "github.com/micro/micro/v3/service/errors"
	"github.com/micro/micro/v3/service/registry"
	"github.com/micro/micro/v3/service/registry/memory"
	"github.com/micro/micro/v3/service/router"
	regRouter "github.com/micro/micro/v3/service/router/registry"
	pgrpc "google.golang.org/grpc"
)

type failingProbeRegistry struct {
	registry.Registry
	err error
}

func (r failingProbeRegistry) Snapshot(context.Context, registry.SnapshotQuery) (*registry.Snapshot, error) {
	return nil, r.err
}

func TestInstanceProbePreservesDiscoveryErrors(t *testing.T) {
	for _, tt := range []struct {
		name    string
		source  registry.Registry
		proxy   string
		options []client.CallOption
		want    error
	}{
		{name: "unsupported registry", source: memory.NewRegistry(), want: registry.ErrSnapshotUnsupported},
		{name: "proxy", proxy: "127.0.0.1:1", want: client.ErrInstanceProbeUnsupported},
		{name: "address override", options: []client.CallOption{client.WithAddress("127.0.0.1:1")}, want: client.ErrInstanceProbeUnsupported},
		{name: "discovery canceled", source: failingProbeRegistry{err: fmt.Errorf("snapshot read: %w", context.Canceled)}, want: context.Canceled},
		{name: "discovery deadline", source: failingProbeRegistry{err: fmt.Errorf("snapshot read: %w", context.DeadlineExceeded)}, want: context.DeadlineExceeded},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := NewClient(client.Proxy(tt.proxy), client.Router(regRouter.NewRouter(router.Registry(tt.source))))
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			req := c.NewRequest("helloworld", "Greeter.SayHello", &pb.HelloRequest{})
			err := client.ProbeInstance(ctx, c, req, &pb.HelloReply{}, "games", "helloworld-one", tt.options...)
			if !stderrors.Is(err, tt.want) {
				t.Fatalf("expected %v in error chain, got %v", tt.want, err)
			}
		})
	}
}

func TestOrdinaryCallPreservesLookupErrorResponse(t *testing.T) {
	want := stderrors.New("lookup failed")
	c := NewClient(func(o *client.Options) {
		o.Lookup = func(context.Context, client.Request, client.CallOptions) ([]string, error) {
			return nil, want
		}
	})
	req := c.NewRequest("helloworld", "Greeter.SayHello", &pb.HelloRequest{})
	err := c.Call(context.Background(), req, &pb.HelloReply{})
	var response *merrors.Error
	if !stderrors.As(err, &response) || response.Code != 500 || response.Detail != want.Error() {
		t.Fatalf("ordinary lookup error response changed: %v", err)
	}
}

type legacyProbeRegistry struct{ registry.Registry }

func (r legacyProbeRegistry) Snapshot(ctx context.Context, q registry.SnapshotQuery) (*registry.Snapshot, error) {
	services, err := r.GetService(q.Service, registry.GetDomain(q.Domain), registry.GetContext(ctx))
	if err != nil {
		return nil, err
	}
	return &registry.Snapshot{Services: services, Revision: "test"}, nil
}

func TestInstanceProbeRejectsLegacyResponse(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := pgrpc.NewServer()
	pb.RegisterGreeterServer(s, &greeterServer{})
	go s.Serve(listener)
	defer s.Stop()
	r := legacyProbeRegistry{memory.NewRegistry()}
	if err := r.Register(&registry.Service{Name: "helloworld", Nodes: []*registry.Node{{Id: "helloworld-old", Address: listener.Addr().String()}}}, registry.RegisterDomain("games")); err != nil {
		t.Fatal(err)
	}
	c := NewClient(client.Router(regRouter.NewRouter(router.Registry(r))))
	req := c.NewRequest("helloworld", "Greeter.SayHello", &pb.HelloRequest{Name: "John"})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	rsp := &pb.HelloReply{}
	if err := client.ProbeInstance(ctx, c, req, rsp, "games", "helloworld-old", client.WithRetries(0)); !stderrors.Is(err, client.ErrInstanceProbeUnsupported) {
		t.Fatalf("unverified response accepted: %v", err)
	}
	if err := c.Call(ctx, req, &pb.HelloReply{}, client.WithServerUid("helloworld-old"), client.WithRetries(0)); err != nil {
		t.Fatalf("ordinary legacy call regressed: %v", err)
	}
}
