package handler

import (
	"context"
	"github.com/micro/micro/v3/plugin/websocket"
	"github.com/micro/micro/v3/service/logger"
	"github.com/micro/micro/v3/service/network/transport"
	pb "github.com/wolfplus2048/mcbeam-plugins/ws_session/v3/proto"
)

type Handler struct {
}

func (h *Handler) Bind(ctx context.Context, req *pb.SessionStatus, rsp *pb.EmptyResponse) error {
	s := websocket.GetSessionBySID(req.Sid)
	if s == nil {
		logger.Debugf("Bind session, not found, sid:%s Status:%+v", req.Sid, req.Status)
		return websocket.ErrSessionNotFound
	}
	s.UpdateStatus(req.Status)
	logger.Debugf("Bind session sid:%s Status:%+v", req.Sid, req.Status)
	return nil
}

func (h *Handler) Send(ctx context.Context, m *pb.Message, rsp *pb.EmptyResponse) error {
	s := websocket.GetSessionBySID(m.Sid)
	if s == nil {
		logger.Debugf("Send sid:%s route:%s err:session not found", m.Sid, m.Route)

		return websocket.ErrSessionNotFound
	}
	msg := &transport.Message{
		Header: make(map[string]string),
		Body:   m.Body,
	}
	msg.Header["Micro-Route"] = m.Route
	err := s.Send(msg)
	logger.Debugf("Send sid:%s route:%s err:%v", m.Sid, m.Route, err)

	return err
}

func (h *Handler) Kick(ctx context.Context, req *pb.KickRequest, rsp *pb.EmptyResponse) error {
	s := websocket.GetSessionBySID(req.Sid)
	if s == nil {
		logger.Debugf("Kick sid:%s err:session not found", req.Sid)
		return websocket.ErrSessionNotFound
	}
	logger.Debugf("Kick sid:%s", req.Sid)
	return s.Close()
}
