package cmd

import (
	stderrors "errors"
	"testing"
	"time"

	"github.com/micro/micro/v3/service/auth"
	authtoken "github.com/micro/micro/v3/util/auth/token"
)

type renewalAuth struct {
	options       auth.Options
	token         func(auth.TokenOptions) (*auth.AccountToken, error)
	generated     *auth.Account
	generateCalls int
}

func (a *renewalAuth) Init(opts ...auth.Option) {
	for _, opt := range opts {
		opt(&a.options)
	}
}

func (a *renewalAuth) Options() auth.Options { return a.options }

func (a *renewalAuth) Generate(_ string, _ ...auth.GenerateOption) (*auth.Account, error) {
	a.generateCalls++
	if a.generated == nil {
		return nil, stderrors.New("unexpected service credential generation")
	}
	return a.generated, nil
}

func (a *renewalAuth) Token(opts ...auth.TokenOption) (*auth.AccountToken, error) {
	return a.token(auth.NewTokenOptions(opts...))
}

func (a *renewalAuth) Verify(*auth.Account, *auth.Resource, ...auth.VerifyOption) error {
	return nil
}

func (a *renewalAuth) Inspect(string) (*auth.Account, error) { return nil, nil }
func (a *renewalAuth) Grant(*auth.Rule) error                { return nil }
func (a *renewalAuth) Revoke(*auth.Rule) error               { return nil }
func (a *renewalAuth) Rules(...auth.RulesOption) ([]*auth.Rule, error) {
	return nil, nil
}
func (a *renewalAuth) String() string { return "renewal-test" }

func installRenewalAuth(t *testing.T, replacement auth.Auth, selfGenerated bool) {
	t.Helper()
	originalAuth := auth.DefaultAuth
	originalSelfGenerated := serviceAuthCredentialsSelfGenerated
	auth.DefaultAuth = replacement
	serviceAuthCredentialsSelfGenerated = selfGenerated
	t.Cleanup(func() {
		auth.DefaultAuth = originalAuth
		serviceAuthCredentialsSelfGenerated = originalSelfGenerated
	})
}

func TestRenewServiceAuthTokenRegeneratesExpiredSelfGeneratedCredentials(t *testing.T) {
	freshToken := &auth.AccountToken{
		AccessToken:  "fresh-access",
		RefreshToken: "fresh-refresh",
		Expiry:       time.Now().Add(10 * time.Minute),
	}
	fake := &renewalAuth{
		options: auth.Options{
			ID:     "expired-id",
			Secret: "expired-secret",
			Token:  &auth.AccountToken{RefreshToken: "expired-refresh"},
		},
		generated: &auth.Account{ID: "fresh-id", Secret: "fresh-secret"},
	}
	fake.token = func(options auth.TokenOptions) (*auth.AccountToken, error) {
		if options.RefreshToken != "" || options.Secret == "expired-secret" {
			return nil, authtoken.ErrInvalidToken
		}
		if options.ID == "fresh-id" && options.Secret == "fresh-secret" {
			return freshToken, nil
		}
		return nil, stderrors.New("unexpected token request")
	}
	installRenewalAuth(t, fake, true)

	if err := renewServiceAuthToken(); err != nil {
		t.Fatalf("renewServiceAuthToken() error = %v", err)
	}
	if fake.generateCalls != 1 {
		t.Fatalf("generate calls = %d, want 1", fake.generateCalls)
	}
	options := fake.Options()
	if options.ID != "fresh-id" {
		t.Fatalf("renewed auth ID = %q, want fresh-id", options.ID)
	}
	if options.Secret != "fresh-secret" {
		t.Fatal("renewed auth secret was not installed")
	}
	if options.Token != freshToken {
		t.Fatal("renewed auth token was not installed")
	}
}

func TestRenewServiceAuthTokenRejectsMissingCurrentToken(t *testing.T) {
	fake := &renewalAuth{
		options: auth.Options{ID: "configured-id", Secret: "configured-secret"},
		token: func(auth.TokenOptions) (*auth.AccountToken, error) {
			t.Fatal("token renewal should not be attempted")
			return nil, nil
		},
	}
	installRenewalAuth(t, fake, false)

	if err := renewServiceAuthToken(); err == nil {
		t.Fatal("renewServiceAuthToken() error = nil, want failure")
	}
}

func TestRenewServiceAuthTokenUsesRefreshToken(t *testing.T) {
	freshToken := &auth.AccountToken{
		AccessToken:  "fresh-access",
		RefreshToken: "fresh-refresh",
		Expiry:       time.Now().Add(10 * time.Minute),
	}
	fake := &renewalAuth{
		options: auth.Options{
			ID:     "configured-id",
			Secret: "configured-secret",
			Token:  &auth.AccountToken{RefreshToken: "current-refresh"},
		},
	}
	fake.token = func(options auth.TokenOptions) (*auth.AccountToken, error) {
		if options.RefreshToken != "current-refresh" {
			t.Fatalf("refresh token = %q, want current-refresh", options.RefreshToken)
		}
		return freshToken, nil
	}
	installRenewalAuth(t, fake, false)

	if err := renewServiceAuthToken(); err != nil {
		t.Fatalf("renewServiceAuthToken() error = %v", err)
	}
	if fake.generateCalls != 0 {
		t.Fatalf("generate calls = %d, want 0", fake.generateCalls)
	}
	if fake.Options().Token != freshToken {
		t.Fatal("renewed auth token was not installed")
	}
}

func TestRenewServiceAuthTokenPreservesConfiguredCredentials(t *testing.T) {
	fake := &renewalAuth{
		options: auth.Options{
			ID:     "configured-id",
			Secret: "configured-secret",
			Token:  &auth.AccountToken{RefreshToken: "expired-refresh"},
		},
	}
	fake.token = func(auth.TokenOptions) (*auth.AccountToken, error) {
		return nil, authtoken.ErrInvalidToken
	}
	installRenewalAuth(t, fake, false)

	if err := renewServiceAuthToken(); err == nil {
		t.Fatal("renewServiceAuthToken() error = nil, want failure")
	}
	if fake.generateCalls != 0 {
		t.Fatalf("generate calls = %d, want 0", fake.generateCalls)
	}
	options := fake.Options()
	if options.ID != "configured-id" || options.Secret != "configured-secret" {
		t.Fatal("configured credentials were replaced after renewal failure")
	}
}

func TestRenewServiceAuthTokenUsesConfiguredCredentialsAfterRefreshFailure(t *testing.T) {
	freshToken := &auth.AccountToken{
		AccessToken:  "fresh-access",
		RefreshToken: "fresh-refresh",
		Expiry:       time.Now().Add(10 * time.Minute),
	}
	fake := &renewalAuth{
		options: auth.Options{
			ID:     "configured-id",
			Secret: "configured-secret",
			Token:  &auth.AccountToken{RefreshToken: "expired-refresh"},
		},
	}
	fake.token = func(options auth.TokenOptions) (*auth.AccountToken, error) {
		if options.RefreshToken != "" {
			return nil, stderrors.New("refresh unavailable")
		}
		return freshToken, nil
	}
	installRenewalAuth(t, fake, false)

	if err := renewServiceAuthToken(); err != nil {
		t.Fatalf("renewServiceAuthToken() error = %v", err)
	}
	if fake.generateCalls != 0 {
		t.Fatalf("generate calls = %d, want 0", fake.generateCalls)
	}
	if fake.Options().Token != freshToken {
		t.Fatalf("renewed token = %+v, want %+v", fake.Options().Token, freshToken)
	}
}

func TestRenewServiceAuthTokenUsesConfiguredCredentialsAfterEmptyRefreshResponse(t *testing.T) {
	freshToken := &auth.AccountToken{
		AccessToken:  "fresh-access",
		RefreshToken: "fresh-refresh",
		Expiry:       time.Now().Add(10 * time.Minute),
	}
	tokenCalls := 0
	fake := &renewalAuth{
		options: auth.Options{
			ID:     "configured-id",
			Secret: "configured-secret",
			Token:  &auth.AccountToken{RefreshToken: "current-refresh"},
		},
	}
	fake.token = func(options auth.TokenOptions) (*auth.AccountToken, error) {
		tokenCalls++
		if tokenCalls == 1 {
			if options.RefreshToken != "current-refresh" {
				t.Fatalf("refresh token = %q, want current-refresh", options.RefreshToken)
			}
			return nil, nil
		}
		if options.ID != "configured-id" || options.Secret != "configured-secret" {
			t.Fatal("configured credentials were not used after empty refresh response")
		}
		return freshToken, nil
	}
	installRenewalAuth(t, fake, false)

	if err := renewServiceAuthToken(); err != nil {
		t.Fatalf("renewServiceAuthToken() error = %v", err)
	}
	if tokenCalls != 2 {
		t.Fatalf("token calls = %d, want 2", tokenCalls)
	}
	if fake.Options().Token != freshToken {
		t.Fatal("renewed auth token was not installed")
	}
}
