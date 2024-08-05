module github.com/micro/micro/v3

go 1.20

require (
	github.com/bitly/go-simplejson v0.5.0
	github.com/caddyserver/certmagic v0.10.6
	github.com/chzyer/readline v0.0.0-20180603132655-2972be24d48e
	github.com/davecgh/go-spew v1.1.1
	github.com/dustin/go-humanize v1.0.0
	github.com/evanphx/json-patch/v5 v5.5.0
	github.com/fsnotify/fsnotify v1.4.9
	github.com/getkin/kin-openapi v0.26.0
	github.com/go-acme/lego/v3 v3.4.0
	github.com/gofrs/uuid v4.0.0+incompatible
	github.com/golang-jwt/jwt v0.0.0-20210529014511-0f726ea0e725
	github.com/golang/protobuf v1.5.3
	github.com/google/uuid v1.3.1
	github.com/gorilla/handlers v1.5.1
	github.com/gorilla/mux v1.7.3
	github.com/gorilla/websocket v1.4.2
	github.com/hashicorp/go-version v1.2.1
	github.com/hpcloud/tail v1.0.0
	github.com/kr/pretty v0.3.1
	github.com/micro/micro/plugin/nats/broker/v3 v3.0.0-00010101000000-000000000000
	github.com/micro/micro/plugin/nats/stream/v3 v3.0.0-00010101000000-000000000000
	github.com/micro/micro/plugin/postgres/v3 v3.0.0-00010101000000-000000000000
	github.com/miekg/dns v1.1.27
	github.com/nightlyone/lockfile v1.0.0
	github.com/olekukonko/tablewriter v0.0.4
	github.com/onsi/gomega v1.27.6
	github.com/oxtoacart/bpool v0.0.0-20190530202638-03653db5a59c
	github.com/patrickmn/go-cache v2.1.0+incompatible
	github.com/philchia/agollo/v4 v4.1.3
	github.com/pkg/errors v0.9.1
	github.com/quic-go/quic-go v0.35.2-0.20230604200428-e1bcedc78cfe
	github.com/schollz/progressbar/v3 v3.8.2
	github.com/serenize/snaker v0.0.0-20171204205717-a683aaf2d516
	github.com/stoewer/go-strcase v1.2.0
	github.com/stretchr/objx v0.5.0
	github.com/stretchr/testify v1.8.4
	github.com/teris-io/shortid v0.0.0-20171029131806-771a37caa5cf
	github.com/uber/jaeger-client-go v2.30.0+incompatible
	github.com/urfave/cli/v2 v2.3.0
	github.com/wolfplus2048/mcbeam-plugins/config/apollo/v3 v3.0.0-20210826053511-6966876170a7
	github.com/wolfplus2048/mcbeam-plugins/session/v3 v3.0.0-20210803053144-09b3e552dd3e
	github.com/wolfplus2048/mcbeam-plugins/ws_session/v3 v3.0.0-20211015055059-04d181a0021c
	github.com/xlab/treeprint v0.0.0-20181112141820-a009c3971eca
	go.etcd.io/bbolt v1.3.5
	golang.org/x/crypto v0.25.0
	golang.org/x/net v0.25.0
	google.golang.org/genproto/googleapis/api v0.0.0-20230822172742-b8732ec3820d
	google.golang.org/grpc v1.59.0
	google.golang.org/protobuf v1.31.0
	gorm.io/driver/postgres v1.4.5
	gorm.io/driver/sqlite v1.4.3
	gorm.io/gorm v1.24.1-0.20221019064659-5dd2bb482755
)

require (
	go.etcd.io/etcd/api/v3 v3.5.11
	go.etcd.io/etcd/client/v3 v3.5.11
)

require (
	github.com/aws/aws-sdk-go v1.34.0 // indirect
	github.com/jmespath/go-jmespath v0.3.0 // indirect
	github.com/micro/micro/plugin/s3/v3 v3.0.0-00010101000000-000000000000 // indirect
)

replace (
	github.com/micro/micro/plugin/etcd/v3 => ./plugin/etcd
	github.com/micro/micro/plugin/nats/broker/v3 => ./plugin/nats/broker
	github.com/micro/micro/plugin/nats/stream/v3 => ./plugin/nats/stream
	github.com/micro/micro/plugin/postgres/v3 => ./plugin/postgres
	github.com/micro/micro/plugin/prometheus/v3 => ./plugin/prometheus
	github.com/micro/micro/plugin/s3/v3 => ./plugin/s3
)
