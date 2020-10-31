module github.com/micro/micro/profile/dev/v3

go 1.15

require (
	github.com/coreos/bbolt v1.3.3 // indirect
	github.com/coreos/go-systemd v0.0.0-20190719114852-fd7a80b32e1f // indirect
	github.com/micro/micro/plugin/etcd/v3 v3.0.0-00010101000000-000000000000
	github.com/micro/micro/plugin/nats/broker/v3 v3.0.0-20201030211035-7b29d3bd49f5
	github.com/micro/micro/v3 v3.0.0-beta.7
	github.com/prometheus/client_golang v1.7.1 // indirect
	github.com/urfave/cli/v2 v2.2.0
)

replace google.golang.org/grpc => google.golang.org/grpc v1.26.0

replace github.com/micro/micro/plugin/etcd/v3 => ../../plugin/etcd

replace github.com/micro/micro/v3 => ../..
