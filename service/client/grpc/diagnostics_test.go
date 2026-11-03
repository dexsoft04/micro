package grpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/micro/micro/v3/service/client"
	pb "github.com/micro/micro/v3/service/client/grpc/proto"
	"github.com/micro/micro/v3/service/context/metadata"
	"github.com/micro/micro/v3/service/errors"
	"github.com/micro/micro/v3/service/logger"
	pgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type diagnosticBuffer struct {
	sync.Mutex
	bytes.Buffer
}

func (b *diagnosticBuffer) Write(p []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	return b.Buffer.Write(p)
}
func (b *diagnosticBuffer) text() string { b.Lock(); defer b.Unlock(); return b.Buffer.String() }
func (b *diagnosticBuffer) records() []map[string]interface{} {
	var records []map[string]interface{}
	for _, line := range strings.Split(b.text(), "\n") {
		_, body, ok := strings.Cut(line, "RPC diagnostic ")
		if !ok {
			continue
		}
		var record map[string]interface{}
		if json.Unmarshal([]byte(body), &record) == nil {
			records = append(records, record)
		}
	}
	return records
}
func captureDiagnostics(t *testing.T) *diagnosticBuffer {
	t.Helper()
	b := new(diagnosticBuffer)
	previous := logger.DefaultLogger
	logger.DefaultLogger = logger.NewLogger(logger.WithOutput(b), logger.WithLevel(logger.WarnLevel))
	t.Cleanup(func() { logger.DefaultLogger = previous })
	return b
}
func awaitRecords(t *testing.T, b *diagnosticBuffer, count int) []map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if r := b.records(); len(r) >= count {
			return r
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("missing diagnostics: %s", b.text())
	return nil
}
func recordEvent(t *testing.T, records []map[string]interface{}, event string) map[string]interface{} {
	t.Helper()
	for _, r := range records {
		if r["event"] == event {
			return r
		}
	}
	t.Fatalf("missing %s: %+v", event, records)
	return nil
}

type diagnosticGreeter struct {
	pb.UnimplementedGreeterServer
	entered chan struct{}
	mu      sync.Mutex
	calls   int
}

func (s *diagnosticGreeter) SayHello(ctx context.Context, r *pb.HelloRequest) (*pb.HelloReply, error) {
	s.mu.Lock()
	s.calls++
	n := s.calls
	s.mu.Unlock()
	switch r.Name {
	case "wait":
		select {
		case s.entered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return nil, status.FromContextError(ctx.Err()).Err()
	case "remote_cancel":
		return nil, status.Error(codes.Canceled, "SECRET_ERROR")
	case "remote_deadline":
		return nil, status.Error(codes.DeadlineExceeded, "SECRET_ERROR")
	case "fail_once":
		if n == 1 {
			return nil, status.Error(codes.Internal, "SECRET_ERROR")
		}
	}
	return &pb.HelloReply{Message: "SECRET_RESPONSE"}, nil
}
func newDiagnosticClient(t *testing.T) (client.Client, *diagnosticGreeter) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := pgrpc.NewServer()
	handler := &diagnosticGreeter{entered: make(chan struct{}, 1)}
	pb.RegisterGreeterServer(srv, handler)
	go srv.Serve(listener)
	t.Cleanup(srv.Stop)
	return NewClient(client.Lookup(func(context.Context, client.Request, client.CallOptions) ([]string, error) {
		return []string{listener.Addr().String()}, nil
	})), handler
}
func syntheticMetadata(ctx context.Context, id string) context.Context {
	return metadata.NewContext(ctx, metadata.Metadata{"X-Request-Id": id, "Authorization": "SECRET_AUTH", "User-Token": "SECRET_TOKEN", "Url": "/anything?secret=SECRET_QUERY"})
}
func diagnosticCall(c client.Client, ctx context.Context, name string, opts ...client.CallOption) error {
	return c.Call(ctx, c.NewRequest("helloworld", "/helloworld.Greeter/SayHello", &pb.HelloRequest{Name: name}), &pb.HelloReply{}, opts...)
}
func TestRPCDiagnosticsContextAndRawStatus(t *testing.T) {
	for _, tc := range []struct {
		name, action       string
		local, preCanceled bool
		grpc               string
	}{
		{name: "pre_canceled", preCanceled: true},
		{name: "local_deadline", action: "wait", local: true},
		{name: "active_cancel", action: "wait"},
		{name: "remote_cancel", action: "remote_cancel", grpc: "Canceled"},
		{name: "remote_deadline", action: "remote_deadline", grpc: "DeadlineExceeded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := captureDiagnostics(t)
			c, h := newDiagnosticClient(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.preCanceled {
				cancel()
			}
			if tc.name == "active_cancel" {
				go func() {
					select {
					case <-h.entered:
						cancel()
					case <-ctx.Done():
					}
				}()
			}
			timeout := time.Second
			if tc.local {
				timeout = 25 * time.Millisecond
			}
			err := diagnosticCall(c, syntheticMetadata(ctx, tc.name), tc.action, client.WithRequestTimeout(timeout), client.WithRetries(0))
			e, ok := err.(*errors.Error)
			if !ok {
				t.Fatalf("expected micro error: %v", err)
			}
			want := int32(408)
			if tc.grpc != "" {
				want = 500
			}
			// v27 races gRPC completion against ctx.Done in two selects. Preserve
			// that contract; diagnostics must not force one winner for in-flight cancellation.
			inFlightCancellation := tc.local || tc.name == "active_cancel"
			if e.Code != want && !(inFlightCancellation && e.Code == 500) {
				t.Fatalf("code %d want %d (or existing cancellation race result)", e.Code, want)
			}
			count := 2
			if tc.preCanceled {
				count = 1
			}
			records := awaitRecords(t, b, count)
			final := recordEvent(t, records, "rpc_call_end")
			if final["request_id"] != tc.name {
				t.Fatalf("wrong correlation: %+v", final)
			}
			if tc.local {
				if final["parent_context"] != "active" || final["call_context"] != "deadline_exceeded" {
					t.Fatalf("wrong deadline attribution: %+v", final)
				}
			} else if tc.grpc != "" {
				a := recordEvent(t, records, "rpc_attempt_end")
				if a["grpc_status"] != tc.grpc || final["call_context"] != "active" {
					t.Fatalf("remote error attribution: %+v", records)
				}
			} else if final["parent_context"] != "canceled" {
				t.Fatalf("missing parent cancel: %+v", final)
			}
			if strings.Contains(b.text(), "SECRET_") {
				t.Fatal("sensitive value logged")
			}
		})
	}
}
func TestRPCDiagnosticsRetryAndSuccessVolume(t *testing.T) {
	for _, mode := range []string{"success", "default_retry", "explicit_retry"} {
		t.Run(mode, func(t *testing.T) {
			b := captureDiagnostics(t)
			c, h := newDiagnosticClient(t)
			action := "fail_once"
			opts := []client.CallOption{client.WithRequestTimeout(time.Second)}
			if mode == "success" {
				action = "ok"
			}
			if mode == "explicit_retry" {
				opts = append(opts, client.WithRetry(client.RetryOnError))
			}
			err := diagnosticCall(c, syntheticMetadata(context.Background(), mode), action, opts...)
			h.mu.Lock()
			calls := h.calls
			h.mu.Unlock()
			if mode == "success" {
				if err != nil || calls != 1 || len(b.records()) != 0 {
					t.Fatalf("success changed or logged: %v %d %s", err, calls, b.text())
				}
				return
			}
			want := 1
			if mode == "explicit_retry" {
				want = 2
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("expected error")
			}
			if calls != want {
				t.Fatalf("calls %d want %d", calls, want)
			}
			records := awaitRecords(t, b, want+1)
			final := recordEvent(t, records, "rpc_call_end")
			if final["attempts_scheduled"] != float64(want) {
				t.Fatalf("attempt count: %+v", final)
			}
			for _, r := range records {
				if r["call_id"] != final["call_id"] {
					t.Fatal("call correlation lost")
				}
			}
			if mode == "explicit_retry" && final["outcome"] != "success" {
				t.Fatal("recovered result not recorded")
			}
			if strings.Contains(b.text(), "SECRET_") {
				t.Fatal("sensitive value logged")
			}
		})
	}
}
func TestRPCDiagnosticsInvalidIDsAndConcurrentCalls(t *testing.T) {
	b := captureDiagnostics(t)
	c, _ := newDiagnosticClient(t)
	ids := []string{"", strings.Repeat("x", 129), "newline\nSECRET_ID", "valid-id"}
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_ = diagnosticCall(c, syntheticMetadata(ctx, ids[i%len(ids)]), "ok")
		}(i)
	}
	wg.Wait()
	records := awaitRecords(t, b, 24)
	unique := map[interface{}]bool{}
	for _, r := range records {
		if unique[r["call_id"]] {
			t.Fatal("call ID collision")
		}
		unique[r["call_id"]] = true
	}
	if strings.Contains(b.text(), "SECRET_") || strings.Contains(b.text(), strings.Repeat("x", 129)) {
		t.Fatal("invalid ID logged")
	}
}
func BenchmarkRPCDiagnosticSuccessBookkeeping(b *testing.B) {
	c := NewClient()
	req := c.NewRequest("svc", "Method.Call", &pb.HelloRequest{})
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		o := newCallObservation(ctx, req, time.Second)
		o.finish(ctx, nil, 1, "call")
	}
}

func TestRPCDiagnosticsInheritedDeadlineAndLookupFailure(t *testing.T) {
	t.Run("inherited_deadline", func(t *testing.T) {
		b := captureDiagnostics(t)
		c, _ := newDiagnosticClient(t)
		ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
		defer cancel()
		err := diagnosticCall(c, syntheticMetadata(ctx, "parent-budget"), "wait", client.WithRequestTimeout(time.Millisecond), client.WithRetries(0))
		if err == nil {
			t.Fatal("expected deadline")
		}
		records := awaitRecords(t, b, 2)
		final := recordEvent(t, records, "rpc_call_end")
		if final["parent_has_deadline"] != true || final["configured_timeout_ms"] != float64(1) || final["selected_timeout_ms"].(float64) < 25 || final["remaining_at_start_ms"].(float64) < 25 || final["elapsed_ms"].(float64) < 25 {
			t.Fatalf("parent budget behavior changed: %+v", final)
		}
		if final["parent_context"] != "deadline_exceeded" {
			t.Fatalf("wrong parent state: %+v", final)
		}
	})
	t.Run("lookup_failure", func(t *testing.T) {
		b := captureDiagnostics(t)
		c := NewClient(client.Lookup(func(context.Context, client.Request, client.CallOptions) ([]string, error) {
			return nil, errors.InternalServerError("synthetic", "SECRET_LOOKUP")
		}))
		err := diagnosticCall(c, syntheticMetadata(context.Background(), "lookup-failure"), "ok")
		if err == nil {
			t.Fatal("expected lookup error")
		}
		final := recordEvent(t, awaitRecords(t, b, 1), "rpc_call_end")
		if final["stage"] != "lookup" || final["attempts_scheduled"] != float64(0) {
			t.Fatalf("wrong stage: %+v", final)
		}
		if strings.Contains(b.text(), "SECRET_") {
			t.Fatal("lookup error payload logged")
		}
	})
}

func TestRPCDiagnosticsConcurrentTransportCorrelation(t *testing.T) {
	b := captureDiagnostics(t)
	c, _ := newDiagnosticClient(t)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := "remote_cancel"
			if i%2 != 0 {
				name = "remote_deadline"
			}
			_ = diagnosticCall(c, syntheticMetadata(context.Background(), fmt.Sprintf("network-%d", i)), name, client.WithRetries(0))
		}(i)
	}
	wg.Wait()
	records := awaitRecords(t, b, 32)
	byCall := map[interface{}][]map[string]interface{}{}
	for _, r := range records {
		byCall[r["call_id"]] = append(byCall[r["call_id"]], r)
	}
	if len(byCall) != 16 {
		t.Fatalf("calls merged: %d", len(byCall))
	}
	for _, r := range byCall {
		if len(r) != 2 || r[0]["request_id"] != r[1]["request_id"] {
			t.Fatalf("cross-call diagnostic contamination: %+v", r)
		}
		a := recordEvent(t, r, "rpc_attempt_end")
		var index int
		if _, err := fmt.Sscanf(a["request_id"].(string), "network-%d", &index); err != nil {
			t.Fatal(err)
		}
		want := "Canceled"
		if index%2 != 0 {
			want = "DeadlineExceeded"
		}
		if a["grpc_status"] != want {
			t.Fatalf("wrong originating status: %+v", a)
		}
	}
	if strings.Contains(b.text(), "SECRET_") {
		t.Fatal("sensitive value logged")
	}
}

func TestRPCDiagnosticRecordSizeBound(t *testing.T) {
	b := captureDiagnostics(t)
	c := NewClient()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ctx = metadata.NewContext(ctx, metadata.Metadata{"X-Request-Id": strings.Repeat("r", 128), "Authorization": strings.Repeat("SECRET_", 2000)})
	req := c.NewRequest(strings.Repeat("s", 128), strings.Repeat("e", 256), &pb.HelloRequest{Name: "SECRET_PAYLOAD"})
	if c.Call(ctx, req, &pb.HelloReply{}) == nil {
		t.Fatal("expected cancellation")
	}
	records := awaitRecords(t, b, 1)
	data, err := json.Marshal(records[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > 3072 {
		t.Fatalf("unbounded record: %d bytes", len(data))
	}
	if strings.Contains(b.text(), "SECRET_") {
		t.Fatal("sensitive value logged")
	}
}
