package grpc_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	wshandler "github.com/micro/micro/v3/plugin/websocket/handler"
	bmemory "github.com/micro/micro/v3/service/broker/memory"
	"github.com/micro/micro/v3/service/logger"
	rmemory "github.com/micro/micro/v3/service/registry/memory"
	"github.com/micro/micro/v3/service/server"
	gsrv "github.com/micro/micro/v3/service/server/grpc"
	pb "github.com/micro/micro/v3/service/server/grpc/proto"
	wspb "github.com/wolfplus2048/mcbeam-plugins/ws_session/v3/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type unexpectedSessionHandler struct{ testServer }

func (*unexpectedSessionHandler) Call(context.Context, *pb.Request, *pb.Response) error {
	return errors.New("session not found")
}

type rejectionLog struct {
	level   logger.Level
	message string
}

type rejectionLogger struct {
	logger.Logger
	mu      sync.Mutex
	entries []rejectionLog
}

func (l *rejectionLogger) Fields(map[string]interface{}) logger.Logger { return l }
func (l *rejectionLogger) Log(level logger.Level, v ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, rejectionLog{level, fmt.Sprint(v...)})
}
func (l *rejectionLogger) Logf(level logger.Level, format string, v ...interface{}) {
	l.Log(level, fmt.Sprintf(format, v...))
}
func (l *rejectionLogger) count(level logger.Level, fragment string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, entry := range l.entries {
		if entry.level == level && strings.Contains(entry.message, fragment) {
			n++
		}
	}
	return n
}

func TestMissingSessionRPCRemainsFailureWithoutErrorLog(t *testing.T) {
	// The server can log asynchronously after Stop returns. Isolate the global
	// logger in a child process instead of racing a restore with that shutdown.
	const helperEnv = "MICRO_EXPECTED_SESSION_LOG_TEST"
	if os.Getenv(helperEnv) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMissingSessionRPCRemainsFailureWithoutErrorLog$", "-test.timeout=10s")
		cmd.Env = append(os.Environ(), helperEnv+"=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("session logging subprocess: %v\n%s", err, output)
		}
		return
	}
	recorder := &rejectionLogger{Logger: logger.NewLogger(logger.WithLevel(logger.TraceLevel))}
	logger.DefaultLogger = recorder
	s := gsrv.NewServer(server.Name("session-logging-test"), server.Address("127.0.0.1:0"),
		server.Registry(rmemory.NewRegistry()), server.Broker(bmemory.NewBroker()))
	if err := wspb.RegisterSessionHandler(s, &wshandler.Handler{}); err != nil {
		t.Fatal(err)
	}
	if err := pb.RegisterTestHandler(s, &unexpectedSessionHandler{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	conn, err := grpc.Dial(s.Options().Address, grpc.WithInsecure())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const sid = "missing-session-logging-test"
	for _, tt := range []struct {
		method  string
		request interface{}
	}{
		{"Send", &wspb.Message{Sid: sid, Route: "test.route"}},
		{"Bind", &wspb.SessionStatus{Sid: sid}},
		{"Kick", &wspb.KickRequest{Sid: sid}},
	} {
		err := conn.Invoke(ctx, "/Session/"+tt.method, tt.request, &wspb.EmptyResponse{})
		if err == nil || status.Code(err) != codes.Unknown || status.Convert(err).Message() != "session not found" {
			t.Fatalf("%s changed wire error: %v", tt.method, err)
		}
	}
	if got := recorder.count(logger.ErrorLevel, "session not found"); got != 0 {
		t.Fatalf("expected rejection logged %d errors", got)
	}
	if got := recorder.count(logger.DebugLevel, "grpc handler rejected request"); got != 3 {
		t.Fatalf("classified debug logs=%d, want 3", got)
	}
	if got := recorder.count(logger.DebugLevel, sid); got != 3 {
		t.Fatalf("SID diagnostic logs=%d, want 3", got)
	}
	err = conn.Invoke(ctx, "/test.Test/Call", &pb.Request{}, &pb.Response{})
	if err == nil || status.Code(err) != codes.Unknown || status.Convert(err).Message() != "session not found" {
		t.Fatalf("ordinary error changed: %v", err)
	}
	if got := recorder.count(logger.ErrorLevel, "grpc handler got error"); got != 1 {
		t.Fatalf("ordinary error logs=%d, want 1", got)
	}
}
