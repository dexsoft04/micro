package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/micro/micro/v3/service/api"
	"github.com/micro/micro/v3/service/client"
	gcli "github.com/micro/micro/v3/service/client/grpc"
	"github.com/micro/micro/v3/service/context/metadata"
	"github.com/micro/micro/v3/service/errors"
	"github.com/micro/micro/v3/service/logger"
)

type observationBuffer struct {
	sync.Mutex
	bytes.Buffer
}

func (b *observationBuffer) Write(p []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	return b.Buffer.Write(p)
}
func (b *observationBuffer) text() string { b.Lock(); defer b.Unlock(); return b.Buffer.String() }
func httpDiagnostic(t *testing.T, b *observationBuffer) map[string]interface{} {
	t.Helper()
	var result map[string]interface{}
	count := 0
	for _, line := range strings.Split(b.text(), "\n") {
		_, body, ok := strings.Cut(line, "RPC diagnostic ")
		if !ok {
			continue
		}
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatal(err)
		}
		count++
	}
	if count != 1 {
		t.Fatalf("want one HTTP record, got %d: %s", count, b.text())
	}
	return result
}

type diagnosticContext struct {
	context.Context
	c       client.Client
	service *api.Service
}

func (c diagnosticContext) Client() client.Client { return c.c }
func (c diagnosticContext) Service() *api.Service { return c.service }
func (c diagnosticContext) Domain() string        { return "synthetic" }

type diagnosticClient struct {
	client.Client
	invoke func(context.Context, client.Request, interface{}) error
}

func (c diagnosticClient) Call(ctx context.Context, req client.Request, rsp interface{}, _ ...client.CallOption) error {
	return c.invoke(ctx, req, rsp)
}

type failedWriter struct{ *httptest.ResponseRecorder }

func (w failedWriter) Write([]byte) (int, error) { return 0, stderrors.New("SECRET_WRITE_ERROR") }

func TestHTTPRPCDiagnosticsPreserveResponseAndPrivacy(t *testing.T) {
	for _, mode := range []string{"success", "rpc_error", "write_error", "canceled_success", "invalid_id", "duplicate_id", "write_error_error_level"} {
		t.Run(mode, func(t *testing.T) {
			b := new(observationBuffer)
			old := logger.DefaultLogger
			level := logger.WarnLevel
			if mode == "write_error_error_level" {
				level = logger.ErrorLevel
			}
			logger.DefaultLogger = logger.NewLogger(logger.WithOutput(b), logger.WithLevel(level))
			defer func() { logger.DefaultLogger = old }()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			c := diagnosticClient{Client: gcli.NewClient(), invoke: func(ctx context.Context, req client.Request, rsp interface{}) error {
				calls++
				token, _ := metadata.Get(ctx, "User-Token")
				if token != "SECRET_TOKEN" {
					t.Fatal("request metadata changed")
				}
				if mode == "rpc_error" || mode == "invalid_id" || mode == "duplicate_id" {
					return errors.Timeout("synthetic", "SECRET_ERROR")
				}
				*rsp.(*json.RawMessage) = json.RawMessage(`{"id":1234567890123456789,"value":"SECRET_RESPONSE"}`)
				if mode == "canceled_success" {
					cancel()
				}
				return nil
			}}
			svc := &api.Service{Name: "synthetic-service", Endpoint: &api.Endpoint{Name: "Fixture.Call"}}
			r := httptest.NewRequest(http.MethodPost, "http://example.test/path?secret=SECRET_QUERY", strings.NewReader(`{"secret":"SECRET_BODY"}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Request-Id", "request-123")
			r.Header.Set("User-Token", "SECRET_TOKEN")
			r.Header.Set("Authorization", "SECRET_AUTH")
			r.Header.Set("Cookie", "SECRET_COOKIE")
			if mode == "invalid_id" {
				r.Header.Set("X-Request-Id", "bad\nSECRET_ID")
			}
			if mode == "duplicate_id" {
				r.Header.Add("X-Request-Id", "second-id")
			}
			r = r.WithContext(diagnosticContext{Context: ctx, c: c, service: svc})
			w := httptest.NewRecorder()
			var writer http.ResponseWriter = w
			if strings.HasPrefix(mode, "write_error") {
				writer = failedWriter{w}
			}
			NewHandler().ServeHTTP(writer, r)
			if calls != 1 {
				t.Fatalf("calls %d", calls)
			}
			expected := http.StatusOK
			if mode == "rpc_error" || mode == "invalid_id" || mode == "duplicate_id" {
				expected = http.StatusRequestTimeout
			}
			if w.Code != expected {
				t.Fatalf("status %d expected %d", w.Code, expected)
			}
			if expected == 408 && !strings.Contains(w.Body.String(), "SECRET_ERROR") {
				t.Fatal("legacy error response changed")
			}
			if expected == 200 && !strings.HasPrefix(mode, "write_error") && w.Body.String() != `{"id":1234567890123456789,"value":"SECRET_RESPONSE"}` {
				t.Fatalf("response changed: %s", w.Body.String())
			}
			if mode == "success" {
				if b.text() != "" {
					t.Fatalf("success logged: %s", b.text())
				}
				return
			}
			record := httpDiagnostic(t, b)
			if record["http_status"] != float64(expected) || record["event"] != "http_rpc_end" {
				t.Fatalf("wrong observation: %+v", record)
			}
			if mode == "canceled_success" && record["request_context"] != "canceled" {
				t.Fatal("missing request cancellation")
			}
			if strings.HasPrefix(mode, "write_error") && record["write_error_kind"] != "other" {
				t.Fatal("missing write failure")
			}
			if mode == "invalid_id" || mode == "duplicate_id" {
				if record["request_id_state"] != "invalid" || record["request_id"] != nil {
					t.Fatal("unsafe request ID retained")
				}
			}
			if strings.Contains(b.text(), "SECRET_") {
				t.Fatalf("sensitive content logged: %s", b.text())
			}
		})
	}
}
func TestHTTPRPCDiagnosticEarlyRouteFailure(t *testing.T) {
	b := new(observationBuffer)
	old := logger.DefaultLogger
	logger.DefaultLogger = logger.NewLogger(logger.WithOutput(b), logger.WithLevel(logger.WarnLevel))
	defer func() { logger.DefaultLogger = old }()
	r := httptest.NewRequest(http.MethodGet, "http://example.test/SECRET_PATH?token=SECRET_QUERY", nil)
	w := httptest.NewRecorder()
	NewHandler().ServeHTTP(w, r)
	record := httpDiagnostic(t, b)
	if w.Code != 500 || record["stage"] != "route" || record["request_id_state"] != "missing" {
		t.Fatalf("wrong routing failure: %+v", record)
	}
	if strings.Contains(b.text(), "SECRET_") {
		t.Fatal("request URL logged")
	}
}
