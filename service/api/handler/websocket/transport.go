package websocket

import (
	"context"
	ws "github.com/gorilla/websocket"
	"github.com/micro/micro/v3/service/network/transport"
	"sync"
	"time"
)

type webSocket struct {
	conn *ws.Conn
	recv chan *transport.Message
	send chan *transport.Message

	exit chan bool
	local string
	remote string

	timeout time.Duration
	ctx context.Context
	sync.RWMutex
}

func (w webSocket) Recv(message *transport.Message) error {
	panic("implement me")
}

func (w webSocket) Send(message *transport.Message) error {
	panic("implement me")
}

func (w webSocket) Close() error {
	panic("implement me")
}

func (w webSocket) Local() string {
	panic("implement me")
}

func (w webSocket) Remote() string {
	panic("implement me")
}
