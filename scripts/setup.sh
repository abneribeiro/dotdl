#!/usr/bin/env bash
# Guided setup after cloning (make setup): leaves dotdl ready to use.
#
#   make setup                         asks at each step
#   make setup YES=1 INSTALL=docker    no questions (INSTALL=docker|native)
set -euo pipefail

cd "$(dirname "$0")/.."

BINDIR=${BINDIR:-$HOME/.local/bin}
YES=${YES:-0}
interactive=1
if [ "$YES" = 1 ] || [ ! -t 0 ]; then
	interactive=0
fi

have() { command -v "$1" >/dev/null 2>&1; }
step() { printf '\n\033[1;38;2;255;135;113m==>\033[0m \033[1m%s\033[0m\n' "$*"; }

step "1/4 Requirements"
docker_ok=0
if have docker && docker info >/dev/null 2>&1; then
	docker_ok=1
	echo "  ✓ $(docker --version)"
else
	echo "  - Docker not found (or no permission): only needed for the Docker install."
fi
go_ok=0
if have go; then
	go_ok=1
	echo "  ✓ $(go version)"
else
	echo "  - Go not found: only needed to build the native install (https://go.dev/dl/)."
fi

step "2/4 Install"
mode=${INSTALL:-}
if [ -z "$mode" ]; then
	# Docker by default: it brings everything without installing anything.
	default=native
	if [ "$docker_ok" = 1 ]; then
		default=docker
	fi
	mode=$default
	if [ "$interactive" = 1 ]; then
		echo "  1) docker  everything in one image; nothing installed on the system"
		echo "  2) native  binary in $BINDIR plus yt-dlp, deno and ffmpeg on the system (needs Go)"
		read -r -p "How do you want to install? [$default] " answer
		case "${answer:-$default}" in
		1 | docker) mode=docker ;;
		2 | native) mode=native ;;
		esac
	fi
fi

case "$mode" in
docker)
	if [ "$docker_ok" = 0 ]; then
		if [ "$(uname -s)" = Darwin ]; then
			echo "  ✗ The Docker install needs Docker: brew install --cask orbstack && open -a OrbStack" >&2
		else
			echo "  ✗ The Docker install needs Docker (https://docs.docker.com/engine/install/)." >&2
		fi
		exit 1
	fi
	make docker-build
	make install-docker
	;;
native)
	if [ "$go_ok" = 0 ]; then
		echo "  ✗ The native install needs Go. Install it or use INSTALL=docker." >&2
		exit 1
	fi
	make deps
	make install
	;;
*)
	echo "  ✗ Unknown install mode: $mode (use docker or native)" >&2
	exit 2
	;;
esac

step "3/4 Output folder"
if [ "$interactive" = 1 ]; then
	read -r -p "Where should downloads go? (empty = the current folder each time) " output
	if [ -n "$output" ]; then
		"$BINDIR/dotdl" config output "$output"
	fi
else
	echo "  Skipped. Set it later with: dotdl config output <folder>"
fi

step "4/4 Doctor"
# doctor exits with 1 when something is missing: the report says what.
"$BINDIR/dotdl" doctor || true

echo
echo "Done. Try: dotdl video https://youtu.be/xxxx"
echo "(if the command is not found, add $BINDIR to your PATH)"
