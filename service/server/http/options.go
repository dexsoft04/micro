package http

import (
	"context"
	"github.com/micro/micro/v3/service/server"
)
type staticDirKey struct{}

func setServerOption(k, v interface{}) server.Option {
	return func(o *server.Options) {
		if o.Context == nil {
			o.Context = context.Background()
		}
		o.Context = context.WithValue(o.Context, k, v)
	}
}

// StaticDir sets the static file directory. This defaults to ./html
func StaticDir(d string) server.Option {
	return setServerOption(staticDirKey{}, d)
}

func newOptions(opts ...server.Option) server.Options {
	opt := server.Options{
		Name:             server.DefaultName,
		Version:          server.DefaultVersion,
		Id:               server.DefaultId,
		Address:          server.DefaultAddress,
		RegisterTTL:      server.DefaultRegisterTTL,
		RegisterInterval: server.DefaultRegisterInterval,
		Context:          context.TODO(),
	}

	for _, o := range opts {
		o(&opt)
	}

	return opt
}