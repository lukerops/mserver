# syntax=docker/dockerfile:1

ARG GO_VERSION=1.27
ARG DEBIAN_VERSION=trixie

# The build always runs on the builder's native platform and cross-compiles
# to the target one, so multi-arch images do not need QEMU emulation.
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
WORKDIR /src

# Dependencies first: this layer is only invalidated when go.mod/go.sum change.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=bind,source=go.mod,target=go.mod \
    --mount=type=bind,source=go.sum,target=go.sum \
    go mod download

ARG TARGETOS
ARG TARGETARCH
# modernc.org/sqlite is pure Go, so CGO is not needed and the binary is static.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=bind,target=. \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/mserver ./cmd/server

# Debian instead of distroless: the storage runs ffprobe (from the ffmpeg
# package) to analyze the media files.
FROM debian:${DEBIAN_VERSION}-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ffmpeg \
    && rm -rf /var/lib/apt/lists/*


COPY --from=build /out/mserver /usr/local/bin/mserver

ENV MSERVER_HTTP_ADDR=:8096 \
    MSERVER_DATA_DIR=/data \
    MSERVER_FFPROBE_PATH=/usr/bin/ffprobe

VOLUME /data
EXPOSE 8096

ENTRYPOINT ["/usr/local/bin/mserver"]
