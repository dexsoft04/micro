package grpc

import (
	"context"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/micro/micro/v3/service/client"
	"github.com/micro/micro/v3/service/context/metadata"
	"github.com/micro/micro/v3/service/errors"
	"github.com/micro/micro/v3/service/logger"
	"github.com/micro/micro/v3/util/rpcdiagnostic"
)

// IDs are process-local; correlate with instance labels and request_id.
var diagnosticCallID atomic.Uint64

type callObservation struct {
	failedAttempt     atomic.Bool
	id                uint64
	started           time.Time
	parent            context.Context
	req               client.Request
	configured        time.Duration
	selected          time.Duration
	parentDeadline    time.Time
	parentHasDeadline bool
}

type attemptObservation struct {
	call       *callObservation
	number     int
	started    time.Time
	backoff    time.Duration
	target     string
	stage      string
	grpcStatus string
}
type attemptObservationKey struct{}

func newCallObservation(ctx context.Context, req client.Request, configured time.Duration) callObservation {
	deadline, ok := ctx.Deadline()
	return callObservation{id: diagnosticCallID.Add(1), started: time.Now(), parent: ctx, req: req, configured: configured, parentDeadline: deadline, parentHasDeadline: ok}
}

func (o *callObservation) fields(ctx context.Context, event string, started time.Time, err error) map[string]interface{} {
	f := rpcdiagnostic.Fields(event, started)
	f["call_id"] = strconv.FormatUint(o.id, 16)
	f["service"] = rpcdiagnostic.Identifier(o.req.Service(), 128)
	f["endpoint"] = rpcdiagnostic.Identifier(o.req.Endpoint(), 256)
	id, _ := metadata.Get(o.parent, "X-Request-Id")
	rpcdiagnostic.RequestID(f, id)
	f["parent_context"] = rpcdiagnostic.ContextState(o.parent)
	f["call_context"] = rpcdiagnostic.ContextState(ctx)
	f["configured_timeout_ms"] = float64(o.configured.Nanoseconds()) / 1e6
	f["selected_timeout_ms"] = float64(o.selected.Nanoseconds()) / 1e6
	f["parent_has_deadline"] = o.parentHasDeadline
	if o.parentHasDeadline {
		f["parent_deadline"] = o.parentDeadline.UTC().Format(time.RFC3339Nano)
		f["parent_remaining_at_start_ms"] = float64(o.parentDeadline.Sub(o.started).Nanoseconds()) / 1e6
	}
	if d, ok := ctx.Deadline(); ok {
		f["effective_deadline"] = d.UTC().Format(time.RFC3339Nano)
		f["remaining_at_start_ms"] = float64(d.Sub(started).Nanoseconds()) / 1e6
		f["remaining_at_end_ms"] = float64(time.Until(d).Nanoseconds()) / 1e6
	}
	f["error_kind"] = rpcdiagnostic.ErrorKind(err)
	f["outcome"] = "success"
	if err != nil {
		f["outcome"] = "error"
	}
	if e, ok := err.(*errors.Error); ok {
		f["micro_code"] = e.Code
	}
	return f
}

func (o *callObservation) finish(ctx context.Context, err error, attempts int, stage string) {
	if err == nil && ctx.Err() == nil && !o.failedAttempt.Load() {
		return
	}
	f := o.fields(ctx, "rpc_call_end", o.started, err)
	f["attempts_scheduled"] = attempts
	f["stage"] = stage
	rpcdiagnostic.Emit(logger.WarnLevel, f)
}

func (o *attemptObservation) finish(ctx context.Context, err error) {
	if err != nil {
		o.call.failedAttempt.Store(true)
	}
	if err == nil && ctx.Err() == nil && !o.call.failedAttempt.Load() {
		return
	}
	f := o.call.fields(ctx, "rpc_attempt_end", o.started, err)
	f["attempt"] = o.number
	f["stage"] = o.stage
	f["backoff_ms"] = float64(o.backoff.Nanoseconds()) / 1e6
	f["target"] = rpcdiagnostic.Target(o.target)
	f["grpc_status"] = o.grpcStatus
	rpcdiagnostic.Emit(logger.WarnLevel, f)
}
