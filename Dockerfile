# syntax=docker/dockerfile:1.7

ARG NODE_IMAGE=node:22-alpine
ARG GOLANG_IMAGE=golang:1.26.5-alpine
ARG ALPINE_IMAGE=alpine:3.21
ARG YTDLP_VERSION=2026.7.4
ARG WHISPER_CPP_VERSION=1.8.6
ARG WHISPER_MODEL_SHA1=465707469ff3a37a2b9b8d8f89f2f99de7299dac

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

FROM ${ALPINE_IMAGE} AS whisper-builder
ARG WHISPER_CPP_VERSION
ARG WHISPER_MODEL_SHA1
RUN apk add --no-cache build-base cmake curl git
WORKDIR /src
RUN git clone --depth 1 --branch "v${WHISPER_CPP_VERSION}" https://github.com/ggml-org/whisper.cpp.git . && \
    cmake -S . -B build -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF -DWHISPER_BUILD_TESTS=OFF -DWHISPER_BUILD_SERVER=OFF && \
    cmake --build build --config Release --target whisper-cli -j2 && \
    ./models/download-ggml-model.sh base && \
    echo "${WHISPER_MODEL_SHA1}  models/ggml-base.bin" | sha1sum -c -

FROM ${ALPINE_IMAGE}
ARG YTDLP_VERSION
ARG WHISPER_CPP_VERSION
RUN apk add --no-cache ca-certificates ffmpeg libgomp libstdc++ python3 py3-pip tzdata && \
    pip3 install --break-system-packages --no-cache-dir "yt-dlp[default,curl-cffi]==${YTDLP_VERSION}" && \
    yt-dlp --list-impersonate-targets | grep -q curl_cffi && \
    addgroup -S collector && \
    adduser -S -D -H -G collector -s /sbin/nologin collector && \
    mkdir -p /app/web /app/cache/tasks /app/cache/tmp && \
    chown -R collector:collector /app

COPY --from=server-builder --chown=collector:collector /out/video-collector /app/video-collector
COPY --from=web-builder --chown=collector:collector /src/dist-web/ /app/web/
COPY --from=whisper-builder --chown=collector:collector /src/build/bin/whisper-cli /usr/local/bin/whisper-cli
COPY --from=whisper-builder --chown=collector:collector /src/models/ggml-base.bin /app/models/ggml-base.bin

USER collector
EXPOSE 8787
ENV VIDEO_COLLECTOR_LISTEN=0.0.0.0:8787 \
    VIDEO_COLLECTOR_WEB_ROOT=/app/web \
    VIDEO_COLLECTOR_TEMP_ROOT=/app/cache/tasks \
    TMPDIR=/app/cache/tmp \
    YTDLP_PATH=/usr/bin/yt-dlp \
    FFMPEG_PATH=/usr/bin/ffmpeg \
    WHISPER_PATH=/usr/local/bin/whisper-cli \
    WHISPER_MODEL_PATH=/app/models/ggml-base.bin \
    WHISPER_VERSION="whisper.cpp ${WHISPER_CPP_VERSION}"

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD wget -q -T 3 -O /dev/null http://localhost:8787/health || exit 1

ENTRYPOINT ["/app/video-collector"]
