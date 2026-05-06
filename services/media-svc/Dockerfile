# syntax=docker/dockerfile:1.7
FROM golang:1.22-alpine AS builder
WORKDIR /src

# See user-svc Dockerfile for why we set GOWORK=off here.
ENV GOWORK=off

COPY shared/go-common /src/shared/go-common
COPY services/media-svc /src/services/media-svc

WORKDIR /src/services/media-svc
RUN go mod download && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /out/server /usr/local/bin/server
EXPOSE 8083
ENTRYPOINT ["/usr/local/bin/server"]
