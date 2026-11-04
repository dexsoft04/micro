package wrapper

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/micro/micro/v3/service/logger"
	"github.com/micro/micro/v3/service/metrics"
	"github.com/micro/micro/v3/service/server"
)

type expectedRejection struct{}

func (expectedRejection) Error() string       { return "session not found" }
func (expectedRejection) ExpectedError() bool { return true }

func TestLogHandlerErrorExpectedRejection(t *testing.T) {
	recorder := &recordingLogger{options: logger.Options{Level: logger.TraceLevel}}
	defer setDefaultLogger(recorder)()
	logHandlerError(expectedRejection{})
	if recorder.errorCount != 0 || recorder.debugCount != 1 {
		t.Fatalf("expected rejection: errors=%d debug=%d", recorder.errorCount, recorder.debugCount)
	}
	logHandlerError(errors.New("session not found"))
	if recorder.errorCount != 1 || recorder.debugCount != 1 {
		t.Fatalf("unclassified error: errors=%d debug=%d", recorder.errorCount, recorder.debugCount)
	}
}

type rejectionRequest struct{ server.Request }

func (rejectionRequest) Service() string  { return "websocket" }
func (rejectionRequest) Endpoint() string { return "Session.Send" }
func (rejectionRequest) Method() string   { return "Session.Send" }

type rejectionMetrics struct {
	metrics.Reporter
	tags metrics.Tags
}

func (r *rejectionMetrics) Timing(_ string, _ time.Duration, tags metrics.Tags) error {
	r.tags = tags
	return nil
}

func TestExpectedRejectionPreservesFailureMetricsAndReturn(t *testing.T) {
	recorder := &recordingLogger{options: logger.Options{Level: logger.TraceLevel}}
	defer setDefaultLogger(recorder)()
	defer setDefaultAuth(&testAuth{})()
	reporter := &rejectionMetrics{}
	previous := metrics.DefaultMetricsReporter
	metrics.DefaultMetricsReporter = reporter
	defer func() { metrics.DefaultMetricsReporter = previous }()
	want := expectedRejection{}
	h := MetricsHandler()(AuthHandler()(func(context.Context, server.Request, interface{}) error { return want }))
	if got := h(context.Background(), rejectionRequest{}, nil); got != want {
		t.Fatalf("handler result changed: %v", got)
	}
	if reporter.tags["result"] != "failure" || reporter.tags["method"] != "Session.Send" {
		t.Fatalf("failure metrics changed: %v", reporter.tags)
	}
	if recorder.errorCount != 0 || recorder.debugCount != 1 {
		t.Fatalf("unexpected log levels: errors=%d debug=%d", recorder.errorCount, recorder.debugCount)
	}
}
