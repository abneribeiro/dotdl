package jobs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abneribeiro/dotdl/internal/downloader"
)

// fakeBin installs an executable with the given sh script on the test PATH.
func fakeBin(t *testing.T, dir, name, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// fakePath creates a folder with only the fake binaries and puts it on PATH
// (with /bin and /usr/bin, so the scripts' sh has the basics).
func fakePath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	return dir
}

// fakeYtdlp imitates yt-dlp: it prints progress in the template's format,
// creates the output file from -o and records it in --print-to-file.
const fakeYtdlp = `
out=""; print=""; ext=mp4
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift ;;
    --print-to-file) print="$3"; shift 2 ;;
    --audio-format) ext="$2"; shift ;;
  esac
  shift
done
file=$(echo "$out" | sed "s/%(title)s/Test/; s/%(id)s/abc/; s/%(ext)s/$ext/")
echo "[download] Destination: $file"
echo "dotdl-progress 512 1024 NA 2048.0 3"
echo "dotdl-progress 1024 1024 NA 2048.0 0"
echo "WARNING: just a warning" >&2
printf 'x' > "$file"
[ -n "$print" ] && echo "$file" >> "$print"
exit 0
`

// fakeFFprobe answers probeMedia's JSON: 8-bit H.264/AAC, so EnsureMP4 does
// not convert.
const fakeFFprobe = `echo '{"streams":[{"codec_type":"video","codec_name":"h264","pix_fmt":"yuv420p","width":1920,"height":1080,"disposition":{"attached_pic":0}},{"codec_type":"audio","codec_name":"aac","disposition":{"attached_pic":0}}],"format":{"duration":"10.000000"}}'`

// collector keeps a Reporter's events.
type collector struct {
	mu     sync.Mutex
	events []Event
}

func (c *collector) report(e Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

func (c *collector) has(pred func(Event) bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.ContainsFunc(c.events, pred)
}

func TestDownloadRunWithFakeBinaries(t *testing.T) {
	bin := fakePath(t)
	fakeBin(t, bin, "yt-dlp", fakeYtdlp)
	fakeBin(t, bin, "ffprobe", fakeFFprobe)

	out := t.TempDir()
	d, err := NewDownload(DownloadInput{URLs: []string{"https://youtu.be/abc"}, OutputDir: out}, "")
	if err != nil {
		t.Fatal(err)
	}

	var c collector
	res, err := d.Run(context.Background(), Env{TempDir: t.TempDir()}, c.report)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	want := filepath.Join(out, "Test [abc].mp4")
	if !slices.Equal(res.Files, []string{want}) || len(res.Converted) != 0 {
		t.Errorf("Files = %v, Converted = %v; want [%s] and nothing converted", res.Files, res.Converted, want)
	}
	// Everything converted: the pending list must not be left behind.
	if _, err := os.Stat(filepath.Join(out, pendingFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the pending list should have been deleted (%v)", err)
	}

	checks := map[string]func(Event) bool{
		"50% event":                func(e Event) bool { return e.Progress && e.Fraction == 0.5 && e.Speed == 2048 },
		"100% event":               func(e Event) bool { return e.Progress && e.Fraction == 1 },
		"the current file as Item": func(e Event) bool { return e.Item == "Test [abc].mp4" },
		"stderr as Log":            func(e Event) bool { return e.Log == "WARNING: just a warning" },
	}
	for name, pred := range checks {
		if !c.has(pred) {
			t.Errorf("missing: %s", name)
		}
	}
	if c.has(func(e Event) bool { return strings.Contains(e.Log, "dotdl-progress") }) {
		t.Error("template lines must not reach the log")
	}
}

// A VP9 download is converted, and the Result says so with the source codec.
func TestDownloadRunReportsConversions(t *testing.T) {
	bin := fakePath(t)
	fakeBin(t, bin, "yt-dlp", fakeYtdlp)
	fakeBin(t, bin, "ffprobe", strings.Replace(fakeFFprobe, `"codec_name":"h264"`, `"codec_name":"vp9"`, 1))
	fakeBin(t, bin, "ffmpeg", `for last; do :; done; printf 'x' > "$last"`)

	out := t.TempDir()
	d, err := NewDownload(DownloadInput{URLs: []string{"https://youtu.be/abc"}, OutputDir: out}, "")
	if err != nil {
		t.Fatal(err)
	}
	var c collector
	res, err := d.Run(context.Background(), Env{TempDir: t.TempDir()}, c.report)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	file := filepath.Join(out, "Test [abc].mp4")
	if res.Converted[file] != "vp9" {
		t.Errorf("Converted = %v, want %s from vp9", res.Converted, file)
	}
	if !c.has(func(e Event) bool { return e.Stage == StageConvert }) {
		t.Error("missing the conversion stage")
	}
}

func TestDownloadRunAudio(t *testing.T) {
	bin := fakePath(t)
	fakeBin(t, bin, "yt-dlp", fakeYtdlp)

	out := t.TempDir()
	d, err := NewDownload(DownloadInput{URLs: []string{"https://youtu.be/abc"}, OutputDir: out, Format: "mp3"}, "")
	if err != nil {
		t.Fatal(err)
	}
	res, err := d.Run(context.Background(), Env{TempDir: t.TempDir()}, func(Event) {})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !slices.Equal(res.Files, []string{filepath.Join(out, "Test [abc].mp3")}) {
		t.Errorf("Files = %v", res.Files)
	}
}

func TestDownloadRunFailsWithNothingDone(t *testing.T) {
	bin := fakePath(t)
	fakeBin(t, bin, "yt-dlp", `echo "ERROR: [youtube] abc: Video unavailable" >&2; exit 1`)

	d, err := NewDownload(DownloadInput{URLs: []string{"https://youtu.be/abc"}, OutputDir: t.TempDir()}, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.Run(context.Background(), Env{TempDir: t.TempDir()}, func(Event) {})
	var dlErr *downloader.DownloadError
	if !errors.As(err, &dlErr) || dlErr.Kind != downloader.ErrUnavailable {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
	if errors.Is(err, ErrPartial) {
		t.Error("nothing downloaded is not a partial result")
	}
}

func TestDownloadRunCanceled(t *testing.T) {
	bin := fakePath(t)
	fakeBin(t, bin, "yt-dlp", `exec sleep 5`)

	d, err := NewDownload(DownloadInput{URLs: []string{"https://youtu.be/abc"}, OutputDir: t.TempDir()}, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := d.Run(ctx, Env{TempDir: t.TempDir()}, func(Event) {}); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestPrepareOutputDir(t *testing.T) {
	tmp := t.TempDir()
	readOnly := filepath.Join(tmp, "read-only")
	if err := os.Mkdir(readOnly, 0o555); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(tmp, "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		dir     string
		wantErr bool
		hint    bool
	}{
		{"creates the folder", filepath.Join(tmp, "new", "sub"), false, false},
		{"existing folder", tmp, false, false},
		{"read-only folder fails early", readOnly, true, true},
		{"a file in the path", filepath.Join(file, "sub"), true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.dir == readOnly && os.Geteuid() == 0 {
				t.Skip("root writes to read-only folders")
			}
			t.Setenv("DOTDL_DOCKER", "1")
			err := PrepareOutputDir(tt.dir)
			if (err != nil) != tt.wantErr {
				t.Fatalf("PrepareOutputDir(%q) = %v, want error: %v", tt.dir, err, tt.wantErr)
			}
			if err != nil && strings.Contains(err.Error(), "in Docker") != tt.hint {
				t.Errorf("Docker hint = %v, want %v: %v", !tt.hint, tt.hint, err)
			}
			if err == nil {
				entries, _ := os.ReadDir(tt.dir)
				for _, e := range entries {
					if strings.HasPrefix(e.Name(), ".dotdl-check-") {
						t.Errorf("the check file %s was left behind", e.Name())
					}
				}
			}
		})
	}
}
