// Package downloader runs yt-dlp and ffmpeg. It knows nothing about the
// interface: progress and output lines go out through callbacks, and the
// caller decides how to show them.
package downloader

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/abneribeiro/dotdl/internal/procio"
)

// Run executes yt-dlp with the given options. Canceling the context sends
// SIGINT (not SIGKILL) so yt-dlp finalizes its .part files: that is what
// makes resuming (--continue) reliable after an interruption.
//
// Both outputs are read line by line: with o.Progress set, progress lines and
// phase changes (see ParseLine) go to the callback, and every other line to
// o.Log. Stderr also feeds a tail used to classify the error.
func Run(ctx context.Context, o Options) error {
	args := BuildArgs(o)
	slog.Debug("running yt-dlp", "args", args)

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 10 * time.Second

	logLine := func(string) {}
	if o.Log != nil {
		logLine = o.Log
	}
	tail := &procio.TailBuffer{Max: 16 * 1024}
	stderr := procio.NewLineWriter(logLine)
	cmd.Stderr = io.MultiWriter(stderr, tail)
	stdout := procio.NewLineWriter(func(line string) {
		if o.Progress != nil {
			if p, ok := ParseLine(line); ok {
				o.Progress(p)
				// Template lines are only for the callback.
				if strings.HasPrefix(line, progressMarker) {
					return
				}
			}
		}
		logLine(line)
	})
	cmd.Stdout = stdout

	err := cmd.Run()
	stdout.Flush()
	stderr.Flush()
	if err != nil {
		dlErr := classify(tail.String(), ctx.Err(), err)
		if dlErr.Kind != ErrCanceled {
			slog.Debug("yt-dlp failed", "err", err, "output", procio.Tail(tail.String(), 2048))
		}
		return dlErr
	}
	return nil
}
