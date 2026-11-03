package rpc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/micro/micro/v3/service/api"
	"github.com/micro/micro/v3/service/client"
	gcli "github.com/micro/micro/v3/service/client/grpc"
)

func BenchmarkHTTPRPCObservedSuccess(b *testing.B) {
	c := diagnosticClient{Client: gcli.NewClient(), invoke: func(_ context.Context, _ client.Request, rsp interface{}) error {
		*rsp.(*json.RawMessage) = json.RawMessage(`{"id":1234567890123456789}`)
		return nil
	}}
	svc := &api.Service{Name: "synthetic", Endpoint: &api.Endpoint{Name: "Fixture.Call"}}
	ctx := diagnosticContext{Context: context.Background(), c: c, service: svc}
	h := NewHandler()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := httptest.NewRequest(http.MethodPost, "http://example.test/path", strings.NewReader(`{}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Request-Id", "benchmark-id")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r.WithContext(ctx))
		if w.Code != 200 {
			b.Fatal(w.Code)
		}
	}
}
