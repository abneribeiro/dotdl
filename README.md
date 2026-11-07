<div align="center">
  <br />
  <h3><b>dotdl</b></h3>
  <p>download videos and music in a format every phone and computer plays, on top of yt-dlp and ffmpeg.</p>
</div>

<br />

```
$ dotdl video -q 1080 https://youtu.be/xxxx

 dotdl  video · up to 1080p
⣾  Downloading  Big Buck Bunny [aqz-KE-bpKQ].mp4
████████████████████████████░░░░░░░░░░░░░░░░░░░░   61%  3.1 MB/s  0:12 left

✓ Big Buck Bunny [aqz-KE-bpKQ].mp4  H.264 · 1920×1080
Saved to ~/Videos
```

## **overview**

-   downloads from youtube, instagram, x, tiktok, soundcloud and [every site yt-dlp supports](https://github.com/yt-dlp/yt-dlp/blob/master/supportedsites.md)
-   video is always mp4 with h.264 + aac, converted only when a site has nothing but vp9/av1, so it plays on iphone, android, windows, macos and whatsapp
-   audio as mp3 or m4a with title, artist and cover art
-   playlists, batch files and trims; resumable, so the same command again skips what is done and resumes what was interrupted
-   stack: `go 1.26+`, `bubble tea v2`, `lip gloss v2`, `yt-dlp`, `ffmpeg`, `deno`, `docker`

## **design & trade-offs**

-   **strict layers:** `cli` parses and decides exit codes, `ui` draws, `jobs` orchestrates, `downloader` runs the tools. `jobs` and `downloader` never write to the terminal; progress reaches the ui as `jobs.Event` values.
-   **resolution before codec:** the format sort is `res,hdr:sdr,vcodec:h264,acodec:aac`. resolution has to outrank the codec, or a 2160 cap is ignored, and sdr is preferred because hdr squeezed into 8-bit h.264 looks washed out.
-   **quality means the short side:** `-q 720` is a cap with fallback (`-S res:720`), never a height filter, which turned a vertical 720x1280 short into 360x640.
-   **interruptions are safe:** ctrl+c sends `os.Interrupt` to yt-dlp/ffmpeg, so `.part` files survive and a rerun resumes. pending conversions are written to `.dotdl-pending.txt`, so an interrupted conversion never leaves a stray `.webm`.
-   **why 1080p is the sweet spot:** up to 1080p youtube serves h.264, so the mp4 needs no conversion. above it only vp9/av1 exist, and converting them takes a while.

## **quickstart**

macos and linux (latest release, checksum verified, offers to install yt-dlp, ffmpeg and deno):

```bash
curl -fsSL https://raw.githubusercontent.com/abneribeiro/dotdl/main/scripts/install.sh | bash
dotdl doctor
dotdl video "https://youtube.com/watch?v=xxxx"
dotdl audio -f m4a "https://youtube.com/watch?v=xxxx"
```

or with go:

```bash
go install github.com/abneribeiro/dotdl/cmd/dotdl@latest
```

windows, docker and building from source are in [install](docs/install.md).

## **testing**

```bash
make check                    # gofmt, vet and tests with -race, as ci
make test-integration-docker  # real downloads inside the docker image
make run ARGS="video <url>"   # run from source
```

`make` lists every target. releases are tagged with `make release VERSION=vX.Y.Z` and published by github actions.

## **docs**

-   [install](docs/install.md): every platform, docker, from source, uninstall
-   [usage](docs/usage.md): commands, flags and examples
-   [settings](docs/settings.md): `dotdl config` and environment variables
-   [troubleshooting](docs/troubleshooting.md): bot checks, broken sites, `PATH`
-   [AGENTS.md](AGENTS.md): architecture, invariants and code conventions

## **license**

-   [mit](LICENSE)
