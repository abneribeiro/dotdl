// dotdl downloads videos and music with yt-dlp, as MP4 or MP3/M4A files
// that play on any phone or computer.
//
//	dotdl video [flags] <url>...   Download videos
//	dotdl audio [flags] <url>...   Download audio
//	dotdl doctor                   Check the environment
//	dotdl config [key] [value]     Show or change the settings
//	dotdl version                  Print the version
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"syscall"

	"github.com/abneribeiro/dotdl/internal/cli"
	"github.com/abneribeiro/dotdl/internal/config"
	"github.com/abneribeiro/dotdl/internal/ui"
)

// Set with -ldflags "-X main.version=..." by the Makefile, GoReleaser and
// the Dockerfile.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

func main() {
	config.LoadFiles()
	cfg, err := config.Load()
	if err != nil {
		ui.PrintError(os.Stderr, fmt.Errorf("invalid configuration: %w", err))
		os.Exit(cli.ExitFailure)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel})))

	// Signals are handled only here. After the first one, the next falls back
	// to the default behavior and ends the process at once.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	context.AfterFunc(ctx, stop)

	code := cli.NewApp(cfg, versionString()).Run(ctx, os.Args[1:])
	stop()
	os.Exit(code)
}

// versionInfo returns the binary's version, commit and date. Without
// -ldflags (go install, go run) it uses what Go embeds: the module version
// for go install @vX.Y.Z, or the git revision for a local build.
func versionInfo() (v, c, d string) {
	v, c, d = version, commit, date
	if info, ok := debug.ReadBuildInfo(); ok {
		if v == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
			v = info.Main.Version
		}
		for _, s := range info.Settings {
			switch {
			case s.Key == "vcs.revision" && c == "":
				c = s.Value[:min(len(s.Value), 7)]
			case s.Key == "vcs.time" && d == "":
				d = s.Value
			}
		}
	}
	return v, c, d
}

// versionString describes the binary in full (dotdl version).
func versionString() string {
	v, c, d := versionInfo()
	out := v
	if c != "" {
		out += " (" + c
		if d != "" {
			out += ", " + d
		}
		out += ")"
	}
	return fmt.Sprintf("%s %s %s/%s", out, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
