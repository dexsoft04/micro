package cmd

import (
	stderrors "errors"
	"testing"

	"github.com/micro/micro/v3/service/auth"
	merrors "github.com/micro/micro/v3/service/errors"
)

func TestShouldRefreshTokenWithCredentials(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "invalid refresh token",
			err:  auth.ErrInvalidToken,
			want: true,
		},
		{
			name: "empty token response",
			err:  merrors.InternalServerError("auth.Auth.Token", "Empty token response"),
			want: true,
		},
		{
			name: "serialized empty token response",
			err:  stderrors.New(`{"Id":"auth.Auth.Token","Code":500,"Detail":"Empty token response","Status":"Internal Server Error"}`),
			want: true,
		},
		{
			name: "other auth token error",
			err:  merrors.InternalServerError("auth.Auth.Token", "Unable to generate token"),
			want: true,
		},
		{
			name: "transport error",
			err:  stderrors.New("connection refused"),
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldRefreshTokenWithCredentials(tc.err); got != tc.want {
				t.Fatalf("shouldRefreshTokenWithCredentials() = %v, want %v", got, tc.want)
			}
		})
	}
}
