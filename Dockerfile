# One image with everything a download needs (dotdl:latest, target app).
#
# Base images are written out in the FROM lines (no ARG) so Dependabot can
# update them.

# Go modules, in a layer that is only rebuilt when they change.
FROM golang:1.27-alpine AS base
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

# The static binary.
FROM base AS builder

# Version embedded in the binary (dotdl version); the Makefile passes git describe.
ARG VERSION=dev
ARG COMMIT=
ARG DATE=

COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath \
        -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
        -o /dotdl ./cmd/dotdl

# yt-dlp, the dependency that ages fastest: it breaks whenever YouTube
# changes. It is pinned and verified against the release's own SHA2-256SUMS;
# `make update-ytdlp` rewrites the version and rebuilds the image.
FROM debian:trixie-slim AS tools

ARG TARGETARCH
ARG YTDLP_VERSION=2026.10.26

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates curl && \
    rm -rf /var/lib/apt/lists/*

# The standalone binary needs no Python and ships yt-dlp-ejs, the scripts
# that solve YouTube's JavaScript challenges.
RUN set -eux; \
    case "${TARGETARCH}" in \
        amd64) asset=yt-dlp_linux ;; \
        arm64) asset=yt-dlp_linux_aarch64 ;; \
        *) echo "unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    release="https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}"; \
    cd /tmp; \
    curl -fsSLO "${release}/${asset}"; \
    curl -fsSLO "${release}/SHA2-256SUMS"; \
    grep " ${asset}\$" SHA2-256SUMS | sha256sum -c -; \
    install -m 0755 "${asset}" /usr/local/bin/yt-dlp; \
    /usr/local/bin/yt-dlp --version

# Deno, the JavaScript runtime yt-dlp uses by default for YouTube.
FROM denoland/deno:bin-2.9.8 AS deno

# Static ffmpeg and ffprobe (with libx264): two binaries instead of the
# hundreds of libraries Debian's ffmpeg package brings, most of the old image.
FROM mwader/static-ffmpeg:9.0.2-1 AS ffmpeg

# The final image. Debian because yt-dlp and deno need glibc.
FROM debian:trixie-slim AS app

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/*

COPY --from=ffmpeg /ffmpeg /ffprobe /usr/local/bin/
COPY --from=tools /usr/local/bin/yt-dlp /usr/local/bin/yt-dlp
COPY --from=deno /deno /usr/local/bin/deno
COPY --from=builder /dotdl /dotdl

# The wrapper runs the container as the calling user; this user only applies
# when the image runs without --user.
RUN useradd --system --uid 10001 --create-home --home-dir /home/dotdl dotdl
# DOTDL_DOCKER: folder errors explain what the wrapper mounts.
ENV DOTDL_DOCKER=1

USER dotdl
WORKDIR /home/dotdl

# e.g. docker run --rm -t dotdl:latest video https://youtu.be/xxxx
ENTRYPOINT ["/dotdl"]
