package websocket

import (
	"context"
	"github.com/micro/micro/v3/service/network/transport"
	"github.com/micro/micro/v3/service/server"
	"google.golang.org/grpc/encoding"
)
type codecsKey struct{}
type transportKey struct{}

func setServerOption(k, v interface{}) server.Option {
	return func(o *server.Options) {
		if o.Context == nil {
			o.Context = context.Background()
		}
		o.Context = context.WithValue(o.Context, k, v)
	}
}

// gRPC Codec to be used to encode/decode requests for a given content type
func Codec(contentType string, c encoding.Codec) server.Option {
	return func(o *server.Options) {
		codecs := make(map[string]encoding.Codec)
		if o.Context == nil {
			o.Context = context.Background()
		}
		if v, ok := o.Context.Value(codecsKey{}).(map[string]encoding.Codec); ok && v != nil {
			codecs = v
		}
		codecs[contentType] = c
		o.Context = context.WithValue(o.Context, codecsKey{}, codecs)
	}
}
func Transport(trans transport.Transport) server.Option {
	return setServerOption(transportKey{}, trans)
}