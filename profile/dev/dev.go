package dev

import (
	"github.com/micro/micro/v3/profile"
	"github.com/micro/micro/v3/service/auth/jwt"
	"github.com/micro/micro/v3/service/config"
	storeConfig "github.com/micro/micro/v3/service/config/store"
	evStore "github.com/micro/micro/v3/service/events/store"
	memStream "github.com/micro/micro/v3/service/events/stream/memory"
	"github.com/micro/micro/v3/service/logger"
	"github.com/micro/micro/v3/service/runtime/local"
	"github.com/micro/micro/v3/service/store/file"
	"github.com/urfave/cli/v2"

	microAuth "github.com/micro/micro/v3/service/auth"
	microEvents "github.com/micro/micro/v3/service/events"
	microRuntime "github.com/micro/micro/v3/service/runtime"
	microStore "github.com/micro/micro/v3/service/store"

	"github.com/micro/micro/plugin/etcd/v3"
	"github.com/micro/micro/plugin/nats/broker/v3"

)

func init() {
	profile.Register("dev", Dev)
}

// Local profile to run locally
var Dev = &profile.Profile{
	Name: "dev",
	Setup: func(ctx *cli.Context) error {
		microAuth.DefaultAuth = jwt.NewAuth()
		microStore.DefaultStore = file.NewStore()
		profile.SetupConfigSecretKey(ctx)
		config.DefaultConfig, _ = storeConfig.NewConfig(microStore.DefaultStore, "")
		profile.SetupBroker(nats.NewBroker())
		profile.SetupRegistry(etcd.NewRegistry())
		profile.SetupJWT(ctx)

		// use the local runtime, note: the local runtime is designed to run source code directly so
		// the runtime builder should NOT be set when using this implementation
		microRuntime.DefaultRuntime = local.NewRuntime()

		var err error
		microEvents.DefaultStream, err = memStream.NewStream()
		if err != nil {
			logger.Fatalf("Error configuring stream: %v", err)
		}
		microEvents.DefaultStore = evStore.NewStore(
			evStore.WithStore(microStore.DefaultStore),
		)

		microStore.DefaultBlobStore, err = file.NewBlobStore()
		if err != nil {
			logger.Fatalf("Error configuring file blob store: %v", err)
		}

		return nil
	},
}
