package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/abneribeiro/dotdl/internal/config"
	"github.com/abneribeiro/dotdl/internal/jobs"
)

// testApp returns an App writing to buffers, in plain (non-interactive)
// mode, with its settings file in a temp folder.
func testApp(t *testing.T) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	return &App{
		Config:  &config.Config{OutputDir: t.TempDir(), TempDir: t.TempDir()},
		Version: "v1.2.3",
		Stdout:  &stdout,
		Stderr:  &stderr,
	}, &stdout, &stderr
}

func TestParseDownload(t *testing.T) {
	url := "https://youtu.be/a"
	tests := []struct {
		name string
		kind kind
		args []string
		want func(*jobs.Download) bool
	}{
		{"short flags", kindVideo, []string{"-q", "720", "-o", "/v", url},
			func(d *jobs.Download) bool { return d.FormatSort == "res:720" && d.OutputDir == "/v" }},
		{"long flags after the URL", kindVideo, []string{url, "--quality", "1080", "--output=/v"},
			func(d *jobs.Download) bool { return d.FormatSort == "res:1080" && d.OutputDir == "/v" }},
		{"video defaults to mp4", kindVideo, []string{url},
			func(d *jobs.Download) bool { return d.Format == jobs.FormatMP4 && !d.Audio() }},
		{"--original keeps the source format", kindVideo, []string{"--original", url},
			func(d *jobs.Download) bool { return d.Format == jobs.FormatOriginal }},
		{"audio defaults to mp3", kindAudio, []string{url},
			func(d *jobs.Download) bool { return d.Format == jobs.FormatMP3 && d.Audio() }},
		{"audio -f m4a", kindAudio, []string{"-f", "m4a", url},
			func(d *jobs.Download) bool { return d.Format == jobs.FormatM4A }},
		{"trim", kindVideo, []string{"--start", "00:10", "--end", "00:20", url},
			func(d *jobs.Download) bool { return d.Section != nil && d.Section.Start == "00:10" }},
		{"-- makes the rest positional", kindVideo, []string{"--", url},
			func(d *jobs.Download) bool { return slices.Equal(d.URLs, []string{url}) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := parseDownload(tt.kind, tt.args, "")
			if err != nil {
				t.Fatalf("parseDownload(%v): %v", tt.args, err)
			}
			if !tt.want(d) {
				t.Errorf("parseDownload(%v) = %+v", tt.args, d)
			}
		})
	}
}

func TestParseDownloadErrors(t *testing.T) {
	url := "https://youtu.be/a"
	tests := []struct {
		name string
		kind kind
		args []string
	}{
		{"no URL", kindVideo, nil},
		{"audio has no quality", kindAudio, []string{"-q", "720", url}},
		{"video has no format flag", kindVideo, []string{"-f", "mp3", url}},
		{"unknown audio format", kindAudio, []string{"-f", "flac", url}},
		{"unknown flag", kindVideo, []string{"--speed", "2", url}},
		{"start without end", kindVideo, []string{"--start", "00:10", url}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseDownload(tt.kind, tt.args, ""); err == nil {
				t.Errorf("parseDownload(%v) should fail", tt.args)
			}
		})
	}
	if _, err := parseDownload(kindVideo, []string{"--help"}, ""); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("--help = %v, want flag.ErrHelp", err)
	}
}

func TestRunCommands(t *testing.T) {
	tests := []struct {
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{nil, ExitOK, "dotdl <command>", ""},
		{[]string{"help"}, ExitOK, "Commands", ""},
		{[]string{"help", "video"}, ExitOK, "--quality", ""},
		{[]string{"video", "--help"}, ExitOK, "--original", ""},
		{[]string{"audio", "-h"}, ExitOK, "--format", ""},
		{[]string{"config", "--help"}, ExitOK, "dotdl config [key] [value]", ""},
		{[]string{"doctor", "-h"}, ExitOK, "dotdl doctor", ""},
		{[]string{"version"}, ExitOK, "v1.2.3", ""},
		{[]string{"nope"}, ExitUsage, "", `unknown command "nope"`},
		{[]string{"help", "nope"}, ExitUsage, "", `unknown command "nope"`},
		{[]string{"video"}, ExitUsage, "", "give at least one URL"},
		{[]string{"video", "youtube.com/x"}, ExitUsage, "", "must start with http"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			app, stdout, stderr := testApp(t)
			if code := app.Run(context.Background(), tt.args); code != tt.wantCode {
				t.Errorf("exit code = %d, want %d (stderr: %s)", code, tt.wantCode, stderr)
			}
			if !strings.Contains(stdout.String(), tt.wantStdout) || !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stdout:\n%s\nstderr:\n%s", stdout, stderr)
			}
		})
	}
}

func TestConfigCommand(t *testing.T) {
	app, stdout, stderr := testApp(t)
	out := filepath.Join(t.TempDir(), "My Videos")

	if code := app.Run(context.Background(), []string{"config", "output", out}); code != ExitOK {
		t.Fatalf("config output: %d %s", code, stderr)
	}
	data, err := os.ReadFile(config.FilePath())
	if err != nil || !strings.Contains(string(data), out) {
		t.Fatalf("config file = %q (%v)", data, err)
	}

	stdout.Reset()
	if code := app.Run(context.Background(), []string{"config"}); code != ExitOK ||
		!strings.Contains(stdout.String(), "output") || !strings.Contains(stdout.String(), config.FilePath()) {
		t.Errorf("config listing (%d):\n%s", code, stdout)
	}

	// A relative folder is saved absolute: it must not depend on where
	// dotdl runs later.
	t.Chdir(t.TempDir())
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if code := app.Run(context.Background(), []string{"config", "output", "Videos"}); code != ExitOK {
		t.Fatalf("config output Videos: %d %s", code, stderr)
	}
	if data, _ := os.ReadFile(config.FilePath()); !strings.Contains(string(data), filepath.Join(wd, "Videos")) {
		t.Errorf("the relative folder was not saved absolute: %q", data)
	}

	for _, args := range [][]string{
		{"config", "cookies", "/does/not/exist.txt"},
		{"config", "speed", "2"},
		{"config", "a", "b", "c"},
	} {
		if code := app.Run(context.Background(), args); code == ExitOK {
			t.Errorf("%v should fail", args)
		}
	}

	if code := app.Run(context.Background(), []string{"config", "output", ""}); code != ExitOK {
		t.Fatalf("clearing failed: %d %s", code, stderr)
	}
	if data, _ := os.ReadFile(config.FilePath()); strings.Contains(string(data), config.KeyOutputDir) {
		t.Errorf("the cleared key should be gone: %q", data)
	}
}

// fakeTools puts fake yt-dlp, ffmpeg and ffprobe on PATH. yt-dlp writes the
// output file and records it in --print-to-file.
func fakeTools(t *testing.T, ytdlp string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	scripts := map[string]string{
		"yt-dlp":  ytdlp,
		"ffprobe": `echo '{"streams":[{"codec_type":"video","codec_name":"h264","pix_fmt":"yuv420p","width":1920,"height":1080,"disposition":{"attached_pic":0}},{"codec_type":"audio","codec_name":"aac","disposition":{"attached_pic":0}}],"format":{"duration":"10.000000"}}'`,
		"ffmpeg":  `exit 0`,
	}
	for name, script := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

const fakeYtdlp = `out=""; print=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift ;;
    --print-to-file) print="$3"; shift 2 ;;
  esac
  shift
done
file=$(echo "$out" | sed "s/%(title)s/Clip/; s/%(id)s/abc/; s/%(ext)s/mp4/")
echo "[download] Destination: $file"
printf 'x' > "$file"
[ -n "$print" ] && echo "$file" >> "$print"
exit 0`

func TestDownloadEndToEnd(t *testing.T) {
	fakeTools(t, fakeYtdlp)
	app, stdout, stderr := testApp(t)

	if code := app.Run(context.Background(), []string{"https://youtu.be/abc"}); code != ExitOK {
		t.Fatalf("exit code %d, stderr: %s", code, stderr)
	}
	for _, want := range []string{"Downloading: Clip [abc].mp4", "✓ Clip [abc].mp4", "H.264 · 1920×1080", "Saved to"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
	}
}

func TestDownloadFailureShowsTheReason(t *testing.T) {
	fakeTools(t, `echo "ERROR: [youtube] abc: Sign in to confirm you're not a bot" >&2; exit 1`)
	app, _, stderr := testApp(t)

	if code := app.Run(context.Background(), []string{"video", "https://youtu.be/abc"}); code != ExitFailure {
		t.Errorf("exit code %d, want %d", code, ExitFailure)
	}
	if !strings.Contains(stderr.String(), "login or a bot check") || !strings.Contains(stderr.String(), "--cookies") {
		t.Errorf("stderr:\n%s", stderr)
	}
}

func TestDownloadWithoutTools(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	app, _, stderr := testApp(t)
	if code := app.Run(context.Background(), []string{"video", "https://youtu.be/abc"}); code != ExitFailure ||
		!strings.Contains(stderr.String(), "yt-dlp is not installed") {
		t.Errorf("exit code %d, stderr:\n%s", code, stderr)
	}
}

func TestPickJSRuntime(t *testing.T) {
	lookPath := func(present ...string) func(string) (string, error) {
		return func(name string) (string, error) {
			if slices.Contains(present, name) {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("not found")
		}
	}
	// Deno is yt-dlp's default: no --js-runtimes needed.
	for present, want := range map[string]string{"deno,node": "", "node": "node", "": ""} {
		if got := pickJSRuntime(lookPath(strings.Split(present, ",")...)); got != want {
			t.Errorf("pickJSRuntime(%s) = %q, want %q", present, got, want)
		}
	}
}
