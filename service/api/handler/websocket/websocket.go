package websocket

import (
	"context"
	"fmt"
	ws "github.com/gorilla/websocket"
	"github.com/micro/micro/v3/service/api"
	"github.com/micro/micro/v3/service/api/handler"
	"github.com/micro/micro/v3/service/context/metadata"
	"github.com/micro/micro/v3/service/logger"
	"github.com/micro/micro/v3/service/network/transport"
	"github.com/micro/micro/v3/util/codec"
	raw "github.com/micro/micro/v3/util/codec/bytes"
	"github.com/micro/micro/v3/util/codec/grpc"
	"github.com/micro/micro/v3/util/codec/json"
	"github.com/micro/micro/v3/util/codec/jsonrpc"
	"github.com/micro/micro/v3/util/codec/proto"
	"github.com/micro/micro/v3/util/codec/protorpc"
	"net/http"
	"runtime/debug"
	"strconv"
	"sync"
	"time"
)

const (
	Handler = "websocket"
)
var (

	DefaultContentType = "application/protobuf"

	DefaultCodecs = map[string]codec.NewCodec{
		"application/grpc":         grpc.NewCodec,
		"application/grpc+json":    grpc.NewCodec,
		"application/grpc+proto":   grpc.NewCodec,
		"application/json":         json.NewCodec,
		"application/json-rpc":     jsonrpc.NewCodec,
		"application/protobuf":     proto.NewCodec,
		"application/proto-rpc":    protorpc.NewCodec,
		"application/octet-stream": raw.NewCodec,
	}
)
var upgrader = ws.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type websocket struct {
	opts *handler.Options
	s    *api.Service
}

func (ws *websocket) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cnn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "failed to upgrade websocket", 500)
		return
	}

	socket := &webSocket{
		conn:    cnn,
		recv:    make(chan *transport.Message),
		send:    make(chan *transport.Message),
		exit:    make(chan bool),
		local:   r.Host,
		remote:  r.RemoteAddr,
		timeout: 5 * time.Second,
		ctx:     context.Background(),
		RWMutex: sync.RWMutex{},
	}
	go ws.serveConn(socket)
}

func (ws *websocket) String() string {

	return "websocket"
}
func (ws *websocket) serveConn(sock transport.Socket) {
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

		timeout := hdr["Timeout"]
		if len(timeout) > 0 {
			if n, err := strconv.ParseInt(timeout, 10, 64); err != nil {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Duration(n))
				defer cancel()
			}
		}

		ct := msg.Header["Content-Type"]
		if len(ct) == 0 {
			msg.Header["Content-Type"] = DefaultContentType
			ct = DefaultContentType
		}

		var cf codec.NewCodec
		var err error
		// try get a new codec
		if cf, err = ws.newCodec(ct); err != nil {
			// no codec found so send back an error
			sock.Send(&transport.Message{
				Header: map[string]string{
					"Content-Type": "text/plain",
				},
				Body: []byte(err.Error()),
			})
			return
		}

	}
}

func (ws *websocket) newCodec(contentType string) (codec.NewCodec, error) {
	if cf, ok := DefaultCodecs[contentType]; ok {
		return cf, nil
	}
	return nil, fmt.Errorf("Unsupported Content-Type: %s", contentType)
}