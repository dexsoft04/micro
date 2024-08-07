module github.com/micro/micro/profile/ci

go 1.20

require (
	github.com/micro/micro/plugin/etcd/v3 v3.3.0
	github.com/micro/micro/v3 v3.3.1-0.20210810095025-31f7876af8e2
	github.com/urfave/cli/v2 v2.3.0
)

replace (
	github.com/micro/micro/plugin/etcd/v3 => ../../plugin/etcd
	github.com/micro/micro/v3 => ../..
)
