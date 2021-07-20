package uauth

import (
	"context"
	"errors"
	"time"
)

var (
	Default         Auth

	ErrUnauthorized = errors.New("Unauthorized")
	// ErrNotFound is returned when a utoken cannot be found
	ErrNotFound = errors.New("user token not found")
	// ErrEncodingToken is returned when the service encounters an error during encoding
	ErrEncodingToken = errors.New("error encoding the user token")
	// ErrInvalidToken is returned when the utoken provided is not valid
	ErrInvalidToken = errors.New("invalid user token provided")
)

type Account struct {
	OpenId string
	Nickname string
	HeadUri string
	ExpiresAt int64
	Metadata map[string]string
}

type Token struct {
	// The actual utoken
	Token string `json:"token"`
	// Time of utoken creation
	Created time.Time `json:"created"`
	// Time of utoken expiry
	Expiry time.Time `json:"expiry"`
}


// Provider generates and inspects tokens
type Auth interface {
	Init(opts ...Option)
	Generate(account *Account, opts ...GenerateOption) (*Token, error)
	Inspect(token string) (*Account, error)
	String() string
}



type accountKey struct{}

// AccountFromContext gets the account from the context, which
// is set by the auth wrapper at the start of a call. If the account
// is not set, a nil account will be returned. The error is only returned
// when there was a problem retrieving an account
func AccountFromContext(ctx context.Context) (*Account, bool) {
	acc, ok := ctx.Value(accountKey{}).(*Account)
	return acc, ok
}

// ContextWithAccount sets the account in the context
func ContextWithAccount(ctx context.Context, account *Account) context.Context {
	return context.WithValue(ctx, accountKey{}, account)
}
func Generate(account *Account, opts ...GenerateOption) (*Token, error) {
	return Default.Generate(account, opts...)
}
func Inspect(token string) (*Account, error) {
	return Default.Inspect(token)
}