// Package platform is a profile for running a highly available Micro platform
package platform

import (
	"crypto/tls"
	"crypto/x509"
	"github.com/micro/micro/plugin/etcd/v3"
	"github.com/micro/micro/plugin/postgres/v3"
	"github.com/micro/micro/v3/profile"
	"github.com/micro/micro/v3/service/auth"
	"github.com/micro/micro/v3/service/auth/jwt"
	"github.com/micro/micro/v3/service/broker"
	microBuilder "github.com/micro/micro/v3/service/build"
	"github.com/micro/micro/v3/service/build/golang"
	"github.com/micro/micro/v3/service/config"
	storeConfig "github.com/micro/micro/v3/service/config/store"
	"github.com/micro/micro/v3/service/events"
	evStore "github.com/micro/micro/v3/service/events/store"
	"github.com/micro/micro/v3/service/logger"
	"github.com/micro/micro/v3/service/metrics"
	"github.com/micro/micro/v3/service/model"
	microRuntime "github.com/micro/micro/v3/service/runtime"
	"github.com/micro/micro/v3/service/runtime/kubernetes"
	"github.com/micro/micro/v3/service/store"
	"github.com/micro/micro/v3/util/opentelemetry"
	"github.com/micro/micro/v3/util/opentelemetry/jaeger"
	"github.com/opentracing/opentracing-go"
	"github.com/micro/micro/plugin/prometheus/v3"
	"github.com/urfave/cli/v2"
	"io/ioutil"
	"os"

	natsBroker "github.com/micro/micro/plugin/nats/broker/v3"
	natsStream "github.com/micro/micro/plugin/nats/stream/v3"
	s3 "github.com/micro/micro/plugin/s3/v3"
)

func init() {
	profile.Register("platform", Profile)
}

// Profile is for running the micro platform
var Profile = &profile.Profile{
	Name: "platform",
	Setup: func(ctx *cli.Context) error {
		auth.DefaultAuth = jwt.NewAuth()
		profile.SetupJWT(ctx)

		var err error
		microRuntime.DefaultRuntime = kubernetes.NewRuntime()
		microBuilder.DefaultBuilder, err = golang.NewBuilder()
		if err != nil {
			logger.Fatalf("Error configuring golang builder: %v", err)
		}

		store.DefaultStore = postgres.NewStore(store.Nodes(ctx.String("store_address")))
		config.DefaultConfig, _ = storeConfig.NewConfig(store.DefaultStore, "")
		profile.SetupConfigSecretKey(ctx)

		if ctx.Args().Get(1) == "broker" {
			profile.SetupBroker(natsBroker.NewBroker(broker.Addrs("nats-cluster")))
		}
		//if ctx.Args().Get(1) == "registry" {
		//	profile.SetupRegistry(etcd.NewRegistry(registry.Addrs("etcd-cluster")))
		//}
		profile.SetupRegistry(etcd.NewRegistry(profile.EtcdOpts(ctx)...))

		// Set up a default metrcs reporter (being careful not to clash with any that have already been set):
		if !metrics.IsSet() {
			prometheusReporter, err := prometheus.New()
			if err != nil {
				return err
			}
			metrics.SetDefaultMetricsReporter(prometheusReporter)
		}

		events.DefaultStream, err = natsStream.NewStream(natsStreamOpts(ctx)...)
		if err != nil {
			logger.Fatalf("Error configuring stream: %v", err)
		}
		events.DefaultStore = evStore.NewStore(evStore.WithStore(store.DefaultStore))

		// only configure the blob store for the store and runtime services
		if ctx.Args().Get(1) == "runtime" || ctx.Args().Get(1) == "store" {
			store.DefaultBlobStore, err = s3.NewBlobStore(
				s3.Credentials(
					os.Getenv("MICRO_BLOB_STORE_ACCESS_KEY"),
					os.Getenv("MICRO_BLOB_STORE_SECRET_KEY"),
				),
				s3.Endpoint("minio-cluster:9000"),
				s3.Region(os.Getenv("MICRO_BLOB_STORE_REGION")),
				s3.Insecure(),
			)
			if err != nil {
				logger.Fatalf("Error configuring s3 blob store: %v", err)
			}
		}

		// set the store in the model
		model.DefaultModel = model.NewModel(
			model.WithStore(store.DefaultStore),
		)

		//// Use k8s routing which is DNS based
		//router.DefaultRouter = k8sRouter.NewRouter()
		//client.DefaultClient.Init(client.Router(router.DefaultRouter))

		// Configure tracing with Jaeger (forced tracing):
		tracingServiceName := ctx.Args().Get(1)
		if len(tracingServiceName) == 0 {
			tracingServiceName = "Micro"
		}
		reporterAddress := ctx.String("tracing_reporter_address")
		if len(reporterAddress) == 0 {
			reporterAddress = jaeger.DefaultReporterAddress
		}
		openTracer, _, err := jaeger.New(
			opentelemetry.WithServiceName(tracingServiceName),
			opentelemetry.WithTraceReporterAddress(reporterAddress),
		)
		if err != nil {
			logger.Fatalf("Error configuring opentracing: %v", err)
		}
		opentracing.SetGlobalTracer(openTracer)
		opentelemetry.DefaultOpenTracer = openTracer

		kubernetes.DefaultImage = "wolfplus2048/cells:v0.0.4"
		return nil
	},
}

// natsStreamOpts returns a slice of options which should be used to configure nats
func natsStreamOpts(ctx *cli.Context) []natsStream.Option {
	opts := []natsStream.Option{
		natsStream.Address("nats://nats-cluster:4222"),
		natsStream.ClusterID("nats-streaming-cluster"),
	}

	// Parse event TLS certs
	if len(ctx.String("events_tls_cert")) > 0 || len(ctx.String("events_tls_key")) > 0 {
		cert, err := tls.LoadX509KeyPair(ctx.String("events_tls_cert"), ctx.String("events_tls_key"))
		if err != nil {
			logger.Fatalf("Error loading event TLS cert: %v", err)
		}

		// load custom certificate authority
		caCertPool := x509.NewCertPool()
		if len(ctx.String("events_tls_ca")) > 0 {
			crt, err := ioutil.ReadFile(ctx.String("events_tls_ca"))
			if err != nil {
				logger.Fatalf("Error loading event TLS certificate authority: %v", err)
			}
			caCertPool.AppendCertsFromPEM(crt)
		}

		cfg := &tls.Config{Certificates: []tls.Certificate{cert}, RootCAs: caCertPool}
		opts = append(opts, natsStream.TLSConfig(cfg))
	}

	return opts
}
