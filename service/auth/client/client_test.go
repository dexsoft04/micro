package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/micro/micro/v3/proto/auth"
	"github.com/micro/micro/v3/service/auth"
	sclient "github.com/micro/micro/v3/service/client"
	merrors "github.com/micro/micro/v3/service/errors"
	"github.com/micro/micro/v3/service/logger"
	"github.com/micro/micro/v3/util/codec"
)

type tokenAuthService struct {
	tokenRsp   *pb.TokenResponse
	tokenErr   error
	inspectRsp *pb.InspectResponse
	inspectErr error
}

func (s *tokenAuthService) Generate(context.Context, *pb.GenerateRequest, ...sclient.CallOption) (*pb.GenerateResponse, error) {
	return nil, nil
}

func (s *tokenAuthService) Inspect(context.Context, *pb.InspectRequest, ...sclient.CallOption) (*pb.InspectResponse, error) {
	return s.inspectRsp, s.inspectErr
}

func (s *tokenAuthService) Token(context.Context, *pb.TokenRequest, ...sclient.CallOption) (*pb.TokenResponse, error) {
	return s.tokenRsp, s.tokenErr
}

func TestTokenReturnsErrorForEmptyTokenResponse(t *testing.T) {
	s := &srv{
		auth: &tokenAuthService{tokenRsp: &pb.TokenResponse{}},
	}

	_, err := s.Token(auth.WithCredentials("admin", "secret"))
	if err == nil {
		t.Fatal("expected error")
	}

	merr := merrors.FromError(err)
	if merr.Code != 500 || merr.Detail != "Empty token response" {
		t.Fatalf("unexpected error: %+v", merr)
	}
}

func TestInspectReturnsInvalidTokenForEmptyAccountResponse(t *testing.T) {
	cases := []struct {
		name string
		rsp  *pb.InspectResponse
	}{
		{name: "nil account", rsp: &pb.InspectResponse{}},
		{name: "nil response", rsp: nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &srv{
				auth: &tokenAuthService{inspectRsp: tc.rsp},
			}
			s.setupJWT()

			var logs bytes.Buffer
			previousLogger := logger.DefaultLogger
			logger.DefaultLogger = logger.NewHelper(logger.NewLogger(
				logger.WithLevel(logger.ErrorLevel),
				logger.WithOutput(&logs),
			))
			defer func() {
				logger.DefaultLogger = previousLogger
			}()

			token := "opaque.token.value"
			acc, err := s.Inspect(token)
			if err != auth.ErrInvalidToken {
				t.Fatalf("expected invalid token error, got account=%+v err=%v", acc, err)
			}
			if acc != nil {
				t.Fatalf("expected nil account, got %+v", acc)
			}
			output := logs.String()
			if strings.Contains(output, token) {
				t.Fatalf("expected log to omit full token, got %q", output)
			}
			if strings.Contains(strings.ToLower(output), "panic") {
				t.Fatalf("expected log message to avoid panic wording, got %q", output)
			}
		})
	}
}

func TestRulesCacheResetWaitsForReaderLock(t *testing.T) {
	var cache rulesCache
	cache.reset(ruleCacheTTL)
	cache.put("micro", []*auth.Rule{{ID: "cached"}})

	cache.RLock()

	started := make(chan struct{})
	resetDone := make(chan struct{})
	go func() {
		close(started)
		cache.reset(ruleCacheTTL)
		close(resetDone)
	}()

	<-started
	select {
	case <-resetDone:
		t.Fatal("reset completed while reader lock was held")
	case <-time.After(50 * time.Millisecond):
	}

	cache.RUnlock()

	select {
	case <-resetDone:
	case <-time.After(time.Second):
		t.Fatal("reset did not complete after reader lock was released")
	}

	if rules := cache.get("micro"); rules != nil {
		t.Fatalf("expected reset to clear cached rules, got %+v", rules)
	}
}

func TestInitClientTokenIsSafeDuringConcurrentRulesAndVerify(t *testing.T) {
	previousClient := sclient.DefaultClient
	sclient.DefaultClient = &rulesListClient{
		rules: []*pb.Rule{{
			Id:       "public",
			Scope:    auth.ScopePublic,
			Priority: 1,
			Access:   pb.Access_GRANTED,
			Resource: &pb.Resource{
				Type:     "*",
				Name:     "*",
				Endpoint: "*",
			},
		}},
	}
	defer func() {
		sclient.DefaultClient = previousClient
	}()

	s := NewAuth(auth.Issuer("micro")).(*srv)
	res := &auth.Resource{Type: "service", Name: "go.micro.service.test", Endpoint: "Test.Call"}

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, 64)

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			for j := 0; j < 200; j++ {
				if _, err := s.Rules(auth.RulesNamespace("micro")); err != nil {
					errs <- fmt.Errorf("worker %d rules: %w", worker, err)
					return
				}
				if err := s.Verify(nil, res, auth.VerifyNamespace("micro")); err != nil {
					errs <- fmt.Errorf("worker %d verify: %w", worker, err)
					return
				}
			}
		}(i)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 200; i++ {
			s.Init(
				auth.Issuer("micro"),
				auth.ClientToken(&auth.AccountToken{
					AccessToken:  fmt.Sprintf("access-%d", i),
					RefreshToken: "refresh",
					Expiry:       time.Now().Add(time.Minute),
				}),
			)
		}
	}()

	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatal(err)
	}
}

type rulesListClient struct {
	rules []*pb.Rule
}

func (c *rulesListClient) Init(...sclient.Option) error {
	return nil
}

func (c *rulesListClient) Options() sclient.Options {
	return sclient.Options{}
}

func (c *rulesListClient) NewMessage(topic string, msg interface{}, opts ...sclient.MessageOption) sclient.Message {
	return &testMessage{topic: topic, payload: msg}
}

func (c *rulesListClient) NewRequest(service, endpoint string, req interface{}, reqOpts ...sclient.RequestOption) sclient.Request {
	return &testRequest{service: service, endpoint: endpoint, body: req}
}

func (c *rulesListClient) Call(ctx context.Context, req sclient.Request, rsp interface{}, opts ...sclient.CallOption) error {
	if req.Endpoint() != "Rules.List" {
		return fmt.Errorf("unexpected endpoint %s", req.Endpoint())
	}
	listRsp, ok := rsp.(*pb.ListResponse)
	if !ok {
		return fmt.Errorf("unexpected response type %T", rsp)
	}
	listRsp.Rules = c.rules
	return nil
}

func (c *rulesListClient) Stream(ctx context.Context, req sclient.Request, opts ...sclient.CallOption) (sclient.Stream, error) {
	return nil, errors.New("not implemented")
}

func (c *rulesListClient) Publish(ctx context.Context, msg sclient.Message, opts ...sclient.PublishOption) error {
	return nil
}

func (c *rulesListClient) String() string {
	return "rules-list"
}

type testMessage struct {
	topic   string
	payload interface{}
}

func (m *testMessage) Topic() string {
	return m.topic
}

func (m *testMessage) Payload() interface{} {
	return m.payload
}

func (m *testMessage) ContentType() string {
	return ""
}

type testRequest struct {
	service  string
	endpoint string
	body     interface{}
}

func (r *testRequest) Service() string {
	return r.service
}

func (r *testRequest) Method() string {
	return r.endpoint
}

func (r *testRequest) Endpoint() string {
	return r.endpoint
}

func (r *testRequest) ContentType() string {
	return ""
}

func (r *testRequest) Body() interface{} {
	return r.body
}

func (r *testRequest) Codec() codec.Writer {
	return nil
}

func (r *testRequest) Stream() bool {
	return false
}
