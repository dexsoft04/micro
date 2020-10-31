module github.com/micro/micro/profile/ci/v3

go 1.15

require (
	github.com/micro/micro/plugin/etcd/v3 v3.0.0-00010101000000-000000000000

	github.com/micro/micro/v3 v3.0.0-beta.6
	github.com/urfave/cli/v2 v2.2.0
)

replace google.golang.org/grpc => google.golang.org/grpc v1.26.0

replace github.com/micro/micro/v3 => ../..

replace github.com/micro/micro/plugin/etcd/v3 => ../../plugin/etcd

