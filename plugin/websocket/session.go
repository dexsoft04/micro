package websocket

import (
	"errors"
	"github.com/golang/protobuf/proto"
	"github.com/google/uuid"
	ws "github.com/gorilla/websocket"
	pb "github.com/micro/micro/v3/proto/transport"
	"github.com/micro/micro/v3/service/logger"
	"github.com/micro/micro/v3/service/network/transport"
	"io"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	sessionsBySID         sync.Map
	sessionCloseCallbacks = make([]func(s *Session), 0)
	SessionCount          int64
	ErrSessionNotFound    = errors.New("session not found")
)

func GetSessionBySID(sid string) *Session {
	// TODO: Block this operation in backend servers
	if val, ok := sessionsBySID.Load(sid); ok {
		return val.(*Session)
	}
	return nil
}
func OnSessionClose(fn func(session *Session)) {
	sf := reflect.ValueOf(fn)
	for _, f := range sessionCloseCallbacks {
		if reflect.ValueOf(f).Pointer() == sf.Pointer() {
			return
		}
	}
	sessionCloseCallbacks = append(sessionCloseCallbacks, fn)
}

type Session struct {
	sid     string
	conn    *ws.Conn
	send    chan *transport.Message
	closed  chan bool
	timeout time.Duration
	domain  string
	status  map[string]string
	locker  sync.RWMutex
}

func NewSession(conn *ws.Conn, domain string) *Session {
	s := &Session{
		sid:     uuid.New().String(),
		conn:    conn,
		send:    make(chan *transport.Message, 128),
		closed:  make(chan bool),
		timeout: 5 * time.Second,
		domain:  domain,
		status:  make(map[string]string),
	}
	atomic.AddInt64(&SessionCount, 1)
	sessionsBySID.Store(s.sid, s)
	go s.process()
	return s
}
func (s *Session) SID() string {
	return s.sid
}
func (s *Session) GetDomain() string {
	return s.domain
}
func (s *Session) UpdateStatus(status map[string]string) {
	s.locker.Lock()
	defer s.locker.Unlock()
	for k, v := range status {
		if v == "" {
			delete(s.status, k)
		}
		s.status[k] = v
	}
}
func (s *Session) GetStatus() map[string]string {
	s.locker.RLock()
	defer s.locker.RUnlock()

	return s.status
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
	msg := new(pb.Message)

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
				logger.Errorf("sendMsg, err:%s sid:%s header:%+v", err.Error(), s.sid, m.Header)
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
		for _, cb := range sessionCloseCallbacks {
			cb(s)
		}
		atomic.AddInt64(&SessionCount, -1)
		s.conn.Close()
		sessionsBySID.Delete(s.sid)
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
