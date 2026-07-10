module github.com/micro/micro/cmd/platform

go 1.26.4

replace google.golang.org/grpc => google.golang.org/grpc v1.26.0

replace github.com/micro/micro/v3 => ../..

//replace github.com/micro/micro/profile/platform/v3 => ../../profile/platform

//replace github.com/micro/micro/plugin/cockroach/v3 => ../../plugin/cockroach

replace github.com/coreos/go-systemd => github.com/coreos/go-systemd/v22 v22.0.0

require github.com/micro/micro/v3 v3.3.0

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bitly/go-simplejson v0.5.0 // indirect
	github.com/bmizerany/assert v0.0.0-20160611221934-b7ed37b82869 // indirect
	github.com/caddyserver/certmagic v0.10.6 // indirect
	github.com/cenkalti/backoff/v4 v4.0.0 // indirect
	github.com/cespare/xxhash/v2 v2.1.1 // indirect
	github.com/chzyer/readline v0.0.0-20180603132655-2972be24d48e // indirect
	github.com/chzyer/test v1.0.0 // indirect
	github.com/cloudflare/cloudflare-go v0.10.9 // indirect
	github.com/coreos/go-semver v0.2.0 // indirect
	github.com/coreos/go-systemd/v22 v22.0.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/desertbit/timer v0.0.0-20180107155436-c41aec40b27f // indirect
	github.com/dgrijalva/jwt-go v3.2.0+incompatible // indirect
	github.com/dustin/go-humanize v1.0.0 // indirect
	github.com/evanphx/json-patch/v5 v5.0.0 // indirect
	github.com/ghodss/yaml v1.0.0 // indirect
	github.com/go-acme/lego/v3 v3.4.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/google/go-querystring v1.0.0 // indirect
	github.com/google/uuid v1.1.2 // indirect
	github.com/gorilla/handlers v1.4.2 // indirect
	github.com/gorilla/mux v1.7.3 // indirect
	github.com/gorilla/websocket v1.4.2 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.1 // indirect
	github.com/hashicorp/go-retryablehttp v0.6.4 // indirect
	github.com/hashicorp/go-version v1.2.1 // indirect
	github.com/hpcloud/tail v1.0.0 // indirect
	github.com/improbable-eng/grpc-web v0.13.0 // indirect
	github.com/json-iterator/go v1.1.11 // indirect
	github.com/klauspost/cpuid v1.3.1 // indirect
	github.com/lib/pq v1.10.4 // indirect
	github.com/mattn/go-runewidth v0.0.13 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.1 // indirect
	github.com/miekg/dns v1.1.27 // indirect
	github.com/minio/md5-simd v1.1.0 // indirect
	github.com/minio/minio-go/v7 v7.0.12 // indirect
	github.com/minio/sha256-simd v0.1.1 // indirect
	github.com/mitchellh/colorstring v0.0.0-20190213212951-d06e56a500db // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/mitchellh/hashstructure v1.0.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.1 // indirect
	github.com/nats-io/nats.go v1.13.1-0.20220308171302-2f2f6968e98d // indirect
	github.com/nats-io/nkeys v0.3.0 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/nats-io/stan.go v0.10.2 // indirect
	github.com/nightlyone/lockfile v1.0.0 // indirect
	github.com/olekukonko/tablewriter v0.0.4 // indirect
	github.com/opentracing/opentracing-go v1.2.0 // indirect
	github.com/oxtoacart/bpool v0.0.0-20190530202638-03653db5a59c // indirect
	github.com/patrickmn/go-cache v2.1.0+incompatible // indirect
	github.com/philchia/agollo/v4 v4.1.3 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_golang v1.11.0 // indirect
	github.com/prometheus/client_model v0.2.0 // indirect
	github.com/prometheus/common v0.26.0 // indirect
	github.com/prometheus/procfs v0.7.3 // indirect
	github.com/rivo/uniseg v0.2.0 // indirect
	github.com/rs/cors v1.7.0 // indirect
	github.com/rs/xid v1.2.1 // indirect
	github.com/russross/blackfriday/v2 v2.0.1 // indirect
	github.com/schollz/progressbar/v3 v3.8.2 // indirect
	github.com/serenize/snaker v0.0.0-20171204205717-a683aaf2d516 // indirect
	github.com/shurcooL/sanitized_anchor_name v1.0.0 // indirect
	github.com/sirupsen/logrus v1.8.1 // indirect
	github.com/stoewer/go-strcase v1.2.0 // indirect
	github.com/stretchr/objx v0.1.1 // indirect
	github.com/stretchr/testify v1.7.0 // indirect
	github.com/teris-io/shortid v0.0.0-20171029131806-771a37caa5cf // indirect
	github.com/uber/jaeger-client-go v2.29.1+incompatible // indirect
	github.com/uber/jaeger-lib v2.4.1+incompatible // indirect
	github.com/urfave/cli/v2 v2.3.0 // indirect
	github.com/wolfplus2048/mcbeam-plugins/config/apollo/v3 v3.0.0-20210826053511-6966876170a7 // indirect
	github.com/wolfplus2048/mcbeam-plugins/session/v3 v3.0.0-20210803053144-09b3e552dd3e // indirect
	github.com/wolfplus2048/mcbeam-plugins/store/minio/v3 v3.0.0-20211020053600-a995302ea708 // indirect
	github.com/wolfplus2048/mcbeam-plugins/ws_session/v3 v3.0.0-20211014071105-a692112a7005 // indirect
	github.com/xanzy/go-gitlab v0.35.1 // indirect
	github.com/xlab/treeprint v0.0.0-20181112141820-a009c3971eca // indirect
	go.etcd.io/bbolt v1.3.6 // indirect
	go.etcd.io/etcd v0.5.0-alpha.5.0.20200425165423-262c93980547 // indirect
	go.uber.org/atomic v1.6.0 // indirect
	go.uber.org/multierr v1.5.0 // indirect
	go.uber.org/zap v1.16.0 // indirect
	golang.org/x/crypto v0.1.0 // indirect
	golang.org/x/net v0.1.0 // indirect
	golang.org/x/oauth2 v0.0.0-20190604053449-0f29369cfe45 // indirect
	golang.org/x/sys v0.1.0 // indirect
	golang.org/x/term v0.1.0 // indirect
	golang.org/x/text v0.4.0 // indirect
	golang.org/x/time v0.0.0-20211116232009-f0f3c7e86c11 // indirect
	google.golang.org/appengine v1.6.1 // indirect
	google.golang.org/genproto v0.0.0-20200526211855-cb27e3aa2013 // indirect
	google.golang.org/grpc v1.27.0 // indirect
	google.golang.org/protobuf v1.26.0 // indirect
	gopkg.in/fsnotify.v1 v1.4.7 // indirect
	gopkg.in/ini.v1 v1.57.0 // indirect
	gopkg.in/square/go-jose.v2 v2.4.1 // indirect
	gopkg.in/tomb.v1 v1.0.0-20141024135613-dd632973f1e7 // indirect
	gopkg.in/yaml.v2 v2.3.0 // indirect
	gopkg.in/yaml.v3 v3.0.0-20200313102051-9f266ea9e77c // indirect
)
