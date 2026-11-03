package rpcdiagnostic

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

func TestAllowedIdentifiersAndTargets(t *testing.T) {
	for _, value := range []string{"", strings.Repeat("x", 129), "bad\nSECRET", `bad"SECRET`, "token=SECRET"} {
		f := map[string]interface{}{}
		RequestID(f, value)
		if f["request_id"] != nil {
			t.Fatalf("unsafe ID accepted: %q", value)
		}
	}
	for _, address := range []string{"http://user:SECRET@host:80/path?token=SECRET", "host:0", "host:99999", "/tmp/SECRET", "/tmp/SECRET:8000", "private/path:80", "host\nSECRET:80"} {
		if Target(address) != "" {
			t.Fatalf("unsafe address accepted: %q", address)
		}
	}
	for _, address := range []string{"127.0.0.1:8000", "service.internal:80", "[::1]:8000"} {
		if Target(address) != address {
			t.Fatalf("valid address lost: %q", address)
		}
	}
	if ErrorKind(errors.New("SECRET")) != "other" || ErrorKind(context.Canceled) != "canceled" || ErrorKind(net.ErrClosed) != "connection_closed" {
		t.Fatal("unsafe or wrong error classification")
	}
}
