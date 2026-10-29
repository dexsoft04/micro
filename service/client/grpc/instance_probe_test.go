package grpc

import (
	"context"
	stderrors "errors"
	"net"
	"testing"
	"time"

	"github.com/micro/micro/v3/service/client"
	pb "github.com/micro/micro/v3/service/client/grpc/proto"
	"github.com/micro/micro/v3/service/registry"
	"github.com/micro/micro/v3/service/registry/memory"
	"github.com/micro/micro/v3/service/router"
	regRouter "github.com/micro/micro/v3/service/router/registry"
	pgrpc "google.golang.org/grpc"
)

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
