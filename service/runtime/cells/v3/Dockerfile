FROM golang:1.15-alpine AS builder
RUN mkdir -p /src
WORKDIR /src
COPY loader .
RUN go env -w GOPROXY=https://goproxy.io,direct \
    && go build  .
FROM alpine:latest
COPY --from=builder /src/loader /loader
CMD ./loader