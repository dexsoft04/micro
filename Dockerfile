ARG GO_IMAGE=golang:1.26.4-alpine

FROM ${GO_IMAGE} AS go

FROM alpine:3.12.1 AS builder

COPY --from=go /usr/local/go/ /usr/local/go/
ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPROXY="https://goproxy.cn,direct"
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories \
    && apk --no-cache add make git gcc libtool musl-dev

COPY go.mod .
COPY go.sum .
COPY plugin/etcd/go.mod plugin/etcd/go.sum ./plugin/etcd/
COPY plugin/prometheus/go.mod plugin/prometheus/go.sum ./plugin/prometheus/
RUN go mod download
COPY . /
RUN cd /cmd/platform && go build -a -installsuffix cgo -ldflags "-s -w" -o /micro . ; rm -rf $GOPATH/pkg/mod

FROM alpine:3.12.1
COPY --from=go /usr/local/go/ /usr/local/go/
ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPROXY="https://goproxy.cn,direct"

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories \
    && apk --no-cache add make git gcc libtool musl-dev openssh-client
RUN apk --no-cache add ca-certificates && rm -rf /var/cache/apk/* /tmp/*
RUN mkdir -p /root/.ssh && ssh-keyscan gitee.com >> /root/.ssh/known_hosts
COPY --from=builder /micro /micro
ENTRYPOINT ["/micro"]
