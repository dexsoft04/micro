FROM golang:1.15-alpine AS builder
WORKDIR loader
COPY loader .
RUN go build  .
FROM alpine:latest
COPY --from=builder /loader /loader
CMD ./loader