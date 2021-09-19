package websocket

import (
	"context"
	ws "github.com/gorilla/websocket"
	"github.com/micro/micro/v3/service/api"
	"github.com/micro/micro/v3/service/api/handler"
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
	Handler = "websocket"
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
		timeout: 0,
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

		timeout := msg.Header["Timeout"]
		if len(timeout) > 0 {
			if n, err := strconv.ParseInt(timeout, 10, 64); err != nil {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Duration(n))
				defer cancel()
			}
		}
		cf := getHeader("Content-Type", msg.Header)
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
			getHeader("Micro-Service", msg.Header),
			getHeader("Micro-Endpoint", msg.Header),
			request,
			client.WithContentType(cf),
		)
		var rsp []byte
		// make the call
		var response *bytes.Frame
		if err := client.DefaultClient.Call(ctx, req, response, callOpt...); err != nil {
			sock.Send(&transport.Message{
				Header: map[string]string{
					"Content-Type": cf,
					"Micro-Error":  err.Error(),
				},
				Body: rsp,
			})
			continue
		}
		rsp = response.Data
		// write the response
		sock.Send(&transport.Message{
			Header: map[string]string{
				"Content-Type": cf,
			},
			Body: rsp,
		})
	}
}
