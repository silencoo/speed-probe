# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=container
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
    -buildvcs=false -tags with_utls -trimpath -ldflags "-s -w -X main.COMMIT=$VERSION" \
    -o /out/speed-probe .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -g 1000 probe && adduser -D -u 1000 -G probe probe \
    && mkdir /data && chown probe:probe /data
COPY --from=build /out/speed-probe /usr/local/bin/speed-probe
USER 1000:1000
WORKDIR /data
EXPOSE 8765
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD nc -z -w 2 127.0.0.1 8765 || exit 1
ENTRYPOINT ["speed-probe"]
CMD ["server", "-bind", "127.0.0.1:8765", "-clients", "/data/clients.json"]
