package doctor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/abneribeiro/dotdl/internal/config"
)

// fakeSystem simulates a PATH with the binaries in outputs (name to the
// output of "--version"/"-version").
func fakeSystem(now time.Time, outputs map[string]string) system {
	return system{
		lookPath: func(name string) (string, error) {
			if _, ok := outputs[name]; ok {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("not found")
		},
		output: func(_ context.Context, name string, _ ...string) (string, error) {
			return outputs[name], nil
		},
		now: now,
	}
}

func byName(checks []Check, name string) (Check, bool) {
	for _, c := range checks {
		if c.Name == name {
			return c, true
		}
	}
	return Check{}, false
}

func TestCollect(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	cfg := &config.Config{TempDir: t.TempDir(), OutputDir: t.TempDir()}

	checks := collect(context.Background(), cfg, fakeSystem(now, map[string]string{
		"yt-dlp":  "2026.10.26\n",
		"ffmpeg":  "ffmpeg version 7.1.5-0+deb13u1 Copyright (c) 2000-2026\n",
		"ffprobe": "ffprobe version 7.1.5-0+deb13u1 Copyright (c) 2000-2026\n",
		"deno":    "deno 2.9.8 (stable, release, x86_64-unknown-linux-gnu)\n",
	}))
	for _, c := range checks {
		if c.Level != LevelOK {
			t.Errorf("%s: level %d (%s), want OK in a complete environment", c.Name, c.Level, c.Detail)
		}
	}
	if HasFailures(checks) {
		t.Error("a complete environment has no failures")
	}
	for name, want := range map[string]string{
		"ffmpeg":     "7.1.5-0+deb13u1",
		"JavaScript": "deno 2.9.8",
		"yt-dlp":     "2026.10.26 (2 days old)",
	} {
		if c, _ := byName(checks, name); c.Detail != want {
			t.Errorf("%s detail = %q, want %q", name, c.Detail, want)
		}
	}
}

func TestCollectIncompleteEnvironment(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	cfg := &config.Config{TempDir: t.TempDir(), OutputDir: t.TempDir()}

	checks := collect(context.Background(), cfg, fakeSystem(now, map[string]string{
		"yt-dlp": "2026.01.10\n", // ~9 months old
		"node":   "v26.3.1\n",
	}))

	for name, want := range map[string]Level{
		"yt-dlp":     LevelWarn, // old, but works
		"ffmpeg":     LevelFail, // no merging or conversion without it
		"ffprobe":    LevelFail, // no format check without it
		"JavaScript": LevelOK,   // node works
		"temp":       LevelOK,
		"output":     LevelOK,
	} {
		c, ok := byName(checks, name)
		if !ok || c.Level != want {
			t.Errorf("%s: level %d (%s), want %d", name, c.Level, c.Detail, want)
		}
		if c.Level != LevelOK && c.Fix == "" {
			t.Errorf("%s: a problem must say how to fix it", name)
		}
	}
	if !HasFailures(checks) {
		t.Error("missing ffmpeg is a failure")
	}
	// Without DOTDL_COOKIES_FILE, the cookies line does not show at all.
	if c, ok := byName(checks, "cookies"); ok {
		t.Errorf("unexpected cookies check: %+v", c)
	}
}

func TestYtdlpReleaseDate(t *testing.T) {
	tests := []struct {
		in   string
		want time.Time
		ok   bool
	}{
		{"2026.10.26", time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), true},
		{"2026.10.26.234255", time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), true}, // nightly
		{" 2025.11.12\n", time.Date(2025, 11, 12, 0, 0, 0, 0, time.UTC), true},
		{"2026.13.01", time.Time{}, false},
		{"stable@2026.10.26", time.Time{}, false},
		{"", time.Time{}, false},
	}
	for _, tt := range tests {
		got, ok := ytdlpReleaseDate(tt.in)
		if ok != tt.ok || !got.Equal(tt.want) {
			t.Errorf("ytdlpReleaseDate(%q) = (%v, %v), want (%v, %v)", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestVersionAfter(t *testing.T) {
	tests := []struct{ out, prefix, want string }{
		{"ffmpeg version 7.1.5-0+deb13u1 Copyright (c)\nbuilt with gcc", "ffmpeg version ", "7.1.5-0+deb13u1"},
		{"deno 2.9.8 (stable, release)\nv8 14.0", "deno ", "2.9.8"},
		{"something unexpected", "ffmpeg version ", ""},
		{"", "deno ", ""},
	}
	for _, tt := range tests {
		if got := versionAfter(tt.out, tt.prefix); got != tt.want {
			t.Errorf("versionAfter(%q, %q) = %q, want %q", tt.out, tt.prefix, got, tt.want)
		}
	}
}

func TestFFmpegInstall(t *testing.T) {
	for goos, want := range map[string]string{
		"darwin":  "brew install ffmpeg",
		"windows": "winget install ffmpeg",
		"linux":   "sudo apt install ffmpeg",
	} {
		if got := ffmpegInstall(goos); got != want {
			t.Errorf("ffmpegInstall(%q) = %q, want %q", goos, got, want)
		}
	}
}
