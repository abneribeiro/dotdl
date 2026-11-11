// Package cli is dotdl's command line: it parses the commands and flags,
// hands requests to the engine (internal/jobs) and shows the results with
// internal/ui. Run returns the process exit code.
package cli

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"

	"github.com/abneribeiro/dotdl/internal/config"
	"github.com/abneribeiro/dotdl/internal/doctor"
	"github.com/abneribeiro/dotdl/internal/ui"
)

// Exit codes.
const (
	ExitOK      = 0
	ExitFailure = 1
	ExitUsage   = 2
	// ExitInterrupted is 128 + SIGINT, as shells do.
	ExitInterrupted = 130
)

// App is what the commands need from the outside world.
type App struct {
	Config  *config.Config
	Version string
	Stdout  io.Writer
	Stderr  io.Writer
	// Interactive draws the animated progress view; otherwise the output is
	// plain lines (pipes, cron, containers without a terminal).
	Interactive bool
}

// NewApp returns an App writing to the process's stdout and stderr.
func NewApp(cfg *config.Config, version string) *App {
	return &App{
		Config:      cfg,
		Version:     version,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		Interactive: term.IsTerminal(os.Stdout.Fd()),
	}
}

// Run executes the command in args and returns the exit code.
func (a *App) Run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		ui.PrintHelp(a.Stdout, rootHelp)
		return ExitOK
	}
	cmd, rest := args[0], args[1:]
	if _, ok := commandHelp[cmd]; ok && len(rest) > 0 && (rest[0] == "-h" || rest[0] == "--help") {
		return a.help([]string{cmd})
	}
	switch cmd {
	case "video":
		return a.download(ctx, kindVideo, rest)
	case "audio":
		return a.download(ctx, kindAudio, rest)
	case "doctor":
		return a.doctor(ctx)
	case "config":
		return a.config(rest)
	case "version", "-v", "--version":
		ui.PrintVersion(a.Stdout, a.Version)
		return ExitOK
	case "help", "-h", "--help":
		return a.help(rest)
	}
	// "dotdl <url>" is a shortcut for "dotdl video <url>".
	if strings.HasPrefix(cmd, "http://") || strings.HasPrefix(cmd, "https://") {
		return a.download(ctx, kindVideo, args)
	}
	ui.PrintUsageError(a.Stderr, unknownCommandError(cmd), "dotdl help")
	return ExitUsage
}

func (a *App) help(args []string) int {
	if len(args) == 0 {
		ui.PrintHelp(a.Stdout, rootHelp)
		return ExitOK
	}
	h, ok := commandHelp[args[0]]
	if !ok {
		ui.PrintUsageError(a.Stderr, unknownCommandError(args[0]), "dotdl help")
		return ExitUsage
	}
	ui.PrintHelp(a.Stdout, h)
	return ExitOK
}

func (a *App) doctor(ctx context.Context) int {
	checks := doctor.Collect(ctx, a.Config)
	ui.PrintDoctor(a.Stdout, checks)
	if doctor.HasFailures(checks) {
		return ExitFailure
	}
	return ExitOK
}

type unknownCommandError string

func (e unknownCommandError) Error() string {
	return `unknown command "` + string(e) + `"`
}

var rootHelp = ui.Help{
	Usage:       "dotdl <command> [flags]",
	Description: "videos and music from YouTube, Instagram, X, TikTok and more",
	Sections: []ui.HelpSection{
		{Title: "Commands", Rows: [][2]string{
			{"video <url>...", "Download videos as MP4 that plays on any phone or computer"},
			{"audio <url>...", "Download audio as MP3 or M4A with cover art and tags"},
			{"doctor", "Check that yt-dlp, ffmpeg and Deno are installed"},
			{"config [key] [value]", "Show or change the settings"},
			{"version", "Print the version"},
			{"help <command>", "Show a command's flags"},
		}},
		{Title: "Examples", Rows: [][2]string{
			{"dotdl video https://youtu.be/xxxx", ""},
			{"dotdl video -q 720 -o lessons https://youtube.com/playlist?list=yyyy", ""},
			{"dotdl audio -f m4a https://youtu.be/xxxx", ""},
			{"dotdl https://x.com/user/status/123", ""},
		}},
	},
	Footer: "Running the same command again skips what was downloaded and resumes what was interrupted.",
}

var commandHelp = map[string]ui.Help{
	"video":  videoHelp,
	"audio":  audioHelp,
	"doctor": {Usage: "dotdl doctor", Description: "check that everything dotdl needs is installed"},
	"config": configHelp,
}
