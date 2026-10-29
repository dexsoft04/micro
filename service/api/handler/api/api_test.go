package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	pb "github.com/micro/micro/v3/proto/api"
	"github.com/micro/micro/v3/service/api/handler"
	apihandler "github.com/micro/micro/v3/service/api/handler/api"
	"github.com/micro/micro/v3/service/api/resolver"
	"github.com/micro/micro/v3/service/api/router"
	registryrouter "github.com/micro/micro/v3/service/api/router/registry"
	apiserver "github.com/micro/micro/v3/service/api/server"
	"github.com/micro/micro/v3/service/client"
	grpcclient "github.com/micro/micro/v3/service/client/grpc"
	microerrors "github.com/micro/micro/v3/service/errors"
	"github.com/micro/micro/v3/service/registry"
	"github.com/micro/micro/v3/service/registry/memory"
)

type stubRPC struct {
	client.Client
	err      error
	response *pb.Response
	calls    int
}

func (s *stubRPC) Call(_ context.Context, req client.Request, rsp interface{}, _ ...client.CallOption) error {
	s.calls++
	if req.Service() != "fixture" || req.Endpoint() != "Fixture.Call" {
		return errors.New("unexpected fixture route")
	}
	if s.err != nil {
		return s.err
	}
	*rsp.(*pb.Response) = *s.response
	return nil
}
func fixtureRouter(t *testing.T, registered bool) router.Router {
	t.Helper()
	reg := memory.NewRegistry()
	if registered {
		if err := reg.Register(&registry.Service{Name: "fixture", Version: "test", Nodes: []*registry.Node{{Id: "fixture-node", Address: "127.0.0.1:1"}}}); err != nil {
			t.Fatal(err)
		}
	}
	rt := registryrouter.NewRouter(router.WithRegistry(reg), router.WithHandler("api"), router.WithResolver(apiserver.NewResolver(resolver.WithHandler("api"))))
	t.Cleanup(func() { rt.Close() })
	return rt
}
func assertLegacyError(t *testing.T, recorder *httptest.ResponseRecorder, status int, expected microerrors.Error) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("HTTP status: got %d, want %d", recorder.Code, status)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type: %q", got)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 4 {
		t.Fatalf("error keys: got %v, want exactly Id/Code/Detail/Status", fields)
	}
	for _, key := range []string{"Id", "Code", "Detail", "Status"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("missing legacy key %s in %s", key, recorder.Body.String())
		}
	}
	if got := microerrors.Parse(recorder.Body.String()); !reflect.DeepEqual(*got, expected) {
		t.Fatalf("error fields: got %+v, want %+v", got, expected)
	}
}

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, errors.New("fixture read failure") }

func TestHandlerLegacyErrorResponses(t *testing.T) {
	detail := "quoted \"value\"\nnext line <tag>"
	cases := []struct {
		name        string
		input       error
		routing     string
		bodyFailure bool
		status      int
		expected    microerrors.Error
	}{
		{name: "request_conversion", bodyFailure: true, status: 500, expected: microerrors.Error{Id: "go.micro.api", Code: 500, Detail: "fixture read failure", Status: "Internal Server Error"}},
		{name: "registry_route_failure", routing: "missing", status: 500, expected: microerrors.Error{Id: "go.micro.api", Code: 500, Detail: "service not found", Status: "Internal Server Error"}},
		{name: "no_router", routing: "none", status: 500, expected: microerrors.Error{Id: "go.micro.api", Code: 500, Detail: "no route found", Status: "Internal Server Error"}},
		{name: "rpc_typed_error", input: microerrors.Forbidden("fixture", "%s", detail), status: 403, expected: microerrors.Error{Id: "fixture", Code: 403, Detail: detail, Status: "Forbidden"}},
		{name: "rpc_lowercase_json", input: errors.New(`{"id":"fixture","code":429,"detail":"retry later","status":"Too Many Requests"}`), status: 429, expected: microerrors.Error{Id: "fixture", Code: 429, Detail: "retry later", Status: "Too Many Requests"}},
		{name: "rpc_uppercase_json", input: errors.New(`{"Id":"fixture","Code":400,"Detail":"bad request","Status":"Bad Request"}`), status: 400, expected: microerrors.Error{Id: "fixture", Code: 400, Detail: "bad request", Status: "Bad Request"}},
		{name: "rpc_unstructured_error", input: errors.New("transport failed"), status: 500, expected: microerrors.Error{Detail: "transport failed"}},
		{name: "rpc_empty_fields", input: errors.New(`{"code":0}`), status: 500, expected: microerrors.Error{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &stubRPC{Client: grpcclient.NewClient(), err: tc.input}
			opts := []handler.Option{handler.WithClient(c)}
			if tc.routing != "none" {
				opts = append(opts, handler.WithRouter(fixtureRouter(t, tc.routing != "missing")))
			}
			req := httptest.NewRequest(http.MethodPost, "/fixture/fixture/call", strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			if tc.bodyFailure {
				req.Body = io.NopCloser(failingBody{})
			}
			recorder := httptest.NewRecorder()
			apihandler.NewHandler(opts...).ServeHTTP(recorder, req)
			assertLegacyError(t, recorder, tc.status, tc.expected)
			wantCalls := 1
			if tc.bodyFailure || tc.routing != "" {
				wantCalls = 0
			}
			if c.calls != wantCalls {
				t.Fatalf("RPC calls: got %d, want %d", c.calls, wantCalls)
			}
		})
	}
}
func TestHandlerPreservesApplicationResponse(t *testing.T) {
	for _, tc := range []struct {
		name              string
		status            int32
		contentType, body string
	}{
		{"default_status_and_type", 0, "", `{"code":0,"detail":"application payload"}`},
		{"created_with_custom_type", 201, "application/vnd.fixture+json", `{"Id":"application field"}`},
		{"application_error_is_not_gateway_error", 422, "text/plain; charset=utf-8", "application validation failed\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rsp := &pb.Response{StatusCode: tc.status, Body: tc.body, Header: map[string]*pb.Pair{"X-Fixture": {Key: "X-Fixture", Values: []string{"one", "two"}}}}
			if tc.contentType != "" {
				rsp.Header["Content-Type"] = &pb.Pair{Key: "Content-Type", Values: []string{tc.contentType}}
			}
			c := &stubRPC{Client: grpcclient.NewClient(), response: rsp}
			h := apihandler.NewHandler(handler.WithClient(c), handler.WithRouter(fixtureRouter(t, true)))
			recorder := httptest.NewRecorder()
			h.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/fixture/fixture/call", nil))
			status := int(tc.status)
			if status == 0 {
				status = 200
			}
			contentType := tc.contentType
			if contentType == "" {
				contentType = "application/json"
			}
			if recorder.Code != status || recorder.Body.String() != tc.body {
				t.Fatalf("application response changed: status=%d body=%q", recorder.Code, recorder.Body.String())
			}
			if recorder.Header().Get("Content-Type") != contentType || !reflect.DeepEqual(recorder.Header().Values("X-Fixture"), []string{"one", "two"}) {
				t.Fatalf("application headers changed: %v", recorder.Header())
			}
			if c.calls != 1 {
				t.Fatalf("RPC calls: %d", c.calls)
			}
		})
	}
}
