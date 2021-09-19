package websocket

import (
	"bytes"
	"errors"
	"github.com/golang/protobuf/proto"

	ws "github.com/gorilla/websocket"
	"github.com/micro/micro/v3/service/network/transport"
	"strings"
	"time"
	pb "github.com/micro/micro/v3/proto/transport"
)

type webSocket struct {
	conn *ws.Conn
	timeout time.Duration
}

func (w webSocket) Recv(m *transport.Message) error {
	if m == nil {
		return errors.New("message passed in is nil")
	}
	if w.timeout > time.Duration(0) {
		w.conn.SetReadDeadline(time.Now().Add(w.timeout))
	}
	_, body, err := w.conn.ReadMessage()
	if err != nil {
		return err
	}
	var msg *pb.Message

	proto.Unmarshal(body, msg)
	m.Header = msg.Header
	m.Body = msg.Body
	return nil
}

func (w webSocket) Send(m *transport.Message) error {
	if w.timeout > time.Duration(0) {
		w.conn.SetWriteDeadline(time.Now().Add(w.timeout))
	}
	buff := bytes.NewBuffer(m.Body)
	if err := w.conn.WriteMessage(ws.BinaryMessage, buff.Bytes()); err != nil {
		return err
	}
	return nil
}

func (w webSocket) Close() error {
	return w.conn.Close()
}

func (w webSocket) Local() string {
	addr := w.conn.LocalAddr().String()
	return strings.Split(addr, ":")[0]}

func (w webSocket) Remote() string {
	addr := w.conn.RemoteAddr().String()
	return strings.Split(addr, ":")[0]
}