# AGENTS.md — dotdl

## Project Overview

dotdl is a command-line tool, in Go, that downloads videos and music with
`yt-dlp` in a format every player opens (iPhone, Android, Windows, macOS,
WhatsApp). Commands:

- `video [flags] <url>...`: MP4/H.264/AAC (or `--original`), quality cap,
  playlists, batch files, trims, resumable.
- `audio [flags] <url>...`: MP3 or M4A with title, artist and cover art.
- `doctor`, `config [key] [value]`, `version`, `help [command]`.
- `dotdl <url>` is a shortcut for `dotdl video <url>`.

Downloads show a Bubble Tea progress view (spinner, coral bar, speed, ETA,
latest warning) on a terminal, and plain lines otherwise. Everything else
(help, results, errors, doctor, config) is printed with Lip Gloss in a coral
theme.

**Stack**: Go 1.26+ (built with 1.27), Bubble Tea v2, Bubbles v2, Lip Gloss v2
(`charm.land/...`), yt-dlp, FFmpeg, Deno, Docker.

## Build Commands

`make` with no target lists everything. The common ones:

```bash
make setup             # Guided setup: install (docker|native), output folder, doctor
make run ARGS="..."    # Run from source
make build             # bin/dotdl with version ldflags
make install           # Native binary into ~/.local/bin (PREFIX=… to change)
make install-docker    # Docker wrapper (scripts/dotdl-docker) as `dotdl`
make deps              # yt-dlp + deno (checksum-verified) and ffmpeg on the host
make docker-build      # The image dotdl:latest (~1GB, mostly ffmpeg)
make update-ytdlp      # Pin the latest yt-dlp in the Dockerfile and rebuild
make release-snapshot  # GoReleaser in Docker, all platforms into dist/
```

The module path is `github.com/abneribeiro/dotdl`.

## Testing

```bash
make check                     # gofmt + vet (incl. integration tag) + go test -race
make test-integration-docker   # real yt-dlp/ffmpeg downloads inside dotdl:latest
```

- Tests are table-driven and use only the standard `testing` package.
- Pure logic is unit-tested: argument building, progress parsing (yt-dlp
  template, ffmpeg `out_time_us`), `compatArgs`, `moovFirstIn`, error
  classification, validation, config and `config.Save`, the doctor with a
  fake system.
- `internal/jobs` and `internal/cli` run whole downloads against **fake
  binaries**: small `sh` scripts on a test `PATH` that imitate yt-dlp,
  ffprobe and ffmpeg. Use this pattern for any new orchestration test: no
  network, no real tools.
- `internal/ui` drives the Bubble Tea model directly (events, Ctrl+C, done)
  and checks the rendered view, including that no line is wider than the
  terminal at 40/60/80/120 columns. Printers are tested by writing to a
  buffer: Lip Gloss drops the colors for non-terminals.
- Integration tests (`//go:build integration`) execute the real tools and the
  network; `make test-integration-docker` compiles them with `go test -c` and
  runs them inside the image.

## Linting & Formatting

`make lint` = gofmt check + `go vet` (with and without the integration tag).
Shell scripts are checked with shellcheck in CI. All code must be
`gofmt`-compliant. Keep the code free of dead code: nothing unused, no
leftover features.

## Code Style Guidelines

### Architecture

```
cmd/dotdl/main.go      Entry point: config.LoadFiles + Load, slog, signals
                       (signal.NotifyContext lives ONLY here), version
                       string, cli.App.Run → exit code.
internal/cli/          Commands: flag parsing (short and long names,
                       parseInterspersed), help definitions, dispatch to the
                       engine, exit codes. Picks the JS runtime per download.
internal/ui/           The look. theme.go (coral/green palette and styles),
                       progress.go (Bubble Tea model, RunWithProgress,
                       RunPlain), print.go (help, result summary, errors,
                       doctor, config), format.go (bytes, durations).
internal/jobs/         THE ENGINE: NewDownload validates a request;
                       Download.Run executes it and reports jobs.Event
                       through a Reporter. Result lists the files and the
                       ones converted (with the source codec).
internal/downloader/   yt-dlp + ffmpeg. BuildArgs, QualitySort, MP4Sort,
                       ParseLine, FFmpegFraction, mp4Compatible, compatArgs,
                       moovFirstIn are pure; Run executes yt-dlp with
                       progress callbacks; EnsureMP4 enforces the format;
                       Describe summarizes a file for the user.
internal/doctor/       Environment checks (Collect, HasFailures).
internal/config/       Env-var configuration, strict validation, the global
                       ~/.config/dotdl/config.env and Save.
internal/procio/       LineWriter (splits on \n AND \r) and TailBuffer.
scripts/               setup.sh, install-deps.sh, dotdl-docker (wrapper),
                       install.sh (releases).
```

Key invariants to preserve:

- **Layers**: `cli` parses and decides exit codes, `ui` draws, `jobs`
  orchestrates, `downloader` runs the tools. `jobs` and `downloader` never
  write to the terminal and never import `cli` or `ui`.
- **Events, not output**: progress reaches the UI as `jobs.Event` (Stage,
  Fraction/-1, Speed, ETA, Item, Index/Count, Log). An Event's empty fields
  mean "unchanged". Reporters may be called from several goroutines; the
  `reporterSink` serializes them. The UI shows only `WARNING:` lines and
  dotdl's own notes from the log.
- **Ctrl+C**: the progress program handles it as a key and cancels the job's
  context (Bubble Tea runs `WithoutSignalHandler`; signals belong to main).
  `cmd.Cancel` sends `os.Interrupt` to yt-dlp/ffmpeg with a `WaitDelay`, so
  `.part` files survive and a rerun resumes. Canceled jobs return
  `context.Canceled` (exit 130); partial success returns an error wrapping
  `jobs.ErrPartial` plus the Result (exit 1, files still listed).
- **Output is always MP4 / H.264 8-bit / AAC, with faststart** (video mode):
  `MP4Sort` asks yt-dlp for `res,hdr:sdr,vcodec:h264,acodec:aac` (resolution
  must outrank vcodec, or a 2160 cap would be ignored; yt-dlp prefers HDR by
  default, and HDR squeezed into 8-bit H.264 looks washed out). `EnsureMP4`
  probes and fixes what landed: every re-encode carries `-pix_fmt yuv420p`
  (no High10), `-profile:v high` and an even-dimensions scale (libx264 rejects
  odd sizes in 4:2:0); a compatible MP4 whose `moov` box sits after `mdat`
  gets a lossless `+faststart` remux (trims are re-encoded by yt-dlp itself).
- **Resolution means the SHORT side**: the quality is only `-S res:N` (a cap
  with fallback), never an `-f [height<=N]` filter, which turned a vertical
  720x1280 Short into 360x640.
- **Pending conversions persist**: in MP4 mode the `--print-to-file` target
  is `<outdir>/.dotdl-pending.txt`. yt-dlp records the archive BEFORE
  `EnsureMP4` runs, so an interrupted conversion would otherwise leave a
  `.webm` forever. Deleted only when everything converted.
- **Trim + archive don't mix**: a trim never writes the archive, forces
  `--no-playlist` and has the interval in the file name (`outputTemplate`).
  Audio uses its own archive (`.dotdl-archive-audio.txt`).
- **`--print-to-file` implies `--simulate`**: `BuildArgs` always emits
  `--no-simulate` with it.

### Imports

Group imports in blocks separated by blank lines, each sorted alphabetically:
standard library, third-party (including `charm.land/...`), then internal
`github.com/abneribeiro/dotdl/...`.

### Formatting & Naming

gofmt; tabs; trailing commas in multi-line literals; ~120-char lines.
`PascalCase` exported, `camelCase` everything else including constants (never
`SCREAMING_SNAKE_CASE`); acronyms uppercase (`URL`, `ID`); lowercase file
names.

### Error Handling

1. Check errors immediately; wrap with `fmt.Errorf("…: %w", err)`.
2. `internal/downloader` returns `*DownloadError` with a `Kind`
   (`ErrPrivate`, `ErrLoginRequired`, `ErrRateLimited`, `ErrCanceled`, …);
   classify with `errors.As` + switch, never by matching message text.
   `Detail()` gives yt-dlp's last `ERROR:` line, `Hint()` a fix.
3. Never discard errors silently; intentionally ignored ones are assigned to
   `_` with a reason.
4. Commands print their own errors through `ui` and return an exit code:
   0 OK, 1 failure or partial, 2 usage, 130 interrupted.

### Logging

`log/slog` only, level from `DOTDL_LOG_LEVEL`, on stderr, used for debug
detail. User-facing output goes through `ui`.

### Text and comments

- Everything is in **English**: user-facing text, errors, comments, tests,
  docs and scripts.
- `//` line comments only, above the declaration they describe.

## Configuration

Env vars parsed in `internal/config` (see `.env.example`):
`DOTDL_OUTPUT_DIR`, `DOTDL_TEMP_DIR`, `DOTDL_COOKIES_FILE`,
`DOTDL_JS_RUNTIMES`, `DOTDL_LOG_LEVEL`. Priority: environment > `./.env`
> `~/.config/dotdl/config.env` (godotenv never overrides). `dotdl config`
writes the file with `config.Save` (only the given keys, 0600). A leading `~`
is expanded in every path.

`DOTDL_OUTPUT_DIR` is the **base path**: used when `-o` is absent and the
root of a relative `-o`; an absolute `-o` ignores it.

## Environment & Dependencies

- **Runtime deps**: `yt-dlp`, `ffmpeg`, `ffprobe`, plus a JS runtime for
  YouTube: Deno (yt-dlp's default) or node (`--js-runtimes node`, picked
  automatically when it is the only one). Downloads refuse to start without
  the three tools and point at `dotdl doctor`.
- **Go deps**: Charm v2 (Bubble Tea, Bubbles, Lip Gloss), `charmbracelet/x`
  (`ansi`, `term`), godotenv. Flag parsing is stdlib `flag`.

## Docker

One image, `dotdl:latest` (target `app`, ~1GB, mostly ffmpeg's libraries).
Base images are written literally in the `FROM` lines so Dependabot can bump
them.

| Stage     | Purpose |
|-----------|---------|
| `base`    | Go + module download (cache layer) |
| `builder` | Static binary (`CGO_ENABLED=0`, `-trimpath`, version ldflags) |
| `tools`   | yt-dlp standalone binary, pinned (`ARG YTDLP_VERSION`), verified against `SHA2-256SUMS` |
| `deno`    | Deno binary (yt-dlp's JS runtime) |
| `app`     | Debian slim + ffmpeg + yt-dlp + deno + binary; non-root user; `ENTRYPOINT /dotdl` |

`scripts/dotdl-docker` runs the image as the calling user with `$HOME`
mounted at the same path, `-t` when there is a terminal (for the progress
view), and same-path mounts for absolute paths outside the home: `-o`/
`--output` values, the output and cookies paths from the environment,
`./.env` or `config.env`, and every `/mnt/<drive>` on WSL. On macOS the
engine runs in a VM; with the `colima` context, which shares only the home,
the wrapper warns when it mounts a path outside it (OrbStack shares more).
The image sets
`DOTDL_DOCKER=1` so `jobs.PrepareOutputDir` can explain an unreachable
folder.

## Releases & CI

- `.github/workflows/ci.yml`: gofmt, vet (with/without the integration tag),
  `go test -race`, cross-compilation for every release target, shellcheck,
  and an image build on pushes to main.
- `.github/workflows/release.yml`: a `v*` tag runs GoReleaser
  (`.goreleaser.yaml`): linux/darwin/windows × amd64/arm64 archives with
  README, `.env.example` and `scripts/install-deps.sh`, plus `checksums.txt`.
  `scripts/install.sh` depends on the archive name template.
- Commit messages: English conventional style (`type: summary` + past-tense
  bullets), no attribution trailers.
