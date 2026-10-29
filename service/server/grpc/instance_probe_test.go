package grpc_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	bmemory "github.com/micro/micro/v3/service/broker/memory"
	"github.com/micro/micro/v3/service/client"
	gcli "github.com/micro/micro/v3/service/client/grpc"
	meta "github.com/micro/micro/v3/service/context/metadata"
	"github.com/micro/micro/v3/service/registry"
	rmemory "github.com/micro/micro/v3/service/registry/memory"
	"github.com/micro/micro/v3/service/router"
	rtreg "github.com/micro/micro/v3/service/router/registry"
	"github.com/micro/micro/v3/service/server"
	gsrv "github.com/micro/micro/v3/service/server/grpc"
	pb "github.com/micro/micro/v3/service/server/grpc/proto"
)

type probeSnapshotRegistry struct{ registry.Registry }

func (r probeSnapshotRegistry) Snapshot(ctx context.Context, q registry.SnapshotQuery) (*registry.Snapshot, error) {
	services, err := r.Registry.ListServices(registry.ListDomain(q.Domain), registry.ListContext(ctx))
	if err != nil {
		return nil, err
	}
	var result []*registry.Service
	for _, service := range services {
		if service.Name == q.Service {
			result = append(result, service)
		}
	}
	return &registry.Snapshot{Services: result, Revision: "test"}, nil
}

type instanceProbeHandler struct {
	testServer
	calls atomic.Int32
}

func (h *instanceProbeHandler) Call(ctx context.Context, req *pb.Request, rsp *pb.Response) error {
	md, _ := meta.FromContext(ctx)
	for _, k := range []string{client.InstanceProbeVersionHeader, client.InstanceProbeUIDHeader, client.InstanceProbeDomainHeader, client.InstanceProofHeader} {
		if md[k] != "" {
			return fmt.Errorf("probe header leaked to handler")
		}
	}
	h.calls.Add(1)
	rsp.Msg = req.Name
	return nil
}

func TestVerifiedInstanceProbe(t *testing.T) {
	const domain = "probe-test"
	r := probeSnapshotRegistry{rmemory.NewRegistry()}
	s := gsrv.NewServer(server.Name("probe"), server.Id("one"), server.Namespace(domain), server.Address("127.0.0.1:0"), server.Registry(r), server.Broker(bmemory.NewBroker()))
	h := &instanceProbeHandler{}
	pb.RegisterTestHandler(s, h)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	c := gcli.NewClient(client.Router(rtreg.NewRouter(router.Registry(r))))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req := c.NewRequest("probe", "Test.Call", &pb.Request{Name: "observed"})
	rsp := &pb.Response{}
	if err := client.ProbeInstance(ctx, c, req, rsp, domain, "probe-one", client.WithRetries(0)); err != nil {
		t.Fatal(err)
	}
	if rsp.Msg != "observed" || h.calls.Load() != 1 {
		t.Fatalf("response %v calls %d", rsp, h.calls.Load())
	}
	if err := client.ProbeInstance(ctx, c, req, &pb.Response{}, domain, "probe-missing", client.WithRetries(0)); err == nil {
		t.Fatal("missing instance accepted")
	}
	if err := client.ProbeInstance(ctx, c, req, &pb.Response{}, domain, "probe-one", client.WithAddress("127.0.0.1:1")); err == nil {
		t.Fatal("address override accepted")
	}
	proxy := gcli.NewClient(client.Proxy("127.0.0.1:1"), client.Router(rtreg.NewRouter(router.Registry(r))))
	if err := client.ProbeInstance(ctx, proxy, req, &pb.Response{}, domain, "probe-one"); err == nil {
		t.Fatal("proxy accepted")
	}
	// Simulate a stale registration whose address now belongs to a different process.
	services, err := r.GetService("probe", registry.GetDomain(domain))
	if err != nil {
		t.Fatal(err)
	}
	address := services[0].Nodes[0].Address
	if err := r.Register(&registry.Service{Name: "probe", Version: "stale", Nodes: []*registry.Node{{Id: "probe-old", Address: address}}}, registry.RegisterDomain(domain)); err != nil {
		t.Fatal(err)
	}
	stale := gcli.NewClient(client.Router(rtreg.NewRouter(router.Registry(r))))
	if err := client.ProbeInstance(ctx, stale, req, &pb.Response{}, domain, "probe-old", client.WithRetries(1)); err == nil {
		t.Fatal("wrong responding process accepted")
	}
	if h.calls.Load() != 1 {
		t.Fatalf("wrong target invoked handler: %d", h.calls.Load())
	}
	// Legacy callers are still accepted without the probe protocol.
	if err := c.Call(ctx, req, &pb.Response{}, client.WithServerUid("probe-one"), client.WithRetries(0)); err != nil {
		t.Fatal(err)
	}
	if h.calls.Load() != 2 {
		t.Fatalf("legacy call failed: %d", h.calls.Load())
	}
}

func TestInstanceProbeDefaultNamespace(t *testing.T) {
	r := probeSnapshotRegistry{rmemory.NewRegistry()}
	s := gsrv.NewServer(server.Name("default-probe"), server.Id("one"), server.Address("127.0.0.1:0"), server.Registry(r), server.Broker(bmemory.NewBroker()))
	pb.RegisterTestHandler(s, &instanceProbeHandler{})
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	c := gcli.NewClient(client.Router(rtreg.NewRouter(router.Registry(r))))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req := c.NewRequest("default-probe", "Test.Call", &pb.Request{})
	if err := client.ProbeInstance(ctx, c, req, &pb.Response{}, registry.DefaultDomain, "default-probe-one", client.WithRetries(0)); err != nil {
		t.Fatal(err)
	}
}
