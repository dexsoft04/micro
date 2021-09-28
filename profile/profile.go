// Package profile is for specific profiles
// @todo this package is the definition of cruft and
// should be rewritten in a more elegant way
package profile

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"github.com/micro/micro/plugin/etcd/v3"
	"github.com/micro/micro/plugin/prometheus/v3"
	"github.com/micro/micro/v3/service/metrics"
	"github.com/micro/micro/v3/service/sync"
	"github.com/opentracing/opentracing-go"
	"github.com/philchia/agollo/v4"
	"github.com/wolfplus2048/mcbeam-plugins/config/apollo/v3"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/micro/micro/v3/service/auth/jwt"
	"github.com/micro/micro/v3/service/broker"
	memBroker "github.com/micro/micro/v3/service/broker/memory"
	"github.com/micro/micro/v3/service/build/golang"
	"github.com/micro/micro/v3/service/client"
	"github.com/micro/micro/v3/service/config"
	storeConfig "github.com/micro/micro/v3/service/config/store"
	evStore "github.com/micro/micro/v3/service/events/store"
	memStream "github.com/micro/micro/v3/service/events/stream/memory"
	"github.com/micro/micro/v3/service/logger"
	"github.com/micro/micro/v3/service/model"
	"github.com/micro/micro/v3/service/registry"
	"github.com/micro/micro/v3/service/registry/memory"
	"github.com/micro/micro/v3/service/router"
	k8sRouter "github.com/micro/micro/v3/service/router/kubernetes"
	regRouter "github.com/micro/micro/v3/service/router/registry"
	"github.com/micro/micro/v3/service/runtime/kubernetes"
	"github.com/micro/micro/v3/service/runtime/local"
	"github.com/micro/micro/v3/service/server"
	"github.com/micro/micro/v3/service/store/file"
	mem "github.com/micro/micro/v3/service/store/memory"
	"github.com/micro/micro/v3/util/opentelemetry"
	"github.com/micro/micro/v3/util/opentelemetry/jaeger"
	"github.com/urfave/cli/v2"

	microAuth "github.com/micro/micro/v3/service/auth"
	microBuilder "github.com/micro/micro/v3/service/build"
	microEvents "github.com/micro/micro/v3/service/events"
	microRuntime "github.com/micro/micro/v3/service/runtime"
	microStore "github.com/micro/micro/v3/service/store"
	inAuth "github.com/micro/micro/v3/util/auth"
	"github.com/micro/micro/v3/util/user"
)

// profiles which when called will configure micro to run in that environment
var profiles = map[string]*Profile{
	// built in profiles
	"client":     Client,
	"service":    Service,
	"test":       Test,
	"local":      Local,
	"kubernetes": Kubernetes,
	"cmd":Cmd,
}

// Profile configures an environment
type Profile struct {
	// name of the profile
	Name string
	// function used for setup
	Setup func(*cli.Context) error
	// TODO: presetup dependencies
	// e.g start resources
}

// Register a profile
func Register(name string, p *Profile) error {
	if _, ok := profiles[name]; ok {
		return fmt.Errorf("profile %s already exists", name)
	}
	profiles[name] = p
	return nil
}

// Load a profile
func Load(name string) (*Profile, error) {
	v, ok := profiles[name]
	if !ok {
		return nil, fmt.Errorf("profile %s does not exist", name)
	}
	return v, nil
}

// Client profile is for any entrypoint that behaves as a client
var Client = &Profile{
	Name: "client",
	Setup: func(ctx *cli.Context) error {
		if !metrics.IsSet() {
			prometheusReporter, err := prometheus.New()
			if err != nil {
				logger.Fatal(err)
			}
			metrics.SetDefaultMetricsReporter(prometheusReporter)
		}
		SetupRegistry(etcd.NewRegistry(EtcdOpts(ctx)...))
		return nil
	},
}
var Cmd = &Profile{
	Name: "cmd",
	Setup: func(ctx *cli.Context) error {
		return nil
	},
}

// Local profile to run locally
var Local = &Profile{
	Name: "local",
	Setup: func(ctx *cli.Context) error {
		microAuth.DefaultAuth = jwt.NewAuth()
		microStore.DefaultStore = file.NewStore(file.WithDir(filepath.Join(user.Dir, "server", "store")))
		SetupConfigSecretKey(ctx)
		config.DefaultConfig, _ = storeConfig.NewConfig(microStore.DefaultStore, "")
		SetupJWT(ctx)

		// the registry service uses the memory registry, the other core services will use the default
		// rpc client and call the registry service
		//if ctx.Args().Get(1) == "registry" {
		//	SetupRegistry(memory.NewRegistry())
		//	//SetupRegistry(etcd.NewRegistry(registry.Addrs("localhost")))
		//
		//} else {
		//	// set the registry address
		//	registry.DefaultRegistry.Init(
		//		registry.Addrs("localhost:8000"),
		//	)
		//
		//	SetupRegistry(registry.DefaultRegistry)
		//}
		SetupRegistry(etcd.NewRegistry(EtcdOpts(ctx)...))

		// the broker service uses the memory broker, the other core services will use the default
		// rpc client and call the broker service
		if ctx.Args().Get(1) == "broker" {
			SetupBroker(memBroker.NewBroker())
		} else {
			broker.DefaultBroker.Init(
				broker.Addrs("localhost:8003"),
			)
			SetupBroker(broker.DefaultBroker)
		}

		// set the store in the model
		model.DefaultModel = model.NewModel(
			model.WithStore(microStore.DefaultStore),
		)

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

		// Configure tracing with Jaeger (forced tracing):
		tracingServiceName := ctx.Args().Get(1)
		if len(tracingServiceName) == 0 {
			tracingServiceName = "Micro"
		}
		openTracer, _, err := jaeger.New(
			opentelemetry.WithServiceName(tracingServiceName),
			opentelemetry.WithSamplingRate(1),
		)
		if err != nil {
			logger.Fatalf("Error configuring opentracing: %v", err)
		}
		opentelemetry.DefaultOpenTracer = openTracer

		return nil
	},
}

// Kubernetes profile to run on kubernetes with zero deps. Designed for use with the micro helm chart
var Kubernetes = &Profile{
	Name: "kubernetes",
	Setup: func(ctx *cli.Context) (err error) {
		microAuth.DefaultAuth = jwt.NewAuth()
		SetupJWT(ctx)

		microRuntime.DefaultRuntime = kubernetes.NewRuntime()
		microBuilder.DefaultBuilder, err = golang.NewBuilder()
		if err != nil {
			logger.Fatalf("Error configuring golang builder: %v", err)
		}

		microEvents.DefaultStream, err = memStream.NewStream()
		if err != nil {
			logger.Fatalf("Error configuring stream: %v", err)
		}

		microStore.DefaultStore = file.NewStore(file.WithDir("/store"))
		microStore.DefaultBlobStore, err = file.NewBlobStore(file.WithDir("/store/blob"))
		if err != nil {
			logger.Fatalf("Error configuring file blob store: %v", err)
		}

		// the registry service uses the memory registry, the other core services will use the default
		// rpc client and call the registry service
		if ctx.Args().Get(1) == "registry" {
			SetupRegistry(memory.NewRegistry())
		}

		// the broker service uses the memory broker, the other core services will use the default
		// rpc client and call the broker service
		if ctx.Args().Get(1) == "broker" {
			SetupBroker(memBroker.NewBroker())
		}

		config.DefaultConfig, err = storeConfig.NewConfig(microStore.DefaultStore, "")
		if err != nil {
			logger.Fatalf("Error configuring config: %v", err)
		}
		SetupConfigSecretKey(ctx)

		// Use k8s routing which is DNS based
		router.DefaultRouter = k8sRouter.NewRouter()
		client.DefaultClient.Init(client.Router(router.DefaultRouter))

		// Configure tracing with Jaeger:
		tracingServiceName := ctx.Args().Get(1)
		if len(tracingServiceName) == 0 {
			tracingServiceName = "Micro"
		}
		openTracer, _, err := jaeger.New(
			opentelemetry.WithServiceName(tracingServiceName),
			opentelemetry.WithTraceReporterAddress("localhost:6831"),
		)
		if err != nil {
			logger.Fatalf("Error configuring opentracing: %v", err)
		}
		opentelemetry.DefaultOpenTracer = openTracer

		return nil
	},
}

// Service is the default for any services run
var Service = &Profile{
	Name: "service",
	Setup: func(ctx *cli.Context) error {

		SetupRegistry(etcd.NewRegistry(EtcdOpts(ctx)...))

		if !metrics.IsSet() {
			prometheusReporter, err := prometheus.New()
			if err != nil {
				logger.Fatal(err)
			}
			metrics.SetDefaultMetricsReporter(prometheusReporter)
		}

		reporterAddress := ctx.String("tracing_reporter_address")
		if len(reporterAddress) == 0 {
			reporterAddress = jaeger.DefaultReporterAddress
		}
		// Configure tracing with Jaeger (forced tracing):
		openTracer, _, err := jaeger.New(
			opentelemetry.WithServiceName(ctx.String("service_name")),
			opentelemetry.WithTraceReporterAddress(reporterAddress),
		)
		logger.Infof("Setting jaeger global tracer to %s", reporterAddress)
		if err != nil {
			logger.Fatalf("Error configuring opentracing: %v", err)
		}
		opentracing.SetGlobalTracer(openTracer)
		opentelemetry.DefaultOpenTracer = openTracer

		//sync.Default = syncEtcd.NewSync(sync.Nodes("etcd-cluster"))
		//if err := sync.Default.Init(syncEtcdOpts(ctx)...); err != nil {
		//	logger.Fatal("Error configuring etcd sync: %v", err)
		//}
		config.DefaultConfig = apollo.NewConfig(apollo.WithConfig(&agollo.Conf{
			AppID:          os.Getenv("MICRO_NAMESPACE"),
			Cluster:        "default",
			NameSpaceNames: []string{os.Getenv("MICRO_SERVICE_NAME") + ".yaml"},
			MetaAddr:       os.Getenv("MICRO_CONFIG_ADDRESS"),
			CacheDir:       filepath.Join(os.TempDir(), "apollo"),
		}))

		return nil
	},
}

// Test profile is used for the go test suite
var Test = &Profile{
	Name: "test",
	Setup: func(ctx *cli.Context) error {
		//microAuth.DefaultAuth = noop.NewAuth()
		microAuth.DefaultAuth = jwt.NewAuth()

		microStore.DefaultStore = mem.NewStore()
		microStore.DefaultBlobStore, _ = file.NewBlobStore()
		config.DefaultConfig, _ = storeConfig.NewConfig(microStore.DefaultStore, "")
		//SetupRegistry(memory.NewRegistry())
		SetupRegistry(etcd.NewRegistry(registry.Addrs("localhost")))
		// set the store in the model
		model.DefaultModel = model.NewModel(
			model.WithStore(microStore.DefaultStore),
		)
		microRuntime.DefaultRuntime = local.NewRuntime()
		return nil
	},
}

// SetupRegistry configures the registry
func SetupRegistry(reg registry.Registry) {
	registry.DefaultRegistry = reg
	router.DefaultRouter = regRouter.NewRouter(router.Registry(reg), router.Cache())
	client.DefaultClient.Init(client.Registry(reg), client.Router(router.DefaultRouter))
	server.DefaultServer.Init(server.Registry(reg))
}

// SetupBroker configures the broker
func SetupBroker(b broker.Broker) {
	broker.DefaultBroker = b
	client.DefaultClient.Init(client.Broker(b))
	server.DefaultServer.Init(server.Broker(b))
}

// SetupJWT configures the default internal system rules
func SetupJWT(ctx *cli.Context) {
	for _, rule := range inAuth.SystemRules {
		if err := microAuth.DefaultAuth.Grant(rule); err != nil {
			logger.Fatal("Error creating default rule: %v", err)
		}
	}
}

func SetupConfigSecretKey(ctx *cli.Context) {
	key := ctx.String("config_secret_key")
	if len(key) == 0 {
		k, err := user.GetConfigSecretKey()
		if err != nil {
			logger.Fatal("Error getting config secret: %v", err)
		}
		os.Setenv("MICRO_CONFIG_SECRET_KEY", k)
	}
}

// natsStreamOpts returns a slice of options which should be used to configure nats
func syncEtcdOpts(ctx *cli.Context) []sync.Option {
	// setup registry
	opts := []sync.Option{
	}

	// Parse registry TLS certs
	if len(ctx.String("registry_tls_cert")) > 0 || len(ctx.String("registry_tls_key")) > 0 {
		cert, err := tls.LoadX509KeyPair(ctx.String("registry_tls_cert"), ctx.String("registry_tls_key"))
		if err != nil {
			logger.Fatalf("Error loading registry tls cert: %v", err)
		}

		// load custom certificate authority
		caCertPool := x509.NewCertPool()
		if len(ctx.String("registry_tls_ca")) > 0 {
			crt, err := ioutil.ReadFile(ctx.String("registry_tls_ca"))
			if err != nil {
				logger.Fatalf("Error loading registry tls certificate authority: %v", err)
			}
			caCertPool.AppendCertsFromPEM(crt)
		}

		cfg := &tls.Config{Certificates: []tls.Certificate{cert}, RootCAs: caCertPool}
		opts = append(opts, sync.TLSConfig(cfg))
	}
	if len(ctx.String("registry_address")) > 0 {
		addresses := strings.Split(ctx.String("registry_address"), ",")
		opts = append(opts, sync.Nodes(addresses...))
	}
	opts = append(opts, sync.Prefix(os.Getenv("MICRO_SERVICE_NAME")))
	return opts
}
func EtcdOpts(ctx *cli.Context) []registry.Option  {
	// setup registry
	registryOpts := []registry.Option {
		registry.Addrs("etcd-cluster.default.svc.cluster.local"),
	}

	// Parse registry TLS certs
	if len(ctx.String("registry_tls_cert")) > 0 || len(ctx.String("registry_tls_key")) > 0 {
		cert, err := tls.LoadX509KeyPair(ctx.String("registry_tls_cert"), ctx.String("registry_tls_key"))
		if err != nil {
			logger.Fatalf("Error loading registry tls cert: %v", err)
		}

		// load custom certificate authority
		caCertPool := x509.NewCertPool()
		if len(ctx.String("registry_tls_ca")) > 0 {
			crt, err := ioutil.ReadFile(ctx.String("registry_tls_ca"))
			if err != nil {
				logger.Fatalf("Error loading registry tls certificate authority: %v", err)
			}
			caCertPool.AppendCertsFromPEM(crt)
		}

		cfg := &tls.Config{Certificates: []tls.Certificate{cert}, RootCAs: caCertPool}
		registryOpts = append(registryOpts, registry.TLSConfig(cfg))
	}

	if len(ctx.String("registry_address")) > 0 {
		addresses := strings.Split(ctx.String("registry_address"), ",")
		registryOpts = append(registryOpts, registry.Addrs(addresses...))
	}
	return registryOpts
}