# syntax=docker/dockerfile:1.7
FROM golang:1.22-alpine AS builder
WORKDIR /src

# Cache shared module + service module separately
COPY shared/go-common /src/shared/go-common
COPY services/user-svc /src/services/user-svc
COPY go.work /src/go.work

WORKDIR /src/services/user-svc
RUN go mod download && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /out/server /usr/local/bin/server
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/server"]
