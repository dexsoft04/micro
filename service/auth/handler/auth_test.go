package handler

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/micro/micro/v3/proto/auth"
	sauth "github.com/micro/micro/v3/service/auth"
	merrors "github.com/micro/micro/v3/service/errors"
	"github.com/micro/micro/v3/util/auth/token"
)

type failingTokenProvider struct{}

func (f failingTokenProvider) Generate(*sauth.Account, ...token.GenerateOption) (*token.Token, error) {
	return nil, stderrors.New("generate failed")
}

func (f failingTokenProvider) Inspect(string) (*sauth.Account, error) {
	return &sauth.Account{ID: "admin"}, nil
}

func (f failingTokenProvider) String() string {
	return "jwt"
}

func TestTokenReturnsErrorWhenJWTRefreshGenerateFails(t *testing.T) {
	h := &Auth{TokenProvider: failingTokenProvider{}, DisableAdmin: true}
	rsp := new(auth.TokenResponse)

	err := h.Token(context.Background(), &auth.TokenRequest{RefreshToken: "jwt-token", TokenExpiry: 60}, rsp)
	if err == nil {
		t.Fatal("expected error")
	}

	merr := merrors.FromError(err)
	if merr.Code != 500 || merr.Detail != "Unable to generate token: generate failed" {
		t.Fatalf("unexpected error: %+v", merr)
	}
	if rsp.Token != nil {
		t.Fatalf("expected empty token response, got %+v", rsp.Token)
	}
}
