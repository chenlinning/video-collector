# syntax=docker/dockerfile:1.7

ARG NODE_IMAGE=node:22-alpine
ARG GOLANG_IMAGE=golang:1.26.5-alpine
ARG ALPINE_IMAGE=alpine:3.21
ARG YTDLP_VERSION=2026.7.4

FROM ${NODE_IMAGE} AS web-builder
WORKDIR /src
RUN corepack enable && corepack prepare pnpm@10.26.2 --activate
COPY package.json pnpm-lock.yaml ./
RUN --mount=type=cache,id=video-collector-pnpm,target=/pnpm/store \
    pnpm config set store-dir /pnpm/store && \
    pnpm install --frozen-lockfile --ignore-scripts
COPY tsconfig.json vite.renderer.config.ts ./
COPY src/renderer/ ./src/renderer/
COPY src/shared/ ./src/shared/
RUN pnpm build:web

FROM ${GOLANG_IMAGE} AS server-builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,id=video-collector-gomod,target=/go/pkg/mod go mod download
COPY server/ ./server/
RUN --mount=type=cache,id=video-collector-gomod,target=/go/pkg/mod \
    --mount=type=cache,id=video-collector-gobuild,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/video-collector ./server/cmd/video-collector

FROM ${ALPINE_IMAGE}
ARG YTDLP_VERSION
RUN apk add --no-cache ca-certificates ffmpeg python3 py3-pip tzdata && \
    pip3 install --break-system-packages --no-cache-dir "yt-dlp[default,curl-cffi]==${YTDLP_VERSION}" && \
    yt-dlp --list-impersonate-targets | grep -q curl_cffi && \
    addgroup -S collector && \
    adduser -S -D -H -G collector -s /sbin/nologin collector && \
    mkdir -p /app/web /app/cache/tasks /app/cache/tmp && \
    chown -R collector:collector /app

COPY --from=server-builder --chown=collector:collector /out/video-collector /app/video-collector
COPY --from=web-builder --chown=collector:collector /src/dist-web/ /app/web/

USER collector
EXPOSE 8787
ENV VIDEO_COLLECTOR_LISTEN=0.0.0.0:8787 \
    VIDEO_COLLECTOR_WEB_ROOT=/app/web \
    VIDEO_COLLECTOR_TEMP_ROOT=/app/cache/tasks \
    TMPDIR=/app/cache/tmp \
    YTDLP_PATH=/usr/bin/yt-dlp \
    FFMPEG_PATH=/usr/bin/ffmpeg

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD wget -q -T 3 -O /dev/null http://localhost:8787/health || exit 1

ENTRYPOINT ["/app/video-collector"]
