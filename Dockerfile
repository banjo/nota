# syntax=docker/dockerfile:1
# The Nota sync server (nota serve): a static Go binary on Alpine.
#   docker build -t nota .
#   docker run -p 8080:8080 -v nota-data:/data nota
# Configuration is environment variables; see docs/deploy.md.

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/banjo/nota/internal/cli.Version=${VERSION}" \
    -o /out/nota ./cmd/nota

FROM alpine:3.22
# ca-certificates for outbound TLS, tini to reap the children of server
# hooks and forward signals, curl for the platform's health checks, sh
# (busybox) for hooks.
RUN apk add --no-cache ca-certificates tini curl \
    && addgroup -S -g 10001 nota && adduser -S -D -H -u 10001 -G nota -h /data nota \
    && mkdir -p /data && chown nota:nota /data
COPY --from=build /out/nota /usr/local/bin/nota
USER nota
ENV NOTA_SERVER_ADDR=:8080 \
    NOTA_SERVER_DATA=/data \
    NOTA_SERVER_LOG_FORMAT=json \
    NOTA_SERVER_HOOKS=off \
    HOME=/data
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -fsS http://127.0.0.1:8080/healthz || exit 1
# tini runs as PID 1 and passes SIGTERM on; nota serve then drains
# requests and closes SSE streams (10 s grace).
ENTRYPOINT ["/sbin/tini", "--", "nota"]
CMD ["serve"]
