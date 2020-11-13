module github.com/micro/micro/v3

go 1.15

require (
	github.com/bitly/go-simplejson v0.5.0
	github.com/blang/semver v3.5.1+incompatible
	github.com/boltdb/bolt v1.3.1
	github.com/caddyserver/certmagic v0.10.6
	github.com/chzyer/readline v0.0.0-20180603132655-2972be24d48e
	github.com/davecgh/go-spew v1.1.1
	github.com/dgrijalva/jwt-go v3.2.0+incompatible
	github.com/dustin/go-humanize v1.0.0
	github.com/evanphx/json-patch/v5 v5.0.0
	github.com/go-acme/lego/v3 v3.4.0
	github.com/gobwas/httphead v0.0.0-20180130184737-2c6c146eadee
	github.com/gobwas/ws v1.0.3
	github.com/golang/protobuf v1.4.3
	github.com/google/uuid v1.1.2
	github.com/gorilla/handlers v1.4.2
	github.com/gorilla/mux v1.7.3
	github.com/hashicorp/go-version v1.2.1
	github.com/hpcloud/tail v1.0.0
	github.com/improbable-eng/grpc-web v0.13.0
	github.com/juju/fslock v0.0.0-20160525022230-4d5c94c67b4b
	github.com/kr/pretty v0.2.0
	github.com/miekg/dns v1.1.27
	github.com/minio/minio-go/v7 v7.0.5
	github.com/olekukonko/tablewriter v0.0.4
	github.com/oxtoacart/bpool v0.0.0-20190530202638-03653db5a59c
	github.com/patrickmn/go-cache v2.1.0+incompatible
	github.com/pkg/errors v0.9.1
	github.com/rhysd/go-github-selfupdate v1.2.2
	github.com/serenize/snaker v0.0.0-20171204205717-a683aaf2d516
	github.com/soheilhy/cmux v0.1.4
	github.com/stretchr/testify v1.6.1
	github.com/teris-io/shortid v0.0.0-20171029131806-771a37caa5cf
	github.com/urfave/cli/v2 v2.2.0
	github.com/wolfplus2048/mcbeam-plugins/session/v3 v3.0.0-20201113103110-bd19b597ce20
	github.com/xanzy/go-gitlab v0.35.1
	github.com/xlab/treeprint v0.0.0-20181112141820-a009c3971eca
	go.etcd.io/bbolt v1.3.5
	golang.org/x/crypto v0.0.0-20200709230013-948cd5f35899
	golang.org/x/net v0.0.0-20200707034311-ab3426394381
	google.golang.org/genproto v0.0.0-20200526211855-cb27e3aa2013
	google.golang.org/grpc v1.29.1
	google.golang.org/protobuf v1.25.0
)

replace go.etcd.io/bbolt => github.com/coreos/bbolt v1.3.5

replace google.golang.org/grpc => google.golang.org/grpc v1.26.0
