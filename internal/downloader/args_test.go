package downloader

import (
	"slices"
	"strings"
	"testing"
)

func TestBuildArgs(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want []string
	}{
		{
			name: "urls with archive",
			opts: Options{
				URLs:           []string{"https://youtu.be/abc", "https://x.com/u/status/1"},
				OutputTemplate: "/videos/%(title)s [%(id)s].%(ext)s",
				ArchiveFile:    "/videos/.dotdl-archive.txt",
				Continue:       true,
				IgnoreErrors:   true,
			},
			want: []string{
				"-o", "/videos/%(title)s [%(id)s].%(ext)s",
				"--download-archive", "/videos/.dotdl-archive.txt",
				"--continue", "--ignore-errors", "--newline",
				"https://youtu.be/abc", "https://x.com/u/status/1",
			},
		},
		{
			name: "batch file",
			opts: Options{BatchFile: "/home/user/urls.txt", OutputTemplate: "/videos/%(id)s.%(ext)s"},
			want: []string{
				"-o", "/videos/%(id)s.%(ext)s", "--newline",
				"--batch-file", "/home/user/urls.txt",
			},
		},
		{
			// Without the MP4 sort, yt-dlp picks AV1/VP9 + Opus and the file
			// does not open on an iPhone.
			name: "mp4",
			opts: Options{
				URLs:           []string{"https://youtu.be/abc"},
				FormatSort:     MP4Sort(""),
				MergeFormat:    "mp4",
				OutputTemplate: "/videos/%(title)s [%(id)s].%(ext)s",
				PrintToFile:    "/tmp/paths.txt",
			},
			want: []string{
				"-S", "res,hdr:sdr,vcodec:h264,acodec:aac",
				"--merge-output-format", "mp4",
				"-o", "/videos/%(title)s [%(id)s].%(ext)s",
				"--newline",
				"--print-to-file", "after_move:filepath", "/tmp/paths.txt", "--no-simulate",
				"https://youtu.be/abc",
			},
		},
		{
			name: "audio, cookies and js runtime",
			opts: Options{
				URLs:          []string{"https://youtu.be/abc"},
				Format:        "ba/b",
				ExtractAudio:  "mp3",
				EmbedMetadata: true,
				CookiesFile:   "/home/user/cookies.txt",
				JSRuntimes:    "node",
			},
			want: []string{
				"-f", "ba/b",
				"-x", "--audio-format", "mp3", "--audio-quality", "0",
				"--embed-metadata", "--embed-thumbnail",
				"--cookies", "/home/user/cookies.txt",
				"--js-runtimes", "node",
				"--newline",
				"https://youtu.be/abc",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BuildArgs(tt.opts); !slices.Equal(got, tt.want) {
				t.Errorf("BuildArgs =\n%v\nwant\n%v", got, tt.want)
			}
		})
	}
}

// A trim forces --no-playlist and adds the section with keyframe cuts.
func TestBuildArgsWithSection(t *testing.T) {
	got := BuildArgs(Options{
		URLs:       []string{"https://youtu.be/abc"},
		NoPlaylist: true,
		Section:    &Section{Start: "00:10", End: "00:20"},
	})
	for _, want := range []string{"--no-playlist", "--download-sections", "*00:10-00:20", "--force-keyframes-at-cuts"} {
		if !slices.Contains(got, want) {
			t.Errorf("missing %q: %v", want, got)
		}
	}
}

// Unset options must not produce flags: a "--cookies" without a value would
// swallow the next URL.
func TestBuildArgsOmitsUnsetOptions(t *testing.T) {
	got := BuildArgs(Options{URLs: []string{"https://youtu.be/abc"}})
	for _, flag := range []string{"--cookies", "--js-runtimes", "-x", "--embed-metadata", "--progress-template"} {
		if slices.Contains(got, flag) {
			t.Errorf("unexpected %q without the option set: %v", flag, got)
		}
	}
}

// With a progress callback, yt-dlp must get the template ParseLine reads.
func TestBuildArgsWithProgress(t *testing.T) {
	got := BuildArgs(Options{URLs: []string{"u"}, Progress: func(Progress) {}})
	i := slices.Index(got, "--progress-template")
	if i < 0 || got[i+1] != progressTemplate {
		t.Fatalf("missing --progress-template: %v", got)
	}
	if !strings.Contains(progressTemplate, progressMarker) {
		t.Error("the template must carry the marker")
	}
}

func TestQualitySort(t *testing.T) {
	tests := []struct {
		q       string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{"best", "", false},
		{"720", "res:720", false},
		{"1080", "res:1080", false},
		{"abc", "", true},
		{"0", "", true},
		{"-720", "", true},
	}
	for _, tt := range tests {
		got, err := QualitySort(tt.q)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("QualitySort(%q) = %q, %v; want %q (error: %v)", tt.q, got, err, tt.want, tt.wantErr)
		}
		// The cap is yt-dlp's "res" (the short side), never the height: a
		// [height<=720] filter threw away a vertical 720x1280 and delivered
		// 360x640.
		if strings.Contains(got, "height") {
			t.Errorf("QualitySort(%q) must not filter on height: %q", tt.q, got)
		}
	}
}

// The resolution must come BEFORE vcodec: otherwise a 480p H.264 beat a
// 2160p AV1 and asking for 2160 was silently ignored.
func TestMP4Sort(t *testing.T) {
	for res, want := range map[string]string{
		"":         "res,hdr:sdr,vcodec:h264,acodec:aac",
		"res:720":  "res:720,hdr:sdr,vcodec:h264,acodec:aac",
		"res:2160": "res:2160,hdr:sdr,vcodec:h264,acodec:aac",
	} {
		if got := MP4Sort(res); got != want {
			t.Errorf("MP4Sort(%q) = %q, want %q", res, got, want)
		}
	}
}
