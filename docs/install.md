# **install**

## **macos and linux**

```bash
curl -fsSL https://raw.githubusercontent.com/abneribeiro/dotdl/main/scripts/install.sh | bash
```

installs the latest release into `~/.local/bin` (checksum verified), offers to
install yt-dlp, ffmpeg and deno, and runs `dotdl doctor`. run it again to
update. options, set before `bash`: `DOTDL_VERSION=v0.1.0`,
`BINDIR=/usr/local/bin`, `DOTDL_DEPS=1` (dependencies without asking).

if `dotdl` is not found, add `~/.local/bin` to your `PATH`:

```bash
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc   # or ~/.bashrc
```

## **go**

```bash
go install github.com/abneribeiro/dotdl/cmd/dotdl@latest
brew install yt-dlp ffmpeg deno   # or your package manager
```

## **windows**

download the `.zip` for your architecture from
[releases](https://github.com/abneribeiro/dotdl/releases/latest), put
`dotdl.exe` on your `PATH`, and install the tools:

```powershell
winget install yt-dlp.yt-dlp Gyan.FFmpeg DenoLand.Deno
```

## **docker (nothing else on your machine)**

the image brings yt-dlp, ffmpeg and deno; `dotdl` becomes a small script
that runs it as your user, with your home mounted at the same path.

```bash
# macOS: OrbStack, the lightest Docker engine (free for personal use)
brew install --cask orbstack && open -a OrbStack
# Linux and WSL: Docker Engine (https://docs.docker.com/engine/install/)

git clone https://github.com/abneribeiro/dotdl && cd dotdl
make docker-build && make install-docker
```

for an open-source engine on macos, use colima instead
(`brew install colima docker docker-buildx && colima start`); it only shares
your home, so save downloads inside it (e.g. `-o ~/Videos`).

## **from source**

```bash
git clone https://github.com/abneribeiro/dotdl && cd dotdl
make setup   # native (Go 1.26+) or Docker, then the output folder
```

**uninstall**: `rm ~/.local/bin/dotdl`, and optionally `~/.config/dotdl`
(and `docker rmi dotdl:latest` for docker).
