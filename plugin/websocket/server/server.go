package gateway

import (
	"context"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/micro/micro/v3/plugin"
	"github.com/micro/micro/v3/plugin/websocket"
	"github.com/micro/micro/v3/plugin/websocket/handler"
	httpapi "github.com/micro/micro/v3/plugin/websocket/server/http"
	"github.com/micro/micro/v3/service"
	"github.com/micro/micro/v3/service/client"
	"github.com/micro/micro/v3/service/logger"
	"github.com/micro/micro/v3/service/server"
	"github.com/micro/micro/v3/util/opentelemetry"
	"github.com/micro/micro/v3/util/opentelemetry/jaeger"
	"github.com/micro/micro/v3/util/wrapper"
	"github.com/opentracing/opentracing-go"
	"github.com/urfave/cli/v2"
	pb "github.com/wolfplus2048/mcbeam-plugins/ws_session/v3/proto"
	"net/http"
)

func init() {
	plugin.Register(Plugin())
}

func Plugin() plugin.Plugin {
	return plugin.NewPlugin(plugin.WithName("websocket"),
		plugin.WithCommand(&cli.Command{
			Name:   "websocket",
			Usage:  "Run the websocket gateway",
			Action: Run,
			Flags:  Flags,
		}),
	)
}

var Flags = []cli.Flag{
	&cli.StringFlag{
		Name:    "address",
		Usage:   "Set the gateway address e.g :8080",
		EnvVars: []string{"MICRO_WS_ADDRESS"},
	},
}

var (
	name    = "websocket"
	address = ":3251"
	wsPath  = "/websocket2"
)

func Run(ctx *cli.Context) error {
	if len(ctx.String("server_name")) > 0 {
		name = ctx.String("server_name")
	}

	if len(ctx.String("wspath")) > 0 {
		wsPath = ctx.String("wspath")
	}

	reporterAddress := ctx.String("tracing_reporter_address")
	if len(reporterAddress) == 0 {
		reporterAddress = jaeger.DefaultReporterAddress
	}
	// Create a new Jaeger opentracer:
	openTracer, traceCloser, err := jaeger.New(
		opentelemetry.WithServiceName("Gate"),
		opentelemetry.WithTraceReporterAddress(reporterAddress),
	)
	logger.Infof("Setting jaeger global tracer to %s", reporterAddress)
	defer traceCloser.Close() // Make sure we flush any pending traces before shutdown:
	if err != nil {
		logger.Warnf("Unable to prepare a Jaeger tracer: %s", err)
	} else {
		// Set the global default opentracing tracer:
		opentracing.SetGlobalTracer(openTracer)
	}
	opentelemetry.DefaultOpenTracer = openTracer

	srv := service.New(
		service.Name(name),
	)
	pb.RegisterSessionHandler(srv.Server(), &handler.Handler{})
	websocket.OnSessionClose(func(s *websocket.Session) {
		c := srv.Client()
		m := c.NewMessage("session.close", &pb.SessionClose{ServerId: server.DefaultServer.Options().Id,
			SessionId: s.SID()})
		err := c.Publish(context.Background(), m, client.PublishNamespace(s.GetDomain()))
		logger.Infof("session.close broker:%s, sid:%s, domain:%s, ret:%v", c.Options().Broker.String(), s.SID(), s.GetDomain(), err)
	})

	var h http.Handler
	r := mux.NewRouter()
	h = r
	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			return
		}
		response := fmt.Sprintf(`{"websocket api version": "%s"}`, ctx.App.Version)
		w.Write([]byte(response))
		return
	})
	r.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {})
	r.Handle(wsPath, websocket.NewHandler())
	h = wrapper.HTTPWrapper(h)

	api := httpapi.NewServer(address)
	api.Init()
	api.Handle("/", h)

	if err := api.Start(); err != nil {
		logger.Fatal(err)
	}
	if err := srv.Run(); err != nil {
		logger.Fatal(err)
	}
	if err := api.Stop(); err != nil {
		logger.Fatal(err)
	}
	return nil
}
