# build app
FROM --platform=$BUILDPLATFORM golang:1.24-alpine3.21 AS app-builder
RUN apk add --no-cache git tzdata

ENV SERVICE=tqm

WORKDIR /src

# Cache Go modules
COPY go.mod go.sum ./
RUN go mod download

COPY . ./

ARG VERSION=dev
ARG REVISION=dev
ARG BUILDTIME
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

RUN --network=none --mount=target=. \
export GOOS=$TARGETOS; \
export GOARCH=$TARGETARCH; \
[ "$GOARCH" = "amd64" ] && export GOAMD64="$TARGETVARIANT"; \
[ "$GOARCH" = "arm64" ] && case "$TARGETVARIANT" in *.*) export GOARM64="$TARGETVARIANT";; esac; \
[ "$GOARCH" = "arm64" ] && case "$TARGETVARIANT" in *.*) : ;; v*) export GOARM64="$TARGETVARIANT.0";; esac; \
[ "$GOARCH" = "arm" ] && [ "$TARGETVARIANT" = "v6" ] && export GOARM=6; \
[ "$GOARCH" = "arm" ] && [ "$TARGETVARIANT" = "v7" ] && export GOARM=7; \
echo $GOARCH $GOOS $GOARM$GOAMD64$GOARM64; \
go build -ldflags "-s -w -X github.com/autobrr/tqm/pkg/runtime.Version=${VERSION} -X github.com/autobrr/tqm/pkg/runtime.GitCommit=${REVISION} -X github.com/autobrr/tqm/pkg/runtime.Timestamp=${BUILDTIME}" -o /out/bin/tqm cmd/tqm/main.go

# build runner
FROM alpine:latest AS runner

LABEL org.opencontainers.image.source="https://github.com/autobrr/tqm"
LABEL org.opencontainers.image.licenses="GPL-3.0"
LABEL org.opencontainers.image.base.name="alpine:latest"

ENV CONFIG_DIR="/config" \
    PUID=1000 \
    PGID=1000

RUN apk --no-cache add ca-certificates curl tzdata jq && \
    addgroup -g $PGID abc && \
    adduser -D -u $PUID -G abc abc

WORKDIR /app
VOLUME /config
EXPOSE 7337

COPY --link --from=app-builder /out/bin/tqm /usr/local/bin/

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD /usr/local/bin/tqm --help > /dev/null || exit 1

USER abc

ENTRYPOINT ["/usr/local/bin/tqm", "--config-dir", "/config"]