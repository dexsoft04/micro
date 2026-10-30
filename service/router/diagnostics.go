package router

import "github.com/micro/micro/v3/service/context/metadata"

// LookupDiagnosticFields excludes payloads and authentication metadata.
func LookupDiagnosticFields(q LookupOptions) map[string]interface{} {
	fields := map[string]interface{}{
		"target_uid": q.Uid, "target_network": q.Network,
		"target_address": q.Address, "target_gateway": q.Gateway,
		"target_router": q.Router, "target_link": q.Link,
	}
	if q.Context != nil {
		for _, key := range []string{"Micro-Trace-Id", "Micro-Span-Id", "Uber-Trace-Id", "X-Request-Id"} {
			if value, ok := metadata.Get(q.Context, key); ok {
				fields[key] = value
			}
		}
	}
	return fields
}
