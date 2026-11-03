// Package rpcdiagnostic provides bounded, payload-free RPC diagnostic fields.
package rpcdiagnostic

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/micro/micro/v3/service/logger"
)

// Identifier excludes control characters, delimiters and unbounded input.
func Identifier(value string, limit int) string {
	if len(value) == 0 || len(value) > limit {
		return ""
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._:-/", c)) {
			return ""
		}
	}
	return value
}

func RequestID(fields map[string]interface{}, value string) {
	state := "missing"
	if value != "" {
		state = "invalid"
		if id := Identifier(value, 128); id != "" {
			fields["request_id"] = id
			state = "present"
		}
	}
	fields["request_id_state"] = state
}

func ContextState(ctx context.Context) string {
	switch ctx.Err() {
	case nil:
		return "active"
	case context.Canceled:
		return "canceled"
	case context.DeadlineExceeded:
		return "deadline_exceeded"
	default:
		return "other"
	}
}

func ErrorKind(err error) string {
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, net.ErrClosed):
		return "connection_closed"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	return "other"
}

// Target never includes credentials, URLs, query parameters or unix paths.
func Target(address string) string {
	host, port, err := net.SplitHostPort(address)
	if err != nil || Identifier(host, 253) == "" || strings.Contains(host, "/") {
		return ""
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return ""
	}
	return net.JoinHostPort(host, port)
}

func Fields(event string, started time.Time) map[string]interface{} {
	ended := time.Now()
	return map[string]interface{}{"event": event, "started_at": started.UTC().Format(time.RFC3339Nano), "ended_at": ended.UTC().Format(time.RFC3339Nano), "elapsed_ms": float64(ended.Sub(started).Nanoseconds()) / 1e6}
}

func Emit(level logger.Level, fields map[string]interface{}) {
	// A single JSON message avoids mutating the shared logger's Fields state.
	if !logger.V(level, logger.DefaultLogger) {
		return
	}
	data, err := json.Marshal(fields)
	if err == nil {
		logger.Log(level, "RPC diagnostic ", string(data))
	}
}
