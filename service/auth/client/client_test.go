package client

import (
	"context"
	"testing"

	pb "github.com/micro/micro/v3/proto/auth"
	"github.com/micro/micro/v3/service/auth"
	sclient "github.com/micro/micro/v3/service/client"
	merrors "github.com/micro/micro/v3/service/errors"
)

type tokenAuthService struct {
	rsp *pb.TokenResponse
	err error
}

func (s *tokenAuthService) Generate(context.Context, *pb.GenerateRequest, ...sclient.CallOption) (*pb.GenerateResponse, error) {
	return nil, nil
}

func (s *tokenAuthService) Inspect(context.Context, *pb.InspectRequest, ...sclient.CallOption) (*pb.InspectResponse, error) {
	return nil, nil
}

func (s *tokenAuthService) Token(context.Context, *pb.TokenRequest, ...sclient.CallOption) (*pb.TokenResponse, error) {
	return s.rsp, s.err
}

func TestTokenReturnsErrorForEmptyTokenResponse(t *testing.T) {
	s := &srv{
		auth: &tokenAuthService{rsp: &pb.TokenResponse{}},
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
