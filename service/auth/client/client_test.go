package client

import (
	"bytes"
	"context"
	"strings"
	"testing"

	pb "github.com/micro/micro/v3/proto/auth"
	"github.com/micro/micro/v3/service/auth"
	sclient "github.com/micro/micro/v3/service/client"
	merrors "github.com/micro/micro/v3/service/errors"
	"github.com/micro/micro/v3/service/logger"
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
