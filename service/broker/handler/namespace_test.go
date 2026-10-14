package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	pb "github.com/micro/micro/v3/proto/broker"
	"github.com/micro/micro/v3/service/auth"
	"github.com/micro/micro/v3/service/broker"
	"github.com/micro/micro/v3/service/broker/memory"
	"github.com/micro/micro/v3/service/context/metadata"
	microerrors "github.com/micro/micro/v3/service/errors"
)

type readyBroker struct {
	broker.Broker
	ready    chan struct{}
	writeErr error
}

func (b *readyBroker) Subscribe(topic string, h broker.Handler, opts ...broker.SubscribeOption) (broker.Subscriber, error) {
	sub, err := b.Broker.Subscribe(topic, h, opts...)
	if err == nil {
		b.ready <- struct{}{}
	}
	return sub, err
}

func (b *readyBroker) Publish(topic string, m *broker.Message, opts ...broker.PublishOption) error {
	if b.writeErr != nil {
		return b.writeErr
	}
	return b.Broker.Publish(topic, m, opts...)
}

type messageStream struct {
	ctx      context.Context
	messages chan *pb.Message
}

func (s *messageStream) Context() context.Context  { return s.ctx }
func (s *messageStream) SendMsg(interface{}) error { return nil }
func (s *messageStream) RecvMsg(interface{}) error { return nil }
func (s *messageStream) Close() error              { return nil }
func (s *messageStream) Send(m *pb.Message) error  { s.messages <- m; return nil }

func testBroker(t *testing.T) *readyBroker {
	t.Helper()
	b := &readyBroker{Broker: memory.NewBroker(), ready: make(chan struct{}, 2)}
	if err := b.Connect(); err != nil {
		t.Fatal(err)
	}
	original := broker.DefaultBroker
	broker.DefaultBroker = b
	t.Cleanup(func() { _ = b.Disconnect(); broker.DefaultBroker = original })
	return b
}

func TestPublishSubscribeNamespaceIsolation(t *testing.T) {
	for _, tc := range []struct {
		name, issuer, override, target string
		present, bodyOverride          bool
	}{
		{name: "issuer fallback", issuer: "micro", target: "micro"},
		{name: "cross namespace", issuer: "micro", override: "igaoshou", present: true, target: "igaoshou"},
		{name: "empty override", issuer: "micro", present: true, target: "micro"},
		{name: "same issuer override", issuer: "igaoshou", override: "igaoshou", present: true, target: "igaoshou"},
		{name: "business issuer fallback", issuer: "igaoshou", target: "igaoshou"},
		{name: "message headers do not route", issuer: "micro", target: "micro", bodyOverride: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := testBroker(t)
			h := &Broker{}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			streams := map[string]*messageStream{}
			done := make(chan error, 2)
			for _, ns := range []string{"micro", "igaoshou"} {
				subctx := auth.ContextWithAccount(ctx, &auth.Account{Issuer: ns})
				if tc.name == "cross namespace" && ns == tc.target {
					subctx = auth.ContextWithAccount(ctx, &auth.Account{Issuer: "micro", Type: "service", Scopes: []string{"service"}})
					subctx = metadata.Set(subctx, "Micro-Sub-Namespace", ns)
				}
				stream := &messageStream{ctx: subctx, messages: make(chan *pb.Message, 2)}
				streams[ns] = stream
				go func() { done <- h.Subscribe(subctx, &pb.SubscribeRequest{Topic: "session.close"}, stream) }()
			}
			t.Cleanup(func() {
				cancel()
				for range 2 {
					select {
					case err := <-done:
						if err != nil {
							t.Error(err)
						}
					case <-time.After(time.Second):
						t.Error("subscription did not exit")
					}
				}
			})
			for range 2 {
				select {
				case <-b.ready:
				case <-time.After(time.Second):
					t.Fatal("subscription not ready")
				}
			}
			pubctx := auth.ContextWithAccount(ctx, &auth.Account{Issuer: tc.issuer, Type: "service", Scopes: []string{"service"}})
			pubctx = metadata.Set(pubctx, "Trace-Id", "namespace-regression")
			if tc.present {
				pubctx = metadata.MergeContext(pubctx, map[string]string{"Micro-Sub-Namespace": tc.override}, true)
			}
			headers := map[string]string{"Preserved": "original"}
			if tc.bodyOverride {
				headers["Micro-Sub-Namespace"] = "igaoshou"
			}
			if err := h.Publish(pubctx, &pb.PublishRequest{Topic: "session.close", Message: &pb.Message{Header: headers, Body: []byte("session-close-event")}}, &pb.Empty{}); err != nil {
				t.Fatal(err)
			}
			select {
			case msg := <-streams[tc.target].messages:
				if string(msg.Body) != "session-close-event" || msg.Header["Preserved"] != "original" || msg.Header["Trace-Id"] != "namespace-regression" {
					t.Fatalf("message changed: %+v", msg)
				}
			case <-time.After(time.Second):
				t.Fatal("target namespace did not receive the message")
			}
			for ns, stream := range streams {
				if ns != tc.target {
					select {
					case <-stream.messages:
						t.Fatal("message leaked to another namespace")
					default:
					}
				}
			}
		})
	}
}

func TestPublishPreservesFailureBoundaries(t *testing.T) {
	b := testBroker(t)
	h := &Broker{}
	valid := &pb.PublishRequest{Topic: "session.close", Message: &pb.Message{Body: []byte("event")}}
	if err := h.Publish(context.Background(), valid, &pb.Empty{}); microerrors.FromError(err).Code != 401 {
		t.Fatalf("unauthenticated publish: %v", err)
	}
	ctx := auth.ContextWithAccount(context.Background(), &auth.Account{Issuer: "micro"})
	if err := h.Publish(ctx, &pb.PublishRequest{Topic: "session.close"}, &pb.Empty{}); microerrors.FromError(err).Code != 400 {
		t.Fatalf("missing message: %v", err)
	}
	b.writeErr = errors.New("test broker unavailable")
	if err := h.Publish(ctx, valid, &pb.Empty{}); microerrors.FromError(err).Code != 500 {
		t.Fatalf("broker error reported as success: %v", err)
	}
}

func TestCrossNamespaceAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name    string
		account *auth.Account
		allowed bool
	}{
		{"platform service", &auth.Account{Issuer: "micro", Type: "service", Scopes: []string{"service"}}, true},
		{"platform admin", &auth.Account{Issuer: "micro", Type: "user", Scopes: []string{"admin"}}, true},
		{"platform user", &auth.Account{Issuer: "micro", Type: "user"}, false},
		{"platform service without scope", &auth.Account{Issuer: "micro", Type: "service"}, false},
		{"other tenant service", &auth.Account{Issuer: "other", Type: "service", Scopes: []string{"service"}}, false},
		{"other tenant admin", &auth.Account{Issuer: "other", Type: "user", Scopes: []string{"admin"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := testBroker(t)
			b.writeErr = errors.New("backend reached")
			base, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			ctx := auth.ContextWithAccount(base, tc.account)
			ctx = metadata.Set(ctx, "Micro-Sub-Namespace", "igaoshou")
			err := (&Broker{}).Publish(ctx, &pb.PublishRequest{Topic: "session.close", Message: &pb.Message{}}, &pb.Empty{})
			want := int32(403)
			if tc.allowed {
				want = 500
			}
			if microerrors.FromError(err).Code != want {
				t.Fatalf("publish authorization: %v", err)
			}
			if !tc.allowed {
				err = (&Broker{}).Subscribe(ctx, &pb.SubscribeRequest{Topic: "session.close"}, &messageStream{ctx: ctx})
				if microerrors.FromError(err).Code != 403 {
					t.Fatalf("subscribe authorization: %v", err)
				}
				if len(b.ready) != 0 {
					t.Fatal("unauthorized subscription reached backend")
				}
			}
		})
	}
}
