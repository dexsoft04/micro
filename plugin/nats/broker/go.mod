module github.com/micro/micro/plugin/nats/broker/v3

go 1.20

require (
	github.com/golang/protobuf v1.5.3
	github.com/micro/micro/v3 v3.3.0
	github.com/nats-io/nats.go v1.36.0
	github.com/oxtoacart/bpool v0.0.0-20190530202638-03653db5a59c
)

replace github.com/micro/micro/v3 => ../../..
