package websocket

import (
	"errors"
	"github.com/golang/protobuf/proto"
	"io"
	"sync"
	"sync/atomic"

	ws "github.com/gorilla/websocket"
	pb "github.com/micro/micro/v3/proto/transport"
	"github.com/micro/micro/v3/service/network/transport"
	"strings"
	"time"
)

var (
	sessionsByID  sync.Map
	sessionsByUID sync.Map
	sessionIDSvc  = newSessionIDService()
)

type sessionIDService struct {
	sid int64
}

func newSessionIDService() *sessionIDService {
	return &sessionIDService{
		sid: 0,
	}
}
func (s *sessionIDService) sessionID() int64 {
	return atomic.AddInt64(&s.sid, 1)
}

type Session struct {
	id      int64
	uid     string
	conn    *ws.Conn
	send    chan *transport.Message
	closed  chan bool
	timeout time.Duration
}

func newSession(conn *ws.Conn) *Session {
	s := &Session{
		id:      sessionIDSvc.sessionID(),
		conn:    conn,
		send:    make(chan *transport.Message, 128),
		closed:  make(chan bool),
		timeout: 5 * time.Second,
	}
	sessionsByID.Store(s.id, s)
	go s.process()
	return s
}
func (s *Session) Recv(m *transport.Message) error {
	if m == nil {
		return errors.New("message passed in is nil")
	}
	if s.timeout > time.Duration(0) {
		s.conn.SetReadDeadline(time.Now().Add(s.timeout))
	}
	_, body, err := s.conn.ReadMessage()
	if err != nil {
		return err
	}
	var msg *pb.Message

	err = proto.Unmarshal(body, msg)
	if err != nil {
		return err
	}
	m.Header = msg.Header
	m.Body = msg.Body
	return nil
}
func (s *Session) Send(m *transport.Message) error {
	select {
	case <-s.closed:
		return io.EOF
	default:
		s.send <- m
	}
	return nil
}
func (s *Session) sendMsg(m *transport.Message) error {
	if s.timeout > time.Duration(0) {
		s.conn.SetWriteDeadline(time.Now().Add(s.timeout))
	}
	msg := &pb.Message{
		Header: m.Header,
		Body:   m.Body,
	}
	body, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	if err := s.conn.WriteMessage(ws.BinaryMessage, body); err != nil {
		return err
	}
	return nil
}
func (s *Session) process() {
	defer func() {
		s.Close()
	}()
	for {
		select {
		case <-s.closed:
			return
		case m := <-s.send:
			if err := s.sendMsg(m); err != nil {
				s.Close()
			}
		}
	}
}
func (s *Session) Close() error {
	select {
	case <-s.closed:
		return nil
	default:
		s.conn.Close()
		sessionsByID.Delete(s.id)
		if len(s.uid) > 0{
			sessionsByUID.Delete(s.uid)
		}

		close(s.closed)
	}
	return nil
}

func (s *Session) Local() string {
	addr := s.conn.LocalAddr().String()
	return strings.Split(addr, ":")[0]
}

func (s *Session) Remote() string {
	addr := s.conn.RemoteAddr().String()
	return strings.Split(addr, ":")[0]
}
