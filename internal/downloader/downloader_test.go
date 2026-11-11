package downloader

import (
	"context"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

// Canceling must stop the process long before WaitDelay: yt-dlp and ffmpeg
// are interrupted, not left to run until they are killed.
func TestInterruptOnCancelStopsTheProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs sh and sleep")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 30")
	interruptOnCancel(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	cancel()

	start := time.Now()
	if err := cmd.Wait(); err == nil {
		t.Error("an interrupted process must return an error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("the process took %s to stop", elapsed)
	}
}
