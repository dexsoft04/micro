FROM alpine:3.12.1 as builder

COPY --from=golang:1.15-alpine /usr/local/go/ /usr/local/go/
ENV PATH="/usr/local/go/bin:${PATH}"

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories
RUN apk --no-cache add make git gcc libtool musl-dev

COPY go.mod .
COPY go.sum .

COPY . /
RUN go env -w GOPROXY="goproxy.cn,direct" \
    && go mod download
RUN make ; rm -rf $GOPATH/pkg/mod

FROM alpine:3.12.1
COPY --from=golang:1.15-alpine /usr/local/go/ /usr/local/go/
ENV PATH="/usr/local/go/bin:${PATH}"

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories
RUN apk --no-cache add make git gcc libtool musl-dev openssh-client \
    && apk --no-cache add ca-certificates && rm -rf /var/cache/apk/* /tmp/*

RUN mkdir -p /root/.ssh && ssh-keyscan gitee.com >> /root/.ssh/known_hosts

COPY --from=builder /micro /micro
ENTRYPOINT ["/micro"]
