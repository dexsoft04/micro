module github.com/micro/micro/cmd/platform

go 1.15

replace google.golang.org/grpc => google.golang.org/grpc v1.26.0

replace github.com/micro/micro/v3 => ../..

replace github.com/micro/micro/profile/platform/v3 => ../../profile/platform

replace github.com/micro/micro/plugin/etcd/v3 => ../../plugin/etcd

replace github.com/micro/micro/plugin/cockroach/v3 => ../../plugin/cockroach

replace github.com/micro/micro/plugin/prometheus/v3 => ../../plugin/prometheus

replace github.com/micro/micro/plugin/nats/broker/v3 => ../../plugin/nats/broker

replace github.com/micro/micro/plugin/nats/stream/v3 => ../../plugin/nats/stream

replace github.com/coreos/go-systemd => github.com/coreos/go-systemd/v22 v22.0.0

require (
	github.com/coreos/go-systemd v0.0.0-00010101000000-000000000000 // indirect
	github.com/desertbit/timer v0.0.0-20180107155436-c41aec40b27f // indirect
	github.com/gobwas/pool v0.2.1 // indirect
	github.com/gorilla/websocket v1.4.2 // indirect
	github.com/micro/micro/profile/platform/v3 v3.0.0-20200928084632-c6281c58b123
	github.com/micro/micro/v3 v3.0.0
	github.com/rs/cors v1.7.0 // indirect
)
