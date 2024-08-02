module dep-test-service

go 1.15

replace dependency => ../

require (
	dependency v0.0.0-00010101000000-000000000000
	github.com/golang/protobuf v1.5.3
	github.com/micro/micro/v3 v3.3.0
	google.golang.org/grpc v1.54.1
)

replace github.com/micro/micro/v3 => ../../..

replace google.golang.org/grpc => google.golang.org/grpc v1.26.0
