# syntax=docker/dockerfile:1
# Image API Photo Annotated untuk Raspberry Pi 4 (linux/arm64). Build silang tanpa emulasi:
#   docker buildx build --platform linux/arm64 -t photo-annotated:<tag> --load .
# Tahap build berjalan di arsitektur mesin build; hanya biner statis yang disalin ke image akhir.

FROM --platform=$BUILDPLATFORM golang:1.26.6-bookworm AS build
ARG TARGETOS=linux
ARG TARGETARCH=arm64
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/api ./cmd/migrate ./cmd/maintenance ./cmd/healthcheck

# distroless static: tanpa shell/paket, pengguna nonroot = UID/GID 65532 (pemilik folder foto di SSD)
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ /usr/local/bin/
USER 65532:65532
EXPOSE 8080
ENV CONFIG_PATH=/etc/photo-annotated/config.json
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 CMD ["/usr/local/bin/healthcheck"]
ENTRYPOINT ["/usr/local/bin/api"]
