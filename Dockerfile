# 构建阶段
FROM alpine:3.18 as builder

# 从官方 Go 镜像中复制 Go 工具链
COPY --from=golang:1.20.14-alpine3.18 /usr/local/go/ /usr/local/go/
ENV PATH="/usr/local/go/bin:${PATH}"

# 安装必要的构建工具
RUN apk --no-cache add make git gcc libtool musl-dev

# 复制项目文件到工作目录
COPY . /

# 使用本地的 vendor 目录进行构建
RUN go build -mod=vendor -o /micro

# 运行阶段
FROM alpine:3.18

# 从官方 Go 镜像中复制 Go 工具链
COPY --from=golang:1.20.14-alpine3.18 /usr/local/go/ /usr/local/go/
ENV PATH="/usr/local/go/bin:${PATH}"

# 安装运行时所需的依赖项
RUN apk --no-cache add ca-certificates && rm -rf /var/cache/apk/* /tmp/*

# 从构建阶段复制构建好的可执行文件到运行阶段
COPY --from=builder /micro /micro

# 设置容器启动时的默认命令
ENTRYPOINT ["/micro"]
