package rpc_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/micro/micro/v3/service/api/handler"
	"github.com/micro/micro/v3/service/api/handler/rpc"
	"github.com/micro/micro/v3/service/api/resolver"
	"github.com/micro/micro/v3/service/api/router"
	registryrouter "github.com/micro/micro/v3/service/api/router/registry"
	apiserver "github.com/micro/micro/v3/service/api/server"
	brokermemory "github.com/micro/micro/v3/service/broker/memory"
	"github.com/micro/micro/v3/service/client"
	grpcclient "github.com/micro/micro/v3/service/client/grpc"
	"github.com/micro/micro/v3/service/registry/memory"
	"github.com/micro/micro/v3/service/server"
	grpcserver "github.com/micro/micro/v3/service/server/grpc"
)

type UnaryWireFixture struct{}

func (*UnaryWireFixture) Echo(_ context.Context, req *wrappers.StringValue, rsp *wrappers.StringValue) error {
	rsp.Value = req.Value
	return nil
}

func (*UnaryWireFixture) Reply(_ context.Context, req *wrappers.StringValue, rsp *wrappers.StringValue) error {
	rsp.Value = "reply:" + req.Value
	return nil
}

func (*UnaryWireFixture) Clear(_ context.Context, _ *wrappers.StringValue, _ *wrappers.StringValue) error {
	return nil
}

func TestProtobufUnaryRoundTrip(t *testing.T) {
	reg := memory.NewRegistry()
	s := grpcserver.NewServer(server.Name("wire-fixture"), server.Address("127.0.0.1:0"), server.Registry(reg), server.Broker(brokermemory.NewBroker()))
	if err := s.Handle(s.NewHandler(&UnaryWireFixture{})); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Stop() })
	if err := s.(interface{ Register() error }).Register(); err != nil {
		t.Fatal(err)
	}
	rt := registryrouter.NewRouter(router.WithRegistry(reg), router.WithHandler("rpc"), router.WithResolver(apiserver.NewResolver(resolver.WithHandler("rpc"))))
	t.Cleanup(func() { rt.Close() })
	c := grpcclient.NewClient(client.Retries(0), client.RequestTimeout(2*time.Second))
	g := httptest.NewServer(rpc.NewHandler(handler.WithClient(c), handler.WithRouter(rt)))
	t.Cleanup(g.Close)
	httpClient := &http.Client{Timeout: 3 * time.Second}
	for _, contentType := range []string{"application/proto", "application/protobuf", "application/grpc", "application/grpc+proto", "application/protobuf; charset=UTF-8"} {
		for _, tc := range []struct{ method, input, want string }{
			{"Echo", "", ""},
			{"Echo", "hello", "hello"},
			{"Reply", "", "reply:"},
			{"Clear", "hello", ""},
		} {
			t.Run(contentType+"/"+tc.method+"/"+tc.input, func(t *testing.T) {
				body, err := proto.Marshal(&wrappers.StringValue{Value: tc.input})
				if err != nil {
					t.Fatal(err)
				}
				req, err := http.NewRequest(http.MethodPost, g.URL+"/wire-fixture/unaryWireFixture/"+tc.method, bytes.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Content-Type", contentType)
				req.Header.Set("Micro-Namespace", "micro")
				rsp, err := httpClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer rsp.Body.Close()
				data, err := io.ReadAll(rsp.Body)
				if err != nil {
					t.Fatal(err)
				}
				wantStatus := http.StatusOK
				if tc.want == "" {
					wantStatus = http.StatusNoContent
				}
				if rsp.StatusCode != wantStatus {
					t.Fatalf("status=%d body=%s", rsp.StatusCode, data)
				}
				var got wrappers.StringValue
				if err := proto.Unmarshal(data, &got); err != nil {
					t.Fatal(err)
				}
				if got.Value != tc.want {
					t.Fatalf("got %q, want %q", got.Value, tc.want)
				}
			})
		}
	}
}
