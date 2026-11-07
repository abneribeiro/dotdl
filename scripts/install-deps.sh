#!/usr/bin/env bash
# Installs the native dotdl's dependencies:
#   - yt-dlp and deno into BINDIR (default ~/.local/bin), without sudo,
#     verified against the checksum each release publishes;
#   - ffmpeg/ffprobe with the system's package manager (asks before using
#     sudo).
#
#   scripts/install-deps.sh [--check] [--yes]
#
# Versions: YTDLP_VERSION (default latest) and DENO_VERSION can be pinned
# through the environment.
set -euo pipefail

YTDLP_VERSION=${YTDLP_VERSION:-latest}
DENO_VERSION=${DENO_VERSION:-2.9.8}
BINDIR=${BINDIR:-$HOME/.local/bin}

check_only=0
assume_yes=${YES:-0}
for arg in "$@"; do
	case "$arg" in
	--check) check_only=1 ;;
	--yes | -y) assume_yes=1 ;;
	-h | --help)
		sed -n '2,11p' "$0" | sed 's/^# \{0,1\}//'
		exit 0
		;;
	*)
		echo "unknown option: $arg" >&2
		exit 2
		;;
	esac
done

have() { command -v "$1" >/dev/null 2>&1; }
ok() { printf '  \033[38;2;86;213;142m✓\033[0m %s\n' "$*"; }
warn() { printf '  \033[38;2;242;196;109m!\033[0m %s\n' "$*"; }
fail() { printf '  \033[38;2;255;135;113m✗\033[0m %s\n' "$*"; }
step() { printf '\n\033[1;38;2;255;135;113m==>\033[0m \033[1m%s\033[0m\n' "$*"; }

confirm() {
	local answer
	if [ "$assume_yes" = 1 ]; then
		return 0
	fi
	if [ ! -t 0 ]; then
		return 1
	fi
	read -r -p "$1 [Y/n] " answer
	case "${answer:-y}" in [yY]*) return 0 ;; *) return 1 ;; esac
}

sha256() {
	if have sha256sum; then
		sha256sum "$1" | awk '{print $1}'
	else
		shasum -a 256 "$1" | awk '{print $1}'
	fi
}

# verify FILE EXPECTED_HASH aborts when the download does not match the
# published checksum (corrupted or tampered download).
verify() {
	local actual
	actual=$(sha256 "$1")
	if [ -z "$2" ] || [ "$actual" != "$2" ]; then
		fail "bad checksum for $(basename "$1") (expected ${2:-?}, got $actual)"
		exit 1
	fi
}

download() { curl -fsSL --retry 3 -o "$2" "$1"; }

# unzip_to ZIP DIR uses unzip when available, otherwise Python, which almost
# every system has (minimal Ubuntu has no unzip).
unzip_to() {
	if have unzip; then
		unzip -q -o "$1" -d "$2"
	else
		python3 -I -c 'import sys, zipfile; zipfile.ZipFile(sys.argv[1]).extractall(sys.argv[2])' "$1" "$2"
	fi
}

os=$(uname -s)
arch=$(uname -m)
case "$os/$arch" in
Linux/x86_64)
	ytdlp_asset=yt-dlp_linux
	triple=x86_64-unknown-linux-gnu
	;;
Linux/aarch64 | Linux/arm64)
	ytdlp_asset=yt-dlp_linux_aarch64
	triple=aarch64-unknown-linux-gnu
	;;
Darwin/arm64)
	ytdlp_asset=yt-dlp_macos
	triple=aarch64-apple-darwin
	;;
Darwin/x86_64)
	ytdlp_asset=yt-dlp_macos
	triple=x86_64-apple-darwin
	;;
*)
	echo "Unsupported system: $os/$arch. Install yt-dlp, deno and ffmpeg by hand." >&2
	exit 1
	;;
esac

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

report() {
	local missing=0
	# What was just installed into BINDIR counts, even if BINDIR is not on
	# this shell's PATH yet (path_hint already warned).
	PATH="$BINDIR:$PATH"
	step "Dependencies"
	if have yt-dlp; then ok "yt-dlp $(yt-dlp --version 2>/dev/null | head -n 1)"; else fail "yt-dlp"; missing=1; fi
	if have ffmpeg && have ffprobe; then
		ok "$(ffmpeg -version 2>/dev/null | head -n 1 | cut -d' ' -f1-3)"
	else
		fail "ffmpeg/ffprobe"
		missing=1
	fi
	if have deno; then
		ok "$(deno --version 2>/dev/null | head -n 1)"
	elif have node; then
		ok "node $(node --version) (yt-dlp recommends deno)"
	else
		warn "no JavaScript runtime (deno or node): YouTube will offer fewer formats"
	fi
	return "$missing"
}

path_hint() {
	case ":$PATH:" in
	*":$BINDIR:"*) ;;
	*)
		warn "$BINDIR is not on your PATH. Add this to ~/.zshrc or ~/.bashrc:"
		echo "       export PATH=\"$BINDIR:\$PATH\""
		;;
	esac
}

install_ytdlp() {
	# A yt-dlp from apt is usually months behind, and YouTube breaks old
	# versions. The official binary updates itself.
	local base
	if [ "$YTDLP_VERSION" = latest ]; then
		base="https://github.com/yt-dlp/yt-dlp/releases/latest/download"
	else
		base="https://github.com/yt-dlp/yt-dlp/releases/download/$YTDLP_VERSION"
	fi
	download "$base/$ytdlp_asset" "$tmp/$ytdlp_asset"
	download "$base/SHA2-256SUMS" "$tmp/SHA2-256SUMS"
	verify "$tmp/$ytdlp_asset" "$(awk -v f="$ytdlp_asset" '$2 == f {print $1}' "$tmp/SHA2-256SUMS")"
	install -m 0755 "$tmp/$ytdlp_asset" "$BINDIR/yt-dlp"
	ok "yt-dlp $("$BINDIR/yt-dlp" --version) in $BINDIR"
}

install_deno() {
	local zip=deno-$triple.zip
	local base="https://github.com/denoland/deno/releases/download/v$DENO_VERSION"
	download "$base/$zip" "$tmp/$zip"
	download "$base/$zip.sha256sum" "$tmp/$zip.sha256sum"
	verify "$tmp/$zip" "$(awk '{print $1; exit}' "$tmp/$zip.sha256sum")"
	mkdir -p "$tmp/deno"
	unzip_to "$tmp/$zip" "$tmp/deno"
	install -m 0755 "$tmp/deno/deno" "$BINDIR/deno"
	ok "$("$BINDIR/deno" --version | head -n 1) in $BINDIR"
}

install_ffmpeg() {
	local cmd
	if have apt-get; then
		cmd="sudo apt-get update && sudo apt-get install -y ffmpeg"
	elif have pacman; then
		cmd="sudo pacman -S --needed --noconfirm ffmpeg"
	elif have zypper; then
		cmd="sudo zypper install -y ffmpeg"
	elif have brew; then
		cmd="brew install ffmpeg"
	elif have dnf; then
		# Fedora's ffmpeg-free has no x264, which the MP4 conversions need:
		# RPM Fusion's build is required.
		warn "On Fedora, install ffmpeg from RPM Fusion: https://rpmfusion.org/Howto/Multimedia"
		return 1
	else
		warn "Unknown package manager: install ffmpeg by hand (https://ffmpeg.org/download.html)."
		return 1
	fi
	if ! confirm "Install ffmpeg with: $cmd ?"; then
		warn "ffmpeg not installed. Run: $cmd"
		return 1
	fi
	eval "$cmd"
	ok "$(ffmpeg -version | head -n 1 | cut -d' ' -f1-3)"
}

if [ "$check_only" = 1 ]; then
	report
	exit $?
fi

mkdir -p "$BINDIR"

step "yt-dlp"
install_ytdlp

step "Deno (the JavaScript runtime yt-dlp uses for YouTube)"
if have deno && [ "$(command -v deno)" != "$BINDIR/deno" ]; then
	ok "already installed: $(deno --version | head -n 1)"
else
	install_deno
fi

step "ffmpeg"
ffmpeg_ok=1
if have ffmpeg && have ffprobe; then
	ok "already installed: $(ffmpeg -version | head -n 1 | cut -d' ' -f1-3)"
else
	install_ffmpeg || ffmpeg_ok=0
fi

path_hint
report || true
if [ "$ffmpeg_ok" = 0 ]; then
	exit 1
fi
