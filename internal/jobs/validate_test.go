package jobs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/abneribeiro/dotdl/internal/downloader"
)

// tempFile creates a temporary file with the given content.
func tempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveOutputDir(t *testing.T) {
	wd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	tests := []struct {
		name, dir, base, want string
	}{
		{"absolute ignores the base", "/from/flag", "/from/env", "/from/flag"},
		{"relative goes inside the base", "music", "/from/env", "/from/env/music"},
		{"relative without a base stays relative", "music", "", "music"},
		{"no -o uses the base", "", "/from/env", "/from/env"},
		{"no -o and no base is the current folder", "", "", wd},
		{"a leading ~ is expanded", "~/Music", "/from/env", filepath.Join(home, "Music")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveOutputDir(tt.dir, tt.base)
			if err != nil || got != tt.want {
				t.Errorf("ResolveOutputDir(%q, %q) = %q, %v; want %q", tt.dir, tt.base, got, err, tt.want)
			}
		})
	}
}

func TestNewDownloadValidation(t *testing.T) {
	urls := tempFile(t, "urls.txt", "https://youtu.be/a\nhttps://youtu.be/b\n")
	one := []string{"https://youtu.be/a"}

	tests := []struct {
		name    string
		in      DownloadInput
		wantErr bool
	}{
		{"one URL", DownloadInput{URLs: one}, false},
		{"batch file", DownloadInput{BatchFile: urls}, false},
		{"trim", DownloadInput{URLs: one, Start: "01:00", End: "02:00"}, false},
		{"quality", DownloadInput{URLs: one, Quality: "720"}, false},
		{"mp3", DownloadInput{URLs: one, Format: "mp3"}, false},
		{"mp3 with best", DownloadInput{URLs: one, Format: "mp3", Quality: "best"}, false},

		{"nothing", DownloadInput{}, true},
		{"URL without a scheme", DownloadInput{URLs: []string{"youtube.com/x"}}, true},
		{"start without end", DownloadInput{URLs: one, Start: "01:00"}, true},
		{"trim with two URLs", DownloadInput{URLs: []string{"https://a.pt/1", "https://a.pt/2"}, Start: "01:00", End: "02:00"}, true},
		{"trim with a batch file", DownloadInput{BatchFile: urls, Start: "01:00", End: "02:00"}, true},
		{"reversed trim", DownloadInput{URLs: one, Start: "02:00", End: "01:00"}, true},
		{"missing batch file", DownloadInput{BatchFile: "/does/not/exist"}, true},
		{"invalid quality", DownloadInput{URLs: one, Quality: "hd"}, true},
		{"quality with audio", DownloadInput{URLs: one, Format: "mp3", Quality: "720"}, true},
		{"unknown format", DownloadInput{URLs: one, Format: "webm"}, true},
		{"missing cookies", DownloadInput{URLs: one, Cookies: "/does/not/exist.txt"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewDownload(tt.in, "/videos"); (err != nil) != tt.wantErr {
				t.Errorf("NewDownload(%+v) error = %v, want error: %v", tt.in, err, tt.wantErr)
			}
		})
	}
}

func TestNewDownloadMapsQualityAndFormat(t *testing.T) {
	d, err := NewDownload(DownloadInput{URLs: []string{"https://youtu.be/a"}, Quality: "2160"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if d.Format != FormatMP4 || d.FormatSort != "res:2160" || d.Height != 2160 || d.Audio() {
		t.Errorf("Download = %+v", d)
	}

	d, err = NewDownload(DownloadInput{URLs: []string{"https://youtu.be/a"}, Format: "m4a"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !d.Audio() || d.FormatSort != "" {
		t.Errorf("Download (m4a) = %+v", d)
	}
}

func TestOutputTemplate(t *testing.T) {
	if full := outputTemplate("/videos", nil); full != "/videos/%(title)s [%(id)s].%(ext)s" {
		t.Errorf("outputTemplate without a trim = %q", full)
	}
	// With the full video's name, yt-dlp said "already downloaded" and the
	// archive recorded the trim in place of the video.
	trimmed := outputTemplate("/videos", &downloader.Section{Start: "01:30", End: "00:02:00"})
	if want := "/videos/%(title)s [%(id)s] (01.30-00.02.00).%(ext)s"; trimmed != want {
		t.Errorf("outputTemplate with a trim = %q, want %q", trimmed, want)
	}
	if strings.Contains(filepath.Base(trimmed), ":") {
		t.Errorf("the name must not contain ':' (invalid on Windows and macOS): %q", trimmed)
	}
}

func TestValidateBatchFile(t *testing.T) {
	tests := []struct {
		name, content string
		wantErr       bool
	}{
		{"valid URLs", "https://youtu.be/a\nhttps://youtu.be/b\n", false},
		{"empty lines and comments are skipped", "# list\n\nhttps://youtu.be/a\n", false},
		{"invalid URL", "https://youtu.be/a\nyoutube.com/x\n", true},
		{"no URLs", "# only comments\n\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateBatchFile(tempFile(t, "urls.txt", tt.content)); (err != nil) != tt.wantErr {
				t.Errorf("validateBatchFile = %v, want error: %v", err, tt.wantErr)
			}
		})
	}
}

func TestPrintedPathsWithoutRepeats(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(existing, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	list := filepath.Join(dir, "paths.txt")
	content := existing + "\n\n" + filepath.Join(dir, "deleted.mkv") + "\n" + existing + "\n"
	if err := os.WriteFile(list, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := printedPaths(list); !slices.Equal(got, []string{existing}) {
		t.Errorf("printedPaths = %v", got)
	}
	if got := printedPaths(filepath.Join(dir, "missing.txt")); got != nil {
		t.Errorf("printedPaths(missing) = %v, want nil", got)
	}
}

func TestCountArchive(t *testing.T) {
	if countArchive("") != 0 || countArchive("/does/not/exist") != 0 {
		t.Error("no archive counts zero")
	}
	if got := countArchive(tempFile(t, "archive.txt", "youtube abc\nyoutube def\n")); got != 2 {
		t.Errorf("countArchive = %d, want 2", got)
	}
}

// A Ctrl+C before a file's turn keeps the pending list, so the next run
// converts it.
func TestEnsurePlayableInterruptedKeepsPending(t *testing.T) {
	dir := t.TempDir()
	video := tempFile(t, "video.webm", "x")
	pending := filepath.Join(dir, pendingFileName)
	if err := os.WriteFile(pending, []byte(video+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := ensurePlayable(ctx, pending, &reporterSink{report: func(Event) {}}); !errors.Is(err, context.Canceled) {
		t.Errorf("ensurePlayable = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(pending); err != nil {
		t.Errorf("the pending list must stay: %v", err)
	}
}
