package rpc

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/micro/micro/v3/service/api"
	"github.com/micro/micro/v3/service/logger"
	"github.com/micro/micro/v3/util/rpcdiagnostic"
)

type responseObservation struct {
	http.ResponseWriter
	status   int
	writeErr error
}

func (w *responseObservation) WriteHeader(code int) {
	if w.status == 0 && code >= 200 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseObservation) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(data)
	if err != nil && w.writeErr == nil {
		w.writeErr = err
	}
	return n, err
}

func (w *responseObservation) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *responseObservation) finish(ctx context.Context, r *http.Request, svc *api.Service, started time.Time, stage string) {
	if w.status < 400 && w.writeErr == nil && ctx.Err() == nil {
		return
	}
	f := rpcdiagnostic.Fields("http_rpc_end", started)
	rpcdiagnostic.RequestID(f, strings.Join(r.Header.Values("X-Request-Id"), ","))
	f["http_method"] = rpcdiagnostic.Identifier(r.Method, 16)
	f["http_status"] = w.status
	f["request_context"] = rpcdiagnostic.ContextState(ctx)
	f["write_error_kind"] = rpcdiagnostic.ErrorKind(w.writeErr)
	f["stage"] = stage
	if svc != nil {
		f["service"] = rpcdiagnostic.Identifier(svc.Name, 128)
		if svc.Endpoint != nil {
			f["endpoint"] = rpcdiagnostic.Identifier(svc.Endpoint.Name, 256)
		}
	}
	if d, ok := ctx.Deadline(); ok {
		f["request_deadline"] = d.UTC().Format(time.RFC3339Nano)
		f["remaining_at_start_ms"] = float64(d.Sub(started).Nanoseconds()) / 1e6
	}
	level := logger.WarnLevel
	if w.writeErr != nil {
		level = logger.ErrorLevel
	}
	rpcdiagnostic.Emit(level, f)
}
