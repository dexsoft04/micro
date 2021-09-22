package websocket

import (
	"context"
	ws "github.com/gorilla/websocket"
	"github.com/micro/micro/v3/service/api"
	hdl "github.com/micro/micro/v3/service/api/handler"
	"github.com/micro/micro/v3/service/client"
	"github.com/micro/micro/v3/service/context/metadata"
	"github.com/micro/micro/v3/service/logger"
	"github.com/micro/micro/v3/service/network/transport"
	"github.com/micro/micro/v3/util/codec/bytes"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"
)

const (
	Handler = "handler"
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
}

type handler struct {
	opts *hdl.Options
	s    *api.Service
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cnn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "failed to upgrade handler", 500)
		return
	}
	socket := &Session{
		conn:    cnn,
		timeout: 0,
	}
	go h.serveConn(socket)
}

func (h *handler) String() string {
	return "handler"
}
func (h *handler) serveConn(sock transport.Socket) {
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
		hdr := make(map[string]string, len(msg.Header))
		for k, v := range msg.Header {
			hdr[k] = v
		}
		ctx := metadata.NewContext(context.Background(), hdr)

		timeout := msg.Header["Timeout"]
		if len(timeout) > 0 {
			if n, err := strconv.ParseInt(timeout, 10, 64); err != nil {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Duration(n))
				defer cancel()
			}
		}
		ct := msg.Header["Content-Type"]
		if !hasCodec(ct, protoCodecs) {
			ct = DefaultContentType
		}

		var request *bytes.Frame
		// if the extracted payload isn't empty lets use it
		if msg.Body != nil {
			request = &bytes.Frame{Data: msg.Body}
		}

		var callOpt []client.CallOption
		if len(msg.Header["Micro-ServiceID"]) > 0 {
			callOpt = append(callOpt, client.WithServerUid(msg.Header["Micro-ServiceID"]))
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
		var response *bytes.Frame
		if err := client.DefaultClient.Call(ctx, req, response, callOpt...); err != nil {
			sock.Send(&transport.Message{
				Header: map[string]string{
					"Content-Type": ct,
					"Micro-Error":  err.Error(),
				},
				Body: rsp,
			})
			continue
		}
		rsp = response.Data
		// write the response
		err := sock.Send(&transport.Message{
			Header: map[string]string{
				"Content-Type": ct,
			},



			Body: rsp,
		})
		if err != nil {
			return
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