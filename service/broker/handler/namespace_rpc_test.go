package handler

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"testing"
	"time"

	pb "github.com/micro/micro/v3/proto/broker"
	"github.com/micro/micro/v3/service/auth"
	jwtAuth "github.com/micro/micro/v3/service/auth/jwt"
	brokerclient "github.com/micro/micro/v3/service/broker/client"
	"github.com/micro/micro/v3/service/client"
	grpcclient "github.com/micro/micro/v3/service/client/grpc"
	"github.com/micro/micro/v3/service/context/metadata"
	microerrors "github.com/micro/micro/v3/service/errors"
	registrymemory "github.com/micro/micro/v3/service/registry/memory"
	"github.com/micro/micro/v3/service/server"
	grpcserver "github.com/micro/micro/v3/service/server/grpc"
	internalAuth "github.com/micro/micro/v3/util/auth"
	raw "github.com/micro/micro/v3/util/codec/bytes"
	"github.com/micro/micro/v3/util/wrapper"
)

// Ephemeral keys exercise the platform AuthHandler without using a platform credential.
func regressionAuth(t *testing.T) map[string]string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	private := base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	public := base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}))
	a := jwtAuth.NewAuth(auth.Issuer("micro"), auth.PrivateKey(private), auth.PublicKey(public))
	for _, rule := range internalAuth.SystemRules {
		if err := a.Grant(rule); err != nil {
			t.Fatal(err)
		}
	}
	tokens := map[string]string{}
	for _, ns := range []string{"micro", "igaoshou", "other"} {
		account, err := a.Generate("namespace-regression", auth.WithIssuer(ns), auth.WithType("service"), auth.WithScopes("service"))
		if err != nil {
			t.Fatal(err)
		}
		tokens[ns] = account.Secret
	}
	a.Init(auth.ClientToken(&auth.AccountToken{AccessToken: tokens["micro"], Expiry: time.Now().Add(time.Hour)}))
	original := auth.DefaultAuth
	auth.DefaultAuth = a
	t.Cleanup(func() { auth.DefaultAuth = original })
	return tokens
}
func TestBrokerNamespaceRPC(t *testing.T) {
	b := testBroker(t)
	tokens := regressionAuth(t)
	r := registrymemory.NewRegistry()
	s := grpcserver.NewServer(
		server.Name("broker"), server.Address("127.0.0.1:0"),
		server.Registry(r), server.Broker(b),
		server.WrapHandler(wrapper.AuthHandler()),
	)
	if err := pb.RegisterBrokerHandler(s, &Broker{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Stop(); err != nil {
			t.Error(err)
		}
	})
	c := grpcclient.NewClient(client.Registry(r), client.RequestTimeout(3*time.Second))
	rpc := pb.NewBrokerService("broker", c)
	originalClient := client.DefaultClient
	client.DefaultClient = wrapper.AuthClient(c)
	t.Cleanup(func() { client.DefaultClient = originalClient })
	publisher := grpcclient.NewClient(client.Broker(brokerclient.NewBroker()))

	for _, tc := range []struct {
		name, override, target string
		present                bool
	}{
		{name: "default issuer", target: "micro"},
		{name: "cross namespace", override: "igaoshou", target: "igaoshou", present: true},
		{name: "empty override", target: "micro", present: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			streams := map[string]pb.Broker_SubscribeService{}
			for _, ns := range []string{"micro", "igaoshou"} {
				subctx := metadata.Set(ctx, "Authorization", internalAuth.BearerScheme+tokens[ns])
				stream, err := rpc.Subscribe(subctx, &pb.SubscribeRequest{Topic: tc.name})
				if err != nil {
					t.Fatal(err)
				}
				defer stream.Close()
				streams[ns] = stream
			}
			for range 2 {
				select {
				case <-b.ready:
				case <-ctx.Done():
					t.Fatal("RPC subscriptions not ready")
				}
			}
			var publishOptions []client.PublishOption
			if tc.present {
				publishOptions = append(publishOptions, client.PublishNamespace(tc.override))
			}
			if err := publisher.Publish(ctx, publisher.NewMessage(tc.name, &raw.Frame{Data: []byte("namespace-wire-event")}), publishOptions...); err != nil {
				t.Fatal(err)
			}

			msg, err := streams[tc.target].Recv()
			if err != nil || string(msg.Body) != "namespace-wire-event" {
				t.Fatalf("RPC target delivery failed: %v", err)
			}
			wrong := "micro"
			if tc.target == "micro" {
				wrong = "igaoshou"
			}
			unexpected := make(chan *pb.Message, 1)
			go func() {
				msg, err := streams[wrong].Recv()
				if err == nil {
					unexpected <- msg
				}
			}()
			select {
			case <-unexpected:
				t.Fatal("RPC message leaked to another namespace")
			case <-time.After(100 * time.Millisecond):
			}
		})
	}
	ctx := metadata.Set(context.Background(), "Authorization", internalAuth.BearerScheme+tokens["other"])
	ctx = metadata.Set(ctx, "Micro-Sub-Namespace", "igaoshou")
	if _, err := rpc.Publish(ctx, &pb.PublishRequest{Topic: "authenticated-override", Message: &pb.Message{Body: []byte("compatibility")}}); err != nil {
		t.Fatalf("existing authenticated routing override rejected: %v", err)
	}

	if _, err := rpc.Publish(context.Background(), &pb.PublishRequest{Topic: "unauthenticated", Message: &pb.Message{}}); microerrors.FromError(err).Code != 401 {
		t.Fatalf("unauthenticated RPC publish: %v", err)
	}
}
