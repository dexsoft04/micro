// Copyright 2020 Asim Aslam
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Original source: github.com/micro/go-micro/v3/util/utoken/ujwt/ujwt.go

package ujwt

import (
	"encoding/base64"
	"github.com/micro/micro/v3/service/uauth"
	"strings"
	"time"

	"github.com/dgrijalva/jwt-go"
)

// authClaims to be encoded in the JWT
type authClaims struct {
	OpenId   string `json:"openid"`
	Nickname string `json:"nickname"`
	HeadUri  string `json:"headuri"`
	Metadata map[string]string `json:"metadata"`
	jwt.StandardClaims
}

// JWT implementation of utoken provider
type JWT struct {
	opts uauth.Options
}

// NewTokenProvider returns an initialized basic provider
func NewAuth(opts ...uauth.Option) uauth.Auth {
	return &JWT{
		opts: uauth.NewOptions(opts...),
	}
}
func (j *JWT) Init(opts ...uauth.Option) {
	for _, o := range opts {
		o(&j.opts)
	}
}
// Generate a new JWT
func (j *JWT) Generate(acc *uauth.Account, opts ...uauth.GenerateOption) (*uauth.Token, error) {
	var priv []byte
	if strings.HasPrefix(j.opts.PrivateKey, "-----BEGIN RSA PRIVATE KEY-----") {
		priv = []byte(j.opts.PrivateKey)
	} else {
		var err error
		priv, err = base64.StdEncoding.DecodeString(j.opts.PrivateKey)
		if err != nil {
			return nil, err
		}
	}

	// parse the private key
	key, err := jwt.ParseRSAPrivateKeyFromPEM(priv)
	if err != nil {
		return nil, uauth.ErrEncodingToken
	}

	// parse the options
	options := uauth.NewGenerateOptions(opts...)

	// generate the JWT
	expiry := time.Now().Add(options.Expiry)

	t := jwt.NewWithClaims(jwt.SigningMethodRS256, authClaims{
		OpenId: acc.OpenId, Nickname: acc.Nickname, HeadUri:acc.HeadUri, Metadata: acc.Metadata,
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: expiry.Unix(),
		},
	})

	tok, err := t.SignedString(key)
	if err != nil {
		return nil, err
	}

	// return the utoken
	return &uauth.Token{
		Token:   tok,
		Expiry:  expiry,
		Created: time.Now(),
	}, nil
}

// Inspect a JWT
func (j *JWT) Inspect(t string) (*uauth.Account, error) {
	var pub []byte
	if strings.HasPrefix(j.opts.PublicKey, "-----BEGIN CERTIFICATE-----") {
		pub = []byte(j.opts.PublicKey)
	} else {
		var err error
		pub, err = base64.StdEncoding.DecodeString(j.opts.PublicKey)
		if err != nil {
			return nil, err
		}
	}

	// parse the public key
	res, err := jwt.ParseWithClaims(t, &authClaims{}, func(token *jwt.Token) (interface{}, error) {
		return jwt.ParseRSAPublicKeyFromPEM(pub)
	})
	if err != nil {
		return nil, uauth.ErrInvalidToken
	}

	// validate the utoken
	if !res.Valid {
		return nil, uauth.ErrInvalidToken
	}
	claims, ok := res.Claims.(*authClaims)
	if !ok {
		return nil, uauth.ErrInvalidToken
	}

	// return the utoken
	return &uauth.Account{
		OpenId:   claims.OpenId,
		Nickname: claims.Nickname,
		HeadUri:  claims.HeadUri,
		Metadata: claims.Metadata,
		ExpiresAt: claims.StandardClaims.ExpiresAt,
	}, nil
}

// String returns JWT
func (j *JWT) String() string {
	return "ujwt"
}
