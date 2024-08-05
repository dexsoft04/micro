module github.com/micro/micro/plugin/nats/stream/v3

go 1.20

require (
	github.com/google/uuid v1.3.1
	github.com/micro/micro/v3 v3.3.0
	github.com/nats-io/nats.go v1.36.0
	github.com/nats-io/stan.go v0.7.0
	github.com/pkg/errors v0.9.1
)

require github.com/nats-io/nats-streaming-server v0.19.0 // indirect

replace github.com/micro/micro/v3 => ../../..
