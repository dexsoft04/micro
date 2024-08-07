module github.com/micro/micro/plugin/s3/v3

go 1.20

require (
	github.com/aws/aws-sdk-go v1.55.5
	github.com/micro/micro/v3 v3.19.0
	github.com/stretchr/testify v1.8.1
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/patrickmn/go-cache v2.1.0+incompatible // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

//replace github.com/micro/micro/v3 => ../..
