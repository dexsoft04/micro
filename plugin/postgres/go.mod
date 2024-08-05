module github.com/micro/micro/plugin/postgres/v3

go 1.20

require (
	github.com/lib/pq v1.10.2
	github.com/micro/micro/v3 v3.3.0
	github.com/pkg/errors v0.9.1
	github.com/stretchr/testify v1.8.4
)

replace github.com/micro/micro/v3 => ../..
