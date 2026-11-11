// Package jobs is dotdl's engine: it validates a download request, runs it
// and reports progress as events. It never writes to the terminal; the
// caller decides how to show each Event.
package jobs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/abneribeiro/dotdl/internal/config"
)

// ErrPartial means part of the work finished and part failed, which is
// normal for a playlist with a private video in the middle. The Result says
// what was done.
var ErrPartial = errors.New("some items failed")

// Stage is what a job is doing, ready to show.
type Stage string

const (
	StageInfo     Stage = "Fetching info"
	StageDownload Stage = "Downloading"
	StageMerge    Stage = "Merging"
	StageExtract  Stage = "Extracting audio"
	StageConvert  Stage = "Converting to MP4"
)

// Event is a job update. Empty fields leave what was known unchanged: an
// Event with only Log is just a log line.
type Event struct {
	// Stage, when not empty, is the new stage.
	Stage Stage
	// Fraction between 0 and 1; negative means unknown (indeterminate bar).
	// It only means something in an Event with Progress set.
	Fraction float64
	Progress bool
	// Speed (bytes/s) and ETA (negative = unknown) of the download.
	Speed float64
	ETA   time.Duration
	// Item is the file being worked on.
	Item string
	// Index and Count: position in a list (playlist, several URLs).
	Index int
	Count int
	// Log is an output line of yt-dlp.
	Log string
	// Note is a message from dotdl itself, for the user to read.
	Note string
}

// Reporter receives a job's events. It may be called from several goroutines
// (the processes' stdout and stderr), so it must be safe for that.
type Reporter func(Event)

// Result describes what a job produced.
type Result struct {
	// OutputDir is the output folder.
	OutputDir string
	// Files are the final files created or updated.
	Files []string
	// Converted lists the files that had to be converted to MP4, with the
	// codec they came in.
	Converted map[string]string
}

// Env is what jobs need from the configuration.
type Env struct {
	TempDir     string
	JSRuntimes  string
	CookiesFile string
}

// EnvFrom extracts the Env from a loaded configuration.
func EnvFrom(cfg *config.Config) Env {
	return Env{
		TempDir:     cfg.TempDir,
		JSRuntimes:  cfg.JSRuntimes,
		CookiesFile: cfg.CookiesFile,
	}
}

// PrepareOutputDir creates the output folder and checks it is writable
// before the job starts, not after minutes of downloading.
func PrepareOutputDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return outputDirError(dir, err)
	}
	f, err := os.CreateTemp(dir, ".dotdl-check-*")
	if err != nil {
		return outputDirError(dir, err)
	}
	f.Close()
	_ = os.Remove(f.Name()) // a forgotten empty file does no harm
	return nil
}

// outputDirError explains the failure. Inside Docker (the image sets
// DOTDL_DOCKER) only the folders the wrapper mounted at start are visible.
func outputDirError(dir string, err error) error {
	if os.Getenv("DOTDL_DOCKER") != "" && (errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrNotExist)) {
		return fmt.Errorf("cannot write to %s: %w (in Docker only your home, the Windows drives under /mnt "+
			"and the configured output folder are visible)", dir, err)
	}
	return fmt.Errorf("cannot write to %s: %w", dir, err)
}
