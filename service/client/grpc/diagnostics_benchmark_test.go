package grpc

import (
	"context"
	"net"
	"testing"

	"github.com/micro/micro/v3/service/client"
	pb "github.com/micro/micro/v3/service/client/grpc/proto"
	"github.com/micro/micro/v3/service/context/metadata"
	pgrpc "google.golang.org/grpc"
)

// Uses only pre-existing public interfaces so the same benchmark can run on v27.
func BenchmarkRPCObservedSuccess(b *testing.B) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	s := pgrpc.NewServer()
	pb.RegisterGreeterServer(s, &greeterServer{})
	go s.Serve(l)
	defer s.Stop()
	c := NewClient(client.Lookup(func(context.Context, client.Request, client.CallOptions) ([]string, error) {
		return []string{l.Addr().String()}, nil
	}))
	ctx := metadata.NewContext(context.Background(), metadata.Metadata{"X-Request-Id": "benchmark-request", "Authorization": "synthetic", "Method": "POST"})
	req := c.NewRequest("helloworld", "Greeter.SayHello", &pb.HelloRequest{Name: "synthetic"})
	rsp := &pb.HelloReply{}
	if err := c.Call(ctx, req, rsp); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := c.Call(ctx, req, rsp); err != nil {
			b.Fatal(err)
		}
	}
}
