# syntax=docker/dockerfile:1.7
FROM golang:1.22-alpine AS builder
WORKDIR /src

# go.work is for local IDE / multi-module dev. Inside Docker we build a single
# service in module mode — the `replace` directive in go.mod resolves the
# shared lib. GOWORK=off makes that explicit and avoids "module not in workspace"
# errors when the build context only has this service.
ENV GOWORK=off

COPY shared/go-common /src/shared/go-common
COPY services/user-svc /src/services/user-svc

WORKDIR /src/services/user-svc
RUN go mod download && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /out/server /usr/local/bin/server
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/server"]
