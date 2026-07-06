package wrapper

import (
	"context"
	"testing"
	"time"

	"github.com/micro/micro/v3/service/auth"
	"github.com/micro/micro/v3/service/client"
	"github.com/micro/micro/v3/service/context/metadata"
	inauth "github.com/micro/micro/v3/util/auth"
)

type testAuth struct {
	options auth.Options
}

func (a *testAuth) String() string {
	return "test"
}

func (a *testAuth) Init(opts ...auth.Option) {
	for _, o := range opts {
		o(&a.options)
	}
}

func (a *testAuth) Options() auth.Options {
	return a.options
}

func (a *testAuth) Generate(string, ...auth.GenerateOption) (*auth.Account, error) {
	return nil, nil
}

func (a *testAuth) Verify(*auth.Account, *auth.Resource, ...auth.VerifyOption) error {
	return nil
}

func (a *testAuth) Inspect(string) (*auth.Account, error) {
	return nil, nil
}

func (a *testAuth) Token(...auth.TokenOption) (*auth.AccountToken, error) {
	return nil, nil
}

func (a *testAuth) Grant(*auth.Rule) error {
	return nil
}

func (a *testAuth) Revoke(*auth.Rule) error {
	return nil
}

func (a *testAuth) Rules(...auth.RulesOption) ([]*auth.Rule, error) {
	return nil, nil
}

func TestWrapContextWithAuthTokenOverridesAuthorization(t *testing.T) {
	restoreAuth := setDefaultAuth(&testAuth{
		options: auth.NewOptions(
			auth.Issuer("igaoshou"),
			auth.ClientToken(&auth.AccountToken{
				AccessToken: "service-token",
				Expiry:      time.Now().Add(time.Hour),
			}),
		),
	})
	defer restoreAuth()

	ctx := metadata.Set(context.Background(), "Authorization", inauth.BearerScheme+"old-token")
	wrapped := (&authWrapper{}).wrapContext(ctx, client.WithAuthToken())

	got, ok := metadata.Get(wrapped, "Authorization")
	if !ok {
		t.Fatal("expected authorization header")
	}
	if want := inauth.BearerScheme + "service-token"; got != want {
		t.Fatalf("unexpected authorization header: got %q want %q", got, want)
	}
}

func TestWrapContextWithAuthTokenRemovesAuthorizationWithoutUsableServiceToken(t *testing.T) {
	cases := []struct {
		name  string
		token *auth.AccountToken
	}{
		{
			name: "expired token",
			token: &auth.AccountToken{
				AccessToken: "expired-service-token",
				Expiry:      time.Now().Add(-time.Hour),
			},
		},
		{
			name:  "missing token",
			token: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			restoreAuth := setDefaultAuth(&testAuth{
				options: auth.NewOptions(
					auth.Issuer("igaoshou"),
					auth.ClientToken(tc.token),
				),
			})
			defer restoreAuth()

			ctx := metadata.Set(context.Background(), "Authorization", inauth.BearerScheme+"old-token")
			wrapped := (&authWrapper{}).wrapContext(ctx, client.WithAuthToken())

			if got, ok := metadata.Get(wrapped, "Authorization"); ok {
				t.Fatalf("expected authorization header to be removed, got %q", got)
			}
		})
	}
}

func TestWrapContextWithoutAuthTokenPreservesAuthorization(t *testing.T) {
	restoreAuth := setDefaultAuth(&testAuth{options: auth.NewOptions(auth.Issuer("igaoshou"))})
	defer restoreAuth()

	ctx := metadata.Set(context.Background(), "Authorization", inauth.BearerScheme+"old-token")
	wrapped := (&authWrapper{}).wrapContext(ctx)

	got, ok := metadata.Get(wrapped, "Authorization")
	if !ok {
		t.Fatal("expected authorization header")
	}
	if want := inauth.BearerScheme + "old-token"; got != want {
		t.Fatalf("unexpected authorization header: got %q want %q", got, want)
	}
}

func setDefaultAuth(a auth.Auth) func() {
	previous := auth.DefaultAuth
	auth.DefaultAuth = a
	return func() {
		auth.DefaultAuth = previous
	}
}
