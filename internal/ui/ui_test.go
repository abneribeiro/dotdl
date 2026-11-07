package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/abneribeiro/dotdl/internal/doctor"
	"github.com/abneribeiro/dotdl/internal/downloader"
	"github.com/abneribeiro/dotdl/internal/jobs"
)

func TestHumanBytesAndRemaining(t *testing.T) {
	for in, want := range map[float64]string{512: "512 B", 2048: "2.0 KB", 3.5 * 1024 * 1024: "3.5 MB"} {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%v) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[time.Duration]string{-1: "", 42 * time.Second: "0:42", 3909 * time.Second: "1:05:09"} {
		if got := remaining(in); got != want {
			t.Errorf("remaining(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestProgressModelAppliesEvents(t *testing.T) {
	m := newProgressModel("video", func() {})
	m.apply(jobs.Event{Stage: jobs.StageDownload, Item: "Song [abc].mp4", Index: 2, Count: 3})
	m.apply(jobs.Event{Progress: true, Fraction: 0.61, Speed: 3 * 1024 * 1024, ETA: 12 * time.Second})
	m.apply(jobs.Event{Log: "[youtube] abc: Downloading webpage"})
	m.apply(jobs.Event{Log: "WARNING: only 720p available"})

	out := plain(m.render())
	for _, want := range []string{"dotdl", "video", "2 of 3", "Downloading", "Song [abc].mp4", "61%",
		"3.0 MB/s", "0:12 left", "only 720p available"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Downloading webpage") {
		t.Error("yt-dlp's chatter must stay hidden")
	}

	// A new stage starts with unknown progress: the bar pulses, no percentage.
	m.apply(jobs.Event{Stage: jobs.StageMerge})
	if out := plain(m.render()); strings.Contains(out, "%") || !strings.Contains(out, "Merging") {
		t.Errorf("a stage without progress should show no percentage:\n%s", out)
	}
}

// Nothing may be wider than the terminal, whatever the item's length.
func TestProgressFitsTheTerminal(t *testing.T) {
	for _, width := range []int{40, 60, 80, 120} {
		m := newProgressModel("video · up to 2160p", func() {})
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		m.apply(jobs.Event{Stage: jobs.StageConvert, Item: strings.Repeat("A very long title ", 10), Index: 10, Count: 120})
		m.apply(jobs.Event{Progress: true, Fraction: 0.5, Speed: 1 << 20, ETA: time.Hour})
		m.apply(jobs.Event{Log: "WARNING: " + strings.Repeat("long warning ", 20)})
		for _, line := range strings.Split(m.render(), "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("width %d: a line is %d columns: %q", width, w, plain(line))
			}
		}
	}
}

func TestProgressCtrlCCancels(t *testing.T) {
	canceled := false
	m := newProgressModel("video", func() { canceled = true })
	m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !canceled || !strings.Contains(plain(m.render()), "Canceling") {
		t.Errorf("ctrl+c should cancel the job and say so (canceled=%v)", canceled)
	}
	if _, cmd := m.Update(doneMsg{}); cmd == nil {
		t.Error("the end of the job should quit the program")
	}
	if m.View().Content != "" {
		t.Error("the view should be cleared once done, before the summary is printed")
	}
}

func TestRunPlain(t *testing.T) {
	var out bytes.Buffer
	res, err := RunPlain(context.Background(), &out, func(_ context.Context, report jobs.Reporter) (jobs.Result, error) {
		report(jobs.Event{Stage: jobs.StageInfo})
		report(jobs.Event{Stage: jobs.StageDownload, Item: "a.mp4"})
		report(jobs.Event{Progress: true, Fraction: 0.5})
		report(jobs.Event{Log: "WARNING: careful"})
		report(jobs.Event{Stage: jobs.StageDownload, Item: "b.mp4"})
		return jobs.Result{Files: []string{"/v/a.mp4"}}, nil
	})
	if err != nil || len(res.Files) != 1 {
		t.Fatalf("RunPlain = %v, %v", res, err)
	}
	want := "Fetching info\nDownloading: a.mp4\n  warning: careful\nDownloading: b.mp4\n"
	if out.String() != want {
		t.Errorf("plain output =\n%q\nwant\n%q", out.String(), want)
	}
}

func TestPrintResult(t *testing.T) {
	var out bytes.Buffer
	PrintResult(&out, jobs.Result{
		OutputDir: "/videos",
		Files:     []string{"/videos/a.mp4", "/videos/b.mp4"},
		Converted: map[string]string{"/videos/b.mp4": "vp9"},
	}, func(p string) string { return "H.264 · 1920×1080" })
	got := out.String()
	for _, want := range []string{"✓ a.mp4", "H.264 · 1920×1080", "(converted from VP9)", "Saved to /videos"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\x1b[") {
		t.Error("output to a non-terminal must have no ANSI codes")
	}

	out.Reset()
	PrintResult(&out, jobs.Result{OutputDir: "/videos"}, nil)
	if !strings.Contains(out.String(), "Nothing new to download") {
		t.Errorf("empty result = %q", out.String())
	}
}

func TestPrintError(t *testing.T) {
	var out bytes.Buffer
	PrintError(&out, &downloader.DownloadError{Kind: downloader.ErrLoginRequired})
	if !strings.Contains(out.String(), "✗ the site asked for a login") || !strings.Contains(out.String(), "--cookies") {
		t.Errorf("login error =\n%s", out.String())
	}

	out.Reset()
	PrintError(&out, fmt.Errorf("%w: %w", jobs.ErrPartial, errors.New("1 of 3 failed")))
	if got := out.String(); !strings.Contains(got, "! Some items failed: 1 of 3 failed") {
		t.Errorf("partial error = %q", got)
	}
}

func TestPrintDoctor(t *testing.T) {
	var out bytes.Buffer
	PrintDoctor(&out, []doctor.Check{
		{Name: "yt-dlp", Detail: "2026.10.26"},
		{Name: "ffmpeg", Level: doctor.LevelFail, Detail: "not found", Fix: "make deps"},
	})
	got := out.String()
	for _, want := range []string{"✓ yt-dlp", "✗ ffmpeg", "→ make deps", "1 problem to fix."} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestPrintHelpAlignsColumns(t *testing.T) {
	var out bytes.Buffer
	PrintHelp(&out, Help{Usage: "dotdl x", Description: "test", Sections: []HelpSection{
		{Title: "Flags", Rows: [][2]string{{"-a", "first"}, {"--long-name", "second"}}},
	}})
	lines := strings.Split(out.String(), "\n")
	var cols []int
	for _, l := range lines {
		if i := strings.Index(l, "first"); i >= 0 {
			cols = append(cols, i)
		}
		if i := strings.Index(l, "second"); i >= 0 {
			cols = append(cols, i)
		}
	}
	if len(cols) != 2 || cols[0] != cols[1] {
		t.Errorf("descriptions are not aligned:\n%s", out.String())
	}
}

// The pulse of an indeterminate bar always fills the bar's width exactly,
// whatever frame it is on, including when it enters and leaves.
func TestIndeterminateBarKeepsItsWidth(t *testing.T) {
	m := newProgressModel("video", func() {})
	for ticks := range 40 {
		m.ticks = ticks
		if w := lipgloss.Width(m.renderBar(20)); w != 20 {
			t.Fatalf("tick %d: bar is %d columns, want 20", ticks, w)
		}
	}
}

// Doctor and config rows line up their second column, even with styled
// names.
func TestPrintersAlignColumns(t *testing.T) {
	var out bytes.Buffer
	PrintDoctor(&out, []doctor.Check{{Name: "yt-dlp", Detail: "first"}, {Name: "JavaScript", Detail: "second"}})
	PrintConfig(&out, "/cfg", []Setting{
		{Key: "output", Value: "third", Description: "d"},
		{Key: "cookies", Value: "fourth", Description: "d"},
	})
	got := out.String()
	col := func(word string) int {
		for line := range strings.SplitSeq(got, "\n") {
			if i := strings.Index(line, word); i >= 0 {
				return lipgloss.Width(line[:i])
			}
		}
		t.Fatalf("missing %q in:\n%s", word, got)
		return 0
	}
	if col("first") != col("second") || col("third") != col("fourth") {
		t.Errorf("columns are not aligned:\n%s", got)
	}
}

// plain removes ANSI codes so views can be compared as text.
func plain(s string) string {
	return ansi.Strip(s)
}
