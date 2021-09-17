package websocket

import (
	"github.com/micro/micro/v3/util/codec"
)

func getHeader(hdr string, md map[string]string) string {
	if hd := md[hdr]; len(hd) > 0 {
		return hd
	}
	return md["X-"+hdr]
}

func getHeaders(m *codec.Message) {
	set := func(v, hdr string) string {
		if len(v) > 0 {
			return v
		}
		return m.Header[hdr]
	}

	m.Id = set(m.Id, "Micro-Id")
	m.Error = set(m.Error, "Micro-Error")
	m.Endpoint = set(m.Endpoint, "Micro-Endpoint")
	m.Method = set(m.Method, "Micro-Method")
	m.Target = set(m.Target, "Micro-Service")

	// TODO: remove this cruft
	if len(m.Endpoint) == 0 {
		m.Endpoint = m.Method
	}
}

func setHeaders(m, r *codec.Message) {
	set := func(hdr, v string) {
		if len(v) == 0 {
			return
		}
		m.Header[hdr] = v
		m.Header["X-"+hdr] = v
	}

	// set headers
	set("Micro-Id", r.Id)
	set("Micro-Service", r.Target)
	set("Micro-Method", r.Method)
	set("Micro-Endpoint", r.Endpoint)
	set("Micro-Error", r.Error)
}
