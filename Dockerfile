# ============================================================
# Multi-stage Dockerfile: one image with everything (dotdl:latest)
# ============================================================
#
# Base images are written out in the FROM lines (no ARG) so Dependabot can
# update them.

# ============================================================
# Base stage: Go dependencies (cached layer)
# ============================================================
FROM golang:1.27-alpine AS base

# Lets Go download the toolchain version go.mod asks for.
ENV GOTOOLCHAIN=auto

WORKDIR /src

# Copy the dependency files first to use Docker's layer cache (modules are
# only downloaded again when they change).
COPY go.mod go.sum ./
RUN go mod download

# ============================================================
# Builder stage: static binary
# ============================================================
FROM base AS builder

# Version embedded in the binary (dotdl version); the Makefile passes git describe.
ARG VERSION=dev
ARG COMMIT=
ARG DATE=

# CGO_ENABLED=0 builds a binary with no C dependencies, which runs on any
# Linux image.
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath \
        -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
        -o /dotdl ./cmd/dotdl

# ============================================================
# Tools stage: verified yt-dlp
# ============================================================
FROM debian:trixie-slim AS tools

ARG TARGETARCH

# yt-dlp breaks whenever YouTube changes: it is the dependency that ages
# fastest in this image. It is pinned (reproducible builds) and verified
# against the release's own SHA2-256SUMS. `make update-ytdlp` rewrites this
# line with the latest version and rebuilds the image.
ARG YTDLP_VERSION=2026.10.26

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates curl && \
    rm -rf /var/lib/apt/lists/*

# The standalone binary is self-contained (no Python needed) and ships
# yt-dlp-ejs, the scripts that solve YouTube's JavaScript challenges.
RUN set -eux; \
    # TARGETARCH is only set by BuildKit; without it, use the image's own.
    arch="${TARGETARCH:-$(dpkg --print-architecture)}"; \
    case "${arch}" in \
        amd64) asset=yt-dlp_linux ;; \
        arm64) asset=yt-dlp_linux_aarch64 ;; \
        *) echo "unsupported architecture: ${arch}" >&2; exit 1 ;; \
    esac; \
    release="https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}"; \
    cd /tmp; \
    curl -fsSLO "${release}/${asset}"; \
    curl -fsSLO "${release}/SHA2-256SUMS"; \
    grep " ${asset}\$" SHA2-256SUMS | sha256sum -c -; \
    install -m 0755 "${asset}" /usr/local/bin/yt-dlp; \
    /usr/local/bin/yt-dlp --version

# ============================================================
# Deno stage: yt-dlp's JavaScript runtime
# ============================================================
# Since 2025.11 yt-dlp needs a JS runtime to solve YouTube's challenges;
# without one there are fewer formats or errors. Deno is what yt-dlp uses
# by default, with no flag.
FROM denoland/deno:bin-2.9.8 AS deno

# ============================================================
# App stage: the final image
# ============================================================
# Only what downloads need: ffmpeg (merging and the MP4 guarantee), yt-dlp,
# deno and the binary.
FROM debian:trixie-slim AS app

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates ffmpeg && \
    rm -rf /var/lib/apt/lists/*

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
