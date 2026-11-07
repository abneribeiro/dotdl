# dotdl: install, test and run.
#
#   make              list the targets
#   make setup        set everything up after cloning (guided)
#
# Useful variables: PREFIX/BINDIR (where to install), IMAGE, VERSION
# (defaults to git describe).

SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help
MAKEFLAGS += --no-print-directory

BINARY          := dotdl
PKG             := ./cmd/dotdl
BIN_DIR         := bin
PREFIX          ?= $(HOME)/.local
BINDIR          ?= $(PREFIX)/bin
DOCKER_CLI_NAME ?= dotdl

IMAGE ?= dotdl:latest

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

GORELEASER_IMAGE ?= goreleaser/goreleaser:v2.19.0

# DOCKER_BUILD_FLAGS adds docker build options, e.g. --network host on a
# machine where DNS fails inside the build.
DOCKER_BUILD_FLAGS ?=
DOCKER_BUILD := docker build $(DOCKER_BUILD_FLAGS) \
	--build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) --build-arg DATE=$(DATE)

# Warns when the install folder is not on PATH.
define path_hint
case ":$$PATH:" in *":$(BINDIR):"*) ;; \
  *) echo "! $(BINDIR) is not on your PATH. Add this to ~/.zshrc or ~/.bashrc:"; \
     echo "    export PATH=\"$(BINDIR):\$$PATH\"" ;; esac
endef

.PHONY: help setup run deps build install install-docker uninstall \
	fmt vet lint test test-race cover test-integration test-integration-docker check \
	docker-build update-ytdlp release-snapshot release clean

##@ Getting started

help: ## Show this help
	@awk 'BEGIN {FS = ":.*## "; printf "Usage: make \033[38;2;255;135;113m<target>\033[0m [VAR=value]\n"} \
		/^##@/ {printf "\n\033[1m%s\033[0m\n", substr($$0, 5)} \
		/^[a-zA-Z0-9_-]+:.*## / {printf "  \033[38;2;255;135;113m%-24s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

setup: ## Guided setup: install dotdl (Docker or native) and pick the output folder
	@BINDIR="$(BINDIR)" IMAGE="$(IMAGE)" scripts/setup.sh

run: ## Run dotdl from source (ARGS="video <url>")
	go run $(PKG) $(ARGS)

##@ Native install

deps: ## Install yt-dlp and deno into BINDIR, and ffmpeg with the package manager
	@BINDIR="$(BINDIR)" scripts/install-deps.sh

build: ## Build bin/dotdl with the version embedded
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY) $(PKG)

install: build ## Install the native binary into BINDIR (default ~/.local/bin)
	@mkdir -p "$(BINDIR)"
	@if [ -f "$(BINDIR)/$(BINARY)" ] && [ "$$(head -c 2 "$(BINDIR)/$(BINARY)")" = "#!" ]; then \
		echo "Replacing the Docker wrapper at $(BINDIR)/$(BINARY) with the native binary."; fi
	install -m 0755 $(BIN_DIR)/$(BINARY) "$(BINDIR)/$(BINARY)"
	@"$(BINDIR)/$(BINARY)" version
	@$(path_hint)

install-docker: ## Install the wrapper that runs dotdl in Docker (DOCKER_CLI_NAME=dotdl)
	@mkdir -p "$(BINDIR)"
	install -m 0755 scripts/dotdl-docker "$(BINDIR)/$(DOCKER_CLI_NAME)"
	@echo "Installed $(BINDIR)/$(DOCKER_CLI_NAME), which runs the $(IMAGE) image (make docker-build)."
	@$(path_hint)

uninstall: ## Remove dotdl from BINDIR
	rm -f "$(BINDIR)/$(BINARY)"

##@ Quality

fmt: ## Format the code (gofmt)
	gofmt -w cmd internal

vet: ## go vet
	go vet ./...

lint: ## Fail on unformatted code or vet warnings
	@unformatted="$$(gofmt -l cmd internal)"; \
	if [ -n "$$unformatted" ]; then echo "Unformatted (make fmt):"; echo "$$unformatted"; exit 1; fi
	go vet ./...
	go vet -tags integration ./...

test: ## Unit tests (fast, no network)
	go test ./...

test-race: ## Tests with the race detector
	go test -race ./...

cover: ## Coverage (coverage.out and the total)
	go test -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | tail -1

test-integration: ## Integration tests on the host (needs yt-dlp, ffmpeg and the network)
	go test -tags integration -v -timeout 30m ./internal/downloader

test-integration-docker: docker-build ## Integration tests inside the image (real downloads)
	CGO_ENABLED=0 go test -c -tags integration -o $(BIN_DIR)/downloader.test ./internal/downloader
	docker run --rm --entrypoint /tests/downloader.test \
		-v "$(CURDIR)/$(BIN_DIR):/tests:ro" $(IMAGE) \
		-test.v -test.timeout 20m $(if $(RUN),-test.run '$(RUN)')

check: lint test-race ## lint + tests with -race (what CI runs)

##@ Docker

docker-build: ## Build the image (IMAGE=dotdl:latest)
	$(DOCKER_BUILD) --target app -t $(IMAGE) .

update-ytdlp: ## Pin the latest yt-dlp in the Dockerfile and rebuild the image
	@tag="$$(curl -fsSI https://github.com/yt-dlp/yt-dlp/releases/latest | tr -d '\r' \
		| grep -i '^location:' | sed 's#.*/tag/##')"; \
	if [ -z "$$tag" ]; then echo "cannot find the latest yt-dlp release" >&2; exit 1; fi; \
	sed -i.bak "s/^ARG YTDLP_VERSION=.*/ARG YTDLP_VERSION=$$tag/" Dockerfile && rm -f Dockerfile.bak; \
	echo "yt-dlp pinned to $$tag in the Dockerfile"
	$(MAKE) docker-build

##@ Releases

release-snapshot: ## Binaries for every platform in dist/, without publishing
	docker run --rm --user "$$(id -u):$$(id -g)" \
		-e HOME=/tmp -e GOCACHE=/tmp/go-build -e GOMODCACHE=/gomod \
		-v "$$(go env GOMODCACHE):/gomod" -v "$(CURDIR):/src" -w /src \
		$(GORELEASER_IMAGE) release --snapshot --clean

release: ## Create and push the tag VERSION=vX.Y.Z (GitHub Actions publishes it)
	@if ! [[ "$(VERSION)" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$$ ]]; then \
		echo "usage: make release VERSION=v0.1.0" >&2; exit 1; fi
	@if [ -n "$$(git status --porcelain)" ]; then echo "there are uncommitted changes" >&2; exit 1; fi
	git tag -a $(VERSION) -m "dotdl $(VERSION)"
	git push origin $(VERSION)

clean: ## Remove bin/, dist/ and the coverage file
	rm -rf $(BIN_DIR) dist coverage.out
