package websocket

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	ws "github.com/gorilla/websocket"
	pb "github.com/micro/micro/v3/proto/transport"
	"github.com/micro/micro/v3/service/client"
	"github.com/micro/micro/v3/service/server"
	grpcserver "github.com/micro/micro/v3/service/server/grpc"
	"github.com/micro/micro/v3/util/codec"
	"github.com/micro/micro/v3/util/codec/bytes"
)

type testRequest struct {
	service     string
	endpoint    string
	contentType string
	body        interface{}
}

func (r *testRequest) Service() string     { return r.service }
func (r *testRequest) Method() string      { return r.endpoint }
func (r *testRequest) Endpoint() string    { return r.endpoint }
func (r *testRequest) ContentType() string { return r.contentType }
func (r *testRequest) Body() interface{}   { return r.body }
func (*testRequest) Codec() codec.Writer   { return nil }
func (*testRequest) Stream() bool          { return false }

type testClient struct{}

func (*testClient) Init(...client.Option) error { return nil }
func (*testClient) Options() client.Options     { return client.Options{} }
func (*testClient) NewMessage(string, interface{}, ...client.MessageOption) client.Message {
	return nil
}
func (*testClient) NewRequest(service, endpoint string, body interface{}, _ ...client.RequestOption) client.Request {
	return &testRequest{service: service, endpoint: endpoint, contentType: "application/protobuf", body: body}
}
func (*testClient) Call(_ context.Context, req client.Request, rsp interface{}, _ ...client.CallOption) error {
	request, ok := req.Body().(*bytes.Frame)
	if !ok || request == nil {
		return errors.New("nil request frame")
	}
	response, ok := rsp.(*bytes.Frame)
	if !ok || response == nil {
		return errors.New("nil response frame")
	}
	response.Data = []byte("ok")
	return nil
}
func (*testClient) Stream(context.Context, client.Request, ...client.CallOption) (client.Stream, error) {
	return nil, errors.New("not implemented")
}
func (*testClient) Publish(context.Context, client.Message, ...client.PublishOption) error {
	return nil
}
func (*testClient) String() string { return "test" }

func TestHandlerForwardsEmptyRequestBody(t *testing.T) {
	originalClient := client.DefaultClient
	originalServer := server.DefaultServer
	client.DefaultClient = &testClient{}
	server.DefaultServer = grpcserver.NewServer()
	defer func() {
		client.DefaultClient = originalClient
		server.DefaultServer = originalServer
	}()

	server := httptest.NewServer(NewHandler())
	defer server.Close()

	conn, _, err := ws.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	request, err := proto.Marshal(&pb.Message{Header: map[string]string{
		"Content-Type":   "application/protobuf",
		"Micro-Endpoint": "Game.EmptyRequest",
		"Micro-Id":       "request-1",
		"Micro-Service":  "game",
	}})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if err := conn.WriteMessage(ws.BinaryMessage, request); err != nil {
		t.Fatalf("write request: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	_, body, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	response := new(pb.Message)
	if err := proto.Unmarshal(body, response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got := response.Header["Micro-Error"]; got != "" {
		t.Fatalf("unexpected call error: %s", got)
	}
	if got := response.Header["Micro-Id"]; got != "request-1" {
		t.Fatalf("unexpected response id: %q", got)
	}
	if got := string(response.Body); got != "ok" {
		t.Fatalf("unexpected response body: %q", got)
	}
}
