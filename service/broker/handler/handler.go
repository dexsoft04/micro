package handler

import (
	"context"

	pb "github.com/micro/micro/v3/proto/broker"
	"github.com/micro/micro/v3/service/auth"
	"github.com/micro/micro/v3/service/broker"
	"github.com/micro/micro/v3/service/context/metadata"
	"github.com/micro/micro/v3/service/errors"
	"github.com/micro/micro/v3/service/logger"
	authns "github.com/micro/micro/v3/util/auth/namespace"
)

type Broker struct{}

func messageNamespace(ctx context.Context, issuer, method string) (string, error) {
	if ns, ok := metadata.Get(ctx, "Micro-Sub-Namespace"); ok && len(ns) > 0 {
		if ns != issuer {
			if err := authns.Authorize(ctx, ns, method); err != nil {
				return "", err
			}
		}
		return ns, nil
	}
	return issuer, nil
}

func (h *Broker) Publish(ctx context.Context, req *pb.PublishRequest, rsp *pb.Empty) error {
	// authorize the request
	acc, ok := auth.AccountFromContext(ctx)
	if !ok {
		return errors.Unauthorized("broker.Broker.Publish", "%s", authns.ErrForbidden.Error())
	}

	// validate the request
	if req.Message == nil {
		return errors.BadRequest("broker.Broker.Publish", "Missing message")
	}

	// ensure the header is not nil
	if req.Message.Header == nil {
		req.Message.Header = map[string]string{}
	}

	// set any headers which aren't already set
	if md, ok := metadata.FromContext(ctx); ok {
		for k, v := range md {
			if _, ok := req.Message.Header[k]; !ok {
				req.Message.Header[k] = v
			}
		}
	}
	ns, err := messageNamespace(ctx, acc.Issuer, "broker.Broker.Publish")
	if err != nil {
		return err
	}

	logger.Debugf("Publishing message to %s topic in the %v namespace", req.Topic, ns)
	err = broker.DefaultBroker.Publish(ns+"."+req.Topic, &broker.Message{
		Header: req.Message.Header,
		Body:   req.Message.Body,
	})
	if err != nil {
		return errors.InternalServerError("broker.Broker.Publish", "%s", err.Error())
	}
	return nil
}

func (h *Broker) Subscribe(ctx context.Context, req *pb.SubscribeRequest, stream pb.Broker_SubscribeStream) error {
	// authorize the request
	acc, ok := auth.AccountFromContext(ctx)
	if !ok {
		return errors.Unauthorized("broker.Broker.Subscribe", "%s", authns.ErrForbidden.Error())
	}
	ns, err := messageNamespace(ctx, acc.Issuer, "broker.Broker.Subscribe")
	if err != nil {
		return err
	}

	if len(req.Topic) == 0 {
		return errors.BadRequest("broker.Broker.Subscribe", "Missing topic")
	}
	errChan := make(chan error, 1)

	// message Broker to stream back messages from broker
	Broker := func(m *broker.Message) error {
		if err := stream.Send(&pb.Message{
			Header: m.Header,
			Body:   m.Body,
		}); err != nil {
			select {
			case errChan <- err:
				return err
			default:
				return err
			}
		}
		return nil
	}

	logger.Debugf("Subscribing to %s topic in namespace %v", req.Topic, ns)
	opts := []broker.SubscribeOption{}
	if len(req.Queue) > 0 {
		opts = append(opts, broker.Queue(req.Queue))
	}
	sub, err := broker.DefaultBroker.Subscribe(ns+"."+req.Topic, Broker, opts...)
	if err != nil {
		return errors.InternalServerError("broker.Broker.Subscribe", "%s", err.Error())
	}
	defer func() {
		logger.Debugf("Unsubscribing from topic %s in namespace %v", req.Topic, ns)
		sub.Unsubscribe()
	}()

	select {
	case <-ctx.Done():
		logger.Debugf("Context done for subscription to topic %s", req.Topic)
		return nil
	case err := <-errChan:
		logger.Debugf("Subscription error for topic %s: %v", req.Topic, err)
		return err
	}
}
