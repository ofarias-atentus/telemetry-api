# syntax=docker/dockerfile:1
#
# Build:    docker build -t atdevice-api .
# Migrate:  run as a one-off BEFORE serve (serve never migrates):
#             docker run --rm --env-file .env.docker atdevice-api migrate status
#             docker run --rm --env-file .env.docker atdevice-api migrate up
#           Back up with pg_dump first: migrations can stall ingestion.
#           For databases created by the old psql scripts use `migrate baseline`.
# Serve:    docker run --env-file .env.docker -p 8080:8080 --stop-timeout 30 atdevice-api
#           serve refuses to start unless the database is at the latest
#           migration version (the migrations are embedded in the binary).
#
# Runtime config: environment variables (-e / --env-file .env.docker; template
# in deploy/.env.docker.example). Never bake it into the image.
#   - Required: DB_HOST, DB_USER, DB_PASSWORD, DB_NAME.
#   - Optional: SERVER_PORT (8080), DB_PORT, DB_SSLMODE, DB_MAX_CONNS,
#     DB_MIN_CONNS, LOG_LEVEL, LOG_FORMAT (text|json). See .env.example.
#   - serve listens on all interfaces, with no auth and no TLS; publish only
#     to trusted networks or put a proxy in front.
#
# Shutdown: serve handles SIGTERM with a 15s graceful budget. Give the
# container a stop grace period of at least 30s (compose:
# stop_grace_period: 30s); Docker's default is only 10s.
#
# No HEALTHCHECK: the distroless image has no shell/curl and the API has no
# health endpoint.

FROM --platform=$BUILDPLATFORM golang:1.27.1-bookworm AS build
ARG TARGETOS
ARG TARGETARCH
ENV GOTOOLCHAIN=local
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -mod=readonly -trimpath -ldflags="-s -w" -o /out/atdevice-api ./cmd/atdevice-api

FROM gcr.io/distroless/static-debian12:nonroot

ARG SOURCE_URL=""
LABEL org.opencontainers.image.title="atdevice-api" \
      org.opencontainers.image.description="Device inventory and hardware telemetry ingestion API (serve, migrate)" \
      org.opencontainers.image.source="${SOURCE_URL}"

COPY --from=build /out/atdevice-api /usr/local/bin/atdevice-api

# Empty working dir: no .env is picked up unless one is deliberately mounted.
WORKDIR /app
USER nonroot:nonroot
EXPOSE 8080
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/local/bin/atdevice-api"]
CMD ["serve"]
