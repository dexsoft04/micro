package client

import (
	"context"

	"github.com/micro/micro/v3/util/codec"
)

type defaultClientProxy struct{}

type defaultMessage struct {
	topic       string
	payload     interface{}
	contentType string
}

type defaultRequest struct {
	service     string
	endpoint    string
	body        interface{}
	contentType string
}

func (d defaultClientProxy) delegate() (Client, error) {
	if DefaultClient == nil {
		return nil, errDefaultClientNotConfigured
	}
	if _, ok := DefaultClient.(defaultClientProxy); ok {
		return nil, errDefaultClientNotConfigured
	}
	return DefaultClient, nil
}

func (d defaultClientProxy) Init(opts ...Option) error {
	c, err := d.delegate()
	if err != nil {
		return err
	}
	return c.Init(opts...)
}

func (d defaultClientProxy) Options() Options {
	c, err := d.delegate()
	if err != nil {
		return Options{}
	}
	return c.Options()
}

func (d defaultClientProxy) NewMessage(topic string, msg interface{}, opts ...MessageOption) Message {
	c, err := d.delegate()
	if err != nil {
		m := &defaultMessage{topic: topic, payload: msg}
		options := MessageOptions{}
		for _, o := range opts {
			o(&options)
		}
		m.contentType = options.ContentType
		return m
	}
	return c.NewMessage(topic, msg, opts...)
}

func (d defaultClientProxy) NewRequest(service, endpoint string, req interface{}, opts ...RequestOption) Request {
	c, err := d.delegate()
	if err != nil {
		r := &defaultRequest{service: service, endpoint: endpoint, body: req}
		options := RequestOptions{}
		for _, o := range opts {
			o(&options)
		}
		r.contentType = options.ContentType
		return r
	}
	return c.NewRequest(service, endpoint, req, opts...)
}

func (d defaultClientProxy) Call(ctx context.Context, req Request, rsp interface{}, opts ...CallOption) error {
	c, err := d.delegate()
	if err != nil {
		return err
	}
	return c.Call(ctx, req, rsp, opts...)
}

func (d defaultClientProxy) Stream(ctx context.Context, req Request, opts ...CallOption) (Stream, error) {
	c, err := d.delegate()
	if err != nil {
		return nil, err
	}
	return c.Stream(ctx, req, opts...)
}

func (d defaultClientProxy) Publish(ctx context.Context, msg Message, opts ...PublishOption) error {
	c, err := d.delegate()
	if err != nil {
		return err
	}
	return c.Publish(ctx, msg, opts...)
}

func (d defaultClientProxy) String() string {
	c, err := d.delegate()
	if err != nil {
		return "default"
	}
	return c.String()
}

func (m *defaultMessage) Topic() string {
	return m.topic
}

func (m *defaultMessage) Payload() interface{} {
	return m.payload
}

func (m *defaultMessage) ContentType() string {
	return m.contentType
}

func (r *defaultRequest) Service() string {
	return r.service
}

func (r *defaultRequest) Method() string {
	return r.endpoint
}

func (r *defaultRequest) Endpoint() string {
	return r.endpoint
}

func (r *defaultRequest) ContentType() string {
	return r.contentType
}

func (r *defaultRequest) Body() interface{} {
	return r.body
}

func (r *defaultRequest) Codec() codec.Writer {
	return nil
}

func (r *defaultRequest) Stream() bool {
	return false
}
