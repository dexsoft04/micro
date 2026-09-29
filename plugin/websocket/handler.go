package websocket

import (
	"context"
	ws "github.com/gorilla/websocket"
	hdl "github.com/micro/micro/v3/service/api/handler"
	"github.com/micro/micro/v3/service/client"
	"github.com/micro/micro/v3/service/context/metadata"
	"github.com/micro/micro/v3/service/errors"
	"github.com/micro/micro/v3/service/logger"
	"github.com/micro/micro/v3/service/network/transport"
	"github.com/micro/micro/v3/service/server"
	"github.com/micro/micro/v3/util/codec/bytes"
	"golang.org/x/net/publicsuffix"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
)

const (
	Handler = "websocket"
)

var (
	DefaultContentType = "application/protobuf"
	// support proto codecs
	protoCodecs = []string{
		"application/grpc",
		"application/grpc+proto",
		"application/proto",
		"application/protobuf",
		"application/proto-rpc",
		"application/octet-stream",
	}
)

var upgrader = ws.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type wsHandler struct {
	opts hdl.Options
}

func requestHeaders(clientHeaders, status map[string]string, sessionID, serverID string) map[string]string {
	headers := make(map[string]string, len(clientHeaders)+len(status)+2)
	for key, value := range clientHeaders {
		key = strings.ToLower(key)
		if strings.HasPrefix(key, "micro-ws-") || strings.HasPrefix(key, "mcb-") {
			continue
		}
		headers[key] = value
	}
	for key, value := range status {
		headers[strings.ToLower(key)] = value
	}
	headers["micro-ws-session-id"] = sessionID
	headers["micro-ws-server-id"] = serverID
	return headers
}

// isSessionControlRequest reports whether a client is trying to reach this gateway's own session RPCs.
func isSessionControlRequest(service, endpoint, gatewayName string) bool {
	return strings.EqualFold(service, gatewayName) && strings.HasPrefix(endpoint, "Session.")
}

func (h *wsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cnn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "failed to upgrade wsHandler", 500)
		return
	}
	domain := domain(r)
	socket := NewSession(cnn, domain)
	go h.serveConn(socket, domain)
}

func (h *wsHandler) String() string {
	return "websocket"
}
func (h *wsHandler) serveConn(sock *Session, domain string) {
	defer func() {
		sock.Close()
		// recover any panics
		if r := recover(); r != nil {
			logger.Error("panic recovered: ", r)
			logger.Error(string(debug.Stack()))
		}
	}()
	for {
		var msg transport.Message
		if err := sock.Recv(&msg); err != nil {
			return
		}
		config := server.DefaultServer.Options()
		serverID := config.Name + "-" + config.Id
		hdr := requestHeaders(msg.Header, sock.GetStatus(), sock.SID(), serverID)
		ctx := metadata.NewContext(context.Background(), hdr)
		ct := msg.Header["Content-Type"]
		if !hasCodec(ct, protoCodecs) {
			ct = DefaultContentType
		}

		request := &bytes.Frame{Data: msg.Body}

		var callOpt []client.CallOption
		if len(domain) > 0 {
			callOpt = append(callOpt, client.WithNetwork(domain))
		}
		if len(msg.Header["Micro-ServiceID"]) > 0 {
			callOpt = append(callOpt, client.WithServerUid(msg.Header["Micro-ServiceID"]))
		}
		if len(msg.Header["Micro-Service"]) == 0 || len(msg.Header["Micro-Endpoint"]) == 0 {
			if err := sock.Send(&transport.Message{}); err != nil {
				return
			}
			continue
		}
		if isSessionControlRequest(msg.Header["Micro-Service"], msg.Header["Micro-Endpoint"], config.Name) {
			if err := sock.Send(&transport.Message{
				Header: map[string]string{
					"Micro-Id":     msg.Header["Micro-Id"],
					"Content-Type": ct,
					"Micro-Error":  errors.Forbidden(config.Name, "session control is not allowed from clients").Error(),
				},
			}); err != nil {
				return
			}
			continue
		}
		// create the request
		req := client.DefaultClient.NewRequest(
			msg.Header["Micro-Service"],
			msg.Header["Micro-Endpoint"],
			request,
			client.WithContentType(ct),
		)
		var rsp []byte
		// make the call
		response := new(bytes.Frame)
		callErr := client.DefaultClient.Call(ctx, req, response, callOpt...)
		if callErr != nil {
			if err := sock.Send(&transport.Message{
				Header: map[string]string{
					"Micro-Id":     msg.Header["Micro-Id"],
					"Content-Type": ct,
					"Micro-Error":  callErr.Error(),
				},
				Body: rsp,
			}); err != nil {
				return
			}
			continue
		}
		if len(msg.Header["Micro-Id"]) > 0 {
			rsp = response.Data
			// write the response
			err := sock.Send(&transport.Message{
				Header: map[string]string{
					"Micro-Id":     msg.Header["Micro-Id"],
					"Content-Type": ct,
				},
				Body: rsp,
			})
			if err != nil {
				return
			}
		}
	}
}
func hasCodec(ct string, codecs []string) bool {
	for _, codec := range codecs {
		if ct == codec {
			return true
		}
	}
	return false
}
func domain(req *http.Request) string {
	// determine the host, e.g. foobar.m3o.app
	host := req.URL.Hostname()
	if len(host) == 0 {
		if h, _, err := net.SplitHostPort(req.Host); err == nil {
			host = h // host does contain a port
		} else if strings.Contains(err.Error(), "missing port in address") {
			host = req.Host // host does not contain a port
		}
	}

	// check for an ip address
	if net.ParseIP(host) != nil {
		return ""
	}

	// check for dev enviroment
	if host == "localhost" || host == "127.0.0.1" {
		return ""
	}

	// extract the top level domain plus one (e.g. 'myapp.com')
	domain, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		logger.Debugf("Unable to extract domain from %v", host)
		return ""
	}

	// there was no subdomain
	if host == domain {
		return ""
	}

	// remove the domain from the host, leaving the subdomain, e.g. "staging.foo.myapp.com" => "staging.foo"
	subdomain := strings.TrimSuffix(host, "."+domain)

	// ignore the API subdomain
	if subdomain == "api" {
		return ""
	}

	// return the reversed subdomain as the namespace, e.g. "staging.foo" => "foo-staging"
	comps := strings.Split(subdomain, ".")
	for i := len(comps)/2 - 1; i >= 0; i-- {
		opp := len(comps) - 1 - i
		comps[i], comps[opp] = comps[opp], comps[i]
	}
	return strings.Join(comps, "-")
}

func NewHandler(opts ...hdl.Option) hdl.Handler {
	options := hdl.NewOptions(opts...)
	return &wsHandler{
		opts: options,
	}
}
