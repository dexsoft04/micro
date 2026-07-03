package client

import (
	"context"
	"errors"
	"testing"
)

type fakeClient struct {
	calls int
}

func (f *fakeClient) Init(...Option) error {
	return nil
}

func (f *fakeClient) Options() Options {
	return Options{}
}

func (f *fakeClient) NewMessage(topic string, msg interface{}, opts ...MessageOption) Message {
	return &defaultMessage{topic: topic, payload: msg}
}

func (f *fakeClient) NewRequest(service, endpoint string, req interface{}, reqOpts ...RequestOption) Request {
	return &defaultRequest{service: service, endpoint: endpoint, body: req}
}

func (f *fakeClient) Call(ctx context.Context, req Request, rsp interface{}, opts ...CallOption) error {
	f.calls++
	return nil
}

func (f *fakeClient) Stream(ctx context.Context, req Request, opts ...CallOption) (Stream, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeClient) Publish(ctx context.Context, msg Message, opts ...PublishOption) error {
	return nil
}

func (f *fakeClient) String() string {
	return "fake"
}

func TestDefaultClientProxyDelegatesAfterDefaultReplacement(t *testing.T) {
	orig := DefaultClient
	defer func() {
		DefaultClient = orig
	}()

	DefaultClient = defaultClientProxy{}
	early := DefaultClient

	realClient := &fakeClient{}
	DefaultClient = realClient

	req := early.NewRequest("svc", "Endpoint", map[string]string{"k": "v"})
	if err := early.Call(context.Background(), req, nil); err != nil {
		t.Fatalf("early default client call failed: %v", err)
	}

	if realClient.calls != 1 {
		t.Fatalf("expected call to delegate to replacement client, got %d", realClient.calls)
	}
}
