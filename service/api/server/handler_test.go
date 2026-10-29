package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/micro/micro/v3/proto/errors"
	"github.com/micro/micro/v3/service/api/handler"
	"github.com/micro/micro/v3/service/api/handler/rpc"
	"github.com/micro/micro/v3/service/api/resolver"
	"github.com/micro/micro/v3/service/api/router"
	registryrouter "github.com/micro/micro/v3/service/api/router/registry"
	apiserver "github.com/micro/micro/v3/service/api/server"
	brokermemory "github.com/micro/micro/v3/service/broker/memory"
	"github.com/micro/micro/v3/service/client"
	grpcclient "github.com/micro/micro/v3/service/client/grpc"
	microerrors "github.com/micro/micro/v3/service/errors"
	"github.com/micro/micro/v3/service/registry"
	"github.com/micro/micro/v3/service/registry/memory"
	"github.com/micro/micro/v3/service/server"
	grpcserver "github.com/micro/micro/v3/service/server/grpc"
)

// ContractFixture deliberately uses the same JSON codec as a real RPC backend.
// Assigning raw bytes to a fake client response would miss codec rounding.
type ContractFixture struct{}

func (*ContractFixture) Echo(_ context.Context, req *json.RawMessage, rsp *json.RawMessage) error {
	*rsp = append((*rsp)[:0], (*req)...)
	return nil
}
func (*ContractFixture) Forbidden(context.Context, *json.RawMessage, *json.RawMessage) error {
	return microerrors.Forbidden("fixture", "quoted \"value\"\nnext <tag>")
}
func (*ContractFixture) Limited(context.Context, *json.RawMessage, *json.RawMessage) error {
	return microerrors.New("fixture", "retry later", 429)
}

func apiFixture(t *testing.T) (client.Client, registry.Registry) {
	t.Helper()
	reg := memory.NewRegistry()
	s := grpcserver.NewServer(server.Name("fixture"), server.Address("127.0.0.1:0"),
		server.Registry(reg), server.Broker(brokermemory.NewBroker()))
	if err := s.Handle(s.NewHandler(&ContractFixture{})); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Stop() })
	if err := s.(interface{ Register() error }).Register(); err != nil {
		t.Fatal(err)
	}
	return grpcclient.NewClient(client.Retries(0), client.RequestTimeout(3*time.Second)), reg
}

func registryRoute(t *testing.T, reg registry.Registry, name string) router.Router {
	t.Helper()
	r := registryrouter.NewRouter(router.WithRegistry(reg), router.WithHandler(name),
		router.WithResolver(apiserver.NewResolver(resolver.WithHandler(name))))
	t.Cleanup(func() { r.Close() })
	return r
}

func serve(h http.Handler, path, contentType, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestMetaAndRPCPreserveJSON(t *testing.T) {
	c, reg := apiFixture(t)
	for _, mode := range []string{"meta", "rpc"} {
		r := registryRoute(t, reg, mode)
		var h http.Handler = apiserver.Meta(c, r, "micro")
		if mode == "rpc" {
			h = rpc.NewHandler(handler.WithClient(c), handler.WithRouter(r))
		}
		for _, ct := range []string{"application/json", "application/grpc+json"} {
			for _, body := range []string{
				`9007199254740993`, `-9007199254740993`, `18446744073709551615`,
				`"9007199254740993"`, `null`, `1.25`, `true`, `"<>&"`,
				`[9007199254740993,null,"<>&"]`,
				`{"n":9007199254740993,"nested":{"n":18446744073709551615},"nil":null,"html":"<>&"}`,
			} {
				t.Run(mode+"/"+ct+"/"+body, func(t *testing.T) {
					// Compare with the actual backend wire representation, including
					// any HTML escapes the gRPC JSON codec has already emitted.
					var direct json.RawMessage
					request := json.RawMessage(body)
					svcs, err := reg.GetService("fixture")
					if err != nil || len(svcs) == 0 || len(svcs[0].Nodes) == 0 {
						t.Fatalf("fixture registration unavailable: %v", err)
					}
					req := c.NewRequest("fixture", "ContractFixture.Echo", &request, client.WithContentType(ct))
					if err := c.Call(context.Background(), req, &direct, client.WithAddress(svcs[0].Nodes[0].Address)); err != nil {
						t.Fatal(err)
					}
					w := serve(h, "/fixture/contractFixture/echo", ct, body)
					if w.Code != 200 || w.Body.String() != string(direct) {
						t.Fatalf("backend response changed: status=%d body=%s, want %s", w.Code, w.Body.String(), direct)
					}
					// UseNumber ensures the assertion itself cannot round large integers.
					decode := func(raw string) interface{} {
						d := json.NewDecoder(strings.NewReader(raw))
						d.UseNumber()
						var value interface{}
						if err := d.Decode(&value); err != nil {
							t.Fatal(err)
						}
						return value
					}
					if !reflect.DeepEqual(decode(w.Body.String()), decode(body)) {
						t.Fatalf("JSON value changed: got %s, want %s", w.Body.String(), body)
					}
					if w.Header().Get("Content-Type") != ct || w.Header().Get("Content-Length") != strconv.Itoa(len(direct)) {
						t.Fatalf("response headers changed: %v", w.Header())
					}
					if ct == "application/grpc+json" && w.Header().Get("grpc-status") != "0" {
						t.Fatalf("success grpc-status changed: %v", w.Header())
					}
				})
			}
		}
	}
}

func assertHTTPError(t *testing.T, w *httptest.ResponseRecorder, expected error) {
	t.Helper()
	want := microerrors.FromError(expected)
	if w.Code != int(want.Code) || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("error status/type: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 4 {
		t.Fatalf("expected four legacy fields: %s", w.Body.String())
	}
	for _, key := range []string{"Id", "Code", "Detail", "Status"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("missing legacy field %s: %s", key, w.Body.String())
		}
	}
	if got := microerrors.Parse(w.Body.String()); !reflect.DeepEqual(got, want) {
		t.Fatalf("error value changed: got %+v, want %+v", got, want)
	}
}

func TestMetaAndRPCLegacyErrors(t *testing.T) {
	c, reg := apiFixture(t)
	for _, mode := range []string{"meta", "rpc"} {
		r := registryRoute(t, reg, mode)
		var h http.Handler = apiserver.Meta(c, r, "micro")
		if mode == "rpc" {
			h = rpc.NewHandler(handler.WithClient(c), handler.WithRouter(r))
		}
		for _, tc := range []struct {
			method string
			err    error
		}{
			{"forbidden", microerrors.Forbidden("fixture", "quoted \"value\"\nnext <tag>")},
			{"limited", microerrors.New("fixture", "retry later", 429)},
		} {
			t.Run(mode+"/"+tc.method, func(t *testing.T) {
				assertHTTPError(t, serve(h, "/fixture/contractFixture/"+tc.method, "application/json", `{}`), tc.err)
			})
		}
		t.Run(mode+"/missing_service", func(t *testing.T) {
			id := "micro"
			if mode == "rpc" {
				id = "go.micro.api"
			}
			assertHTTPError(t, serve(h, "/missing/contractFixture/echo", "application/json", `{}`),
				microerrors.InternalServerError(id, "service not found"))
		})
	}
	t.Run("rpc/no_router", func(t *testing.T) {
		assertHTTPError(t, serve(rpc.NewHandler(handler.WithClient(c)), "/fixture/contractFixture/echo", "application/json", `{}`),
			microerrors.InternalServerError("go.micro.api", "no route found"))
	})
}
