// Package doctor checks that the environment has what dotdl needs and says
// how to fix what is missing. It is the first thing to run when a download
// fails for no clear reason.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/abneribeiro/dotdl/internal/config"
)

// Level is how bad a check result is.
type Level int

const (
	LevelOK Level = iota
	LevelWarn
	LevelFail
)

// Check is one line of the report.
type Check struct {
	Name   string
	Level  Level
	Detail string
	// Fix says how to solve it; empty when all is well.
	Fix string
}

const (
	// ytdlpMaxAge is the age past which yt-dlp has probably fallen behind
	// YouTube's changes, which break downloads.
	ytdlpMaxAge = 60 * 24 * time.Hour

	// commandTimeout caps each "--version": the standalone yt-dlp is slow to
	// start the first time, while it unpacks.
	commandTimeout = 30 * time.Second

	// installDeps is how to get yt-dlp, deno and ffmpeg, for both ways of
	// installing dotdl: the release installer and a clone.
	installDeps = "rerun the installer with DOTDL_DEPS=1 (from a clone: make deps)"
)

// system abstracts the system so tests do not depend on the real PATH.
type system struct {
	lookPath func(string) (string, error)
	output   func(ctx context.Context, name string, args ...string) (string, error)
	now      time.Time
}

// Collect runs the checks and returns them.
func Collect(ctx context.Context, cfg *config.Config) []Check {
	return collect(ctx, cfg, system{lookPath: exec.LookPath, output: commandOutput, now: time.Now()})
}

// HasFailures reports whether any check failed.
func HasFailures(checks []Check) bool {
	for _, c := range checks {
		if c.Level == LevelFail {
			return true
		}
	}
	return false
}

func commandOutput(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out), err
}

func collect(ctx context.Context, cfg *config.Config, sys system) []Check {
	checks := []Check{
		checkYtdlp(ctx, sys),
		checkTool(ctx, sys, "ffmpeg", "ffmpeg version "),
		checkTool(ctx, sys, "ffprobe", "ffprobe version "),
		checkJSRuntime(ctx, cfg, sys),
		checkConfigFile(),
		checkOutputDir(cfg),
		checkTempDir(cfg),
	}
	if cfg.CookiesFile != "" {
		// config.Load already made sure it exists.
		checks = append(checks, Check{Name: "cookies", Level: LevelOK, Detail: cfg.CookiesFile})
	}
	return checks
}

func checkYtdlp(ctx context.Context, sys system) Check {
	c := Check{Name: "yt-dlp"}
	if _, err := sys.lookPath("yt-dlp"); err != nil {
		c.Level, c.Detail, c.Fix = LevelFail, "not found", installDeps
		return c
	}
	out, err := sys.output(ctx, "yt-dlp", "--version")
	if err != nil {
		c.Level, c.Detail, c.Fix = LevelFail, "does not start: "+firstLine(out), installDeps
		return c
	}

	version := firstLine(out)
	released, ok := ytdlpReleaseDate(version)
	if !ok {
		c.Detail = version
		return c
	}
	age := sys.now.Sub(released)
	c.Detail = fmt.Sprintf("%s (%s)", version, describeAge(age))
	if age > ytdlpMaxAge {
		// YouTube changes often and yt-dlp keeps up: an old version is the
		// most common reason downloads stop working.
		c.Level, c.Fix = LevelWarn, "yt-dlp -U, or "+installDeps+"; for the Docker image: make update-ytdlp"
	}
	return c
}

// checkTool checks a required binary and shows the version it announces on
// the first line of "-version" (after prefix).
func checkTool(ctx context.Context, sys system, name, prefix string) Check {
	c := Check{Name: name}
	if _, err := sys.lookPath(name); err != nil {
		c.Level, c.Detail, c.Fix = LevelFail, "not found", ffmpegInstall(runtime.GOOS)+", or "+installDeps
		return c
	}
	out, err := sys.output(ctx, name, "-version")
	if err != nil {
		c.Level, c.Detail, c.Fix = LevelFail, "does not start: "+firstLine(out), "reinstall ffmpeg"
		return c
	}
	c.Detail = versionAfter(out, prefix)
	if c.Detail == "" {
		c.Detail = "found"
	}
	return c
}

// ffmpegInstall is the usual command to install ffmpeg on goos.
func ffmpegInstall(goos string) string {
	switch goos {
	case "darwin":
		return "brew install ffmpeg"
	case "windows":
		return "winget install ffmpeg"
	}
	return "sudo apt install ffmpeg"
}

func checkJSRuntime(ctx context.Context, cfg *config.Config, sys system) Check {
	c := Check{Name: "JavaScript"}
	if cfg.JSRuntimes != "" {
		c.Detail = cfg.JSRuntimes + " (DOTDL_JS_RUNTIMES)"
		return c
	}
	if _, err := sys.lookPath("deno"); err == nil {
		out, _ := sys.output(ctx, "deno", "--version") // the binary exists; a missing version is cosmetic
		c.Detail = "deno " + versionAfter(out, "deno ")
		return c
	}
	if _, err := sys.lookPath("node"); err == nil {
		out, _ := sys.output(ctx, "node", "--version") // idem
		c.Detail = "node " + firstLine(out) + " (picked automatically; deno is recommended)"
		return c
	}
	c.Level = LevelWarn
	c.Detail = "no runtime (deno or node): YouTube may fail or offer fewer formats"
	c.Fix = "install deno: " + installDeps
	return c
}

// checkConfigFile shows where the settings live.
func checkConfigFile() Check {
	path := config.FilePath()
	c := Check{Name: "config", Detail: path}
	if _, err := os.Stat(path); err != nil {
		c.Detail += " (not created yet; the defaults are fine)"
	}
	return c
}

func checkOutputDir(cfg *config.Config) Check {
	c := Check{Name: "output"}
	dir := cfg.OutputDir
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			c.Level, c.Detail = LevelWarn, "cannot determine the current folder"
			return c
		}
		dir = wd
	}
	c.Detail = dir

	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		c.Detail += " (created on the first download)"
	case err != nil:
		c.Level, c.Fix = LevelWarn, err.Error()
	case !info.IsDir():
		c.Level, c.Fix = LevelFail, "DOTDL_OUTPUT_DIR points to a file"
	case !writable(dir):
		c.Level, c.Fix = LevelWarn, "not writable: run dotdl config output <folder>, or use -o"
	}
	return c
}

func checkTempDir(cfg *config.Config) Check {
	c := Check{Name: "temp", Detail: cfg.TempDir}
	info, err := os.Stat(cfg.TempDir)
	switch {
	case err != nil:
		c.Level, c.Fix = LevelFail, "DOTDL_TEMP_DIR does not exist: "+err.Error()
	case !info.IsDir():
		c.Level, c.Fix = LevelFail, "DOTDL_TEMP_DIR points to a file"
	case !writable(cfg.TempDir):
		c.Level, c.Fix = LevelFail, "DOTDL_TEMP_DIR is not writable"
	}
	return c
}

// writable confirms writing by creating and deleting a file: permissions
// alone can lie (ACLs, read-only file systems).
func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".dotdl-doctor-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	_ = os.Remove(name) // a forgotten empty file does no harm
	return true
}

// ytdlpReleaseDate reads yt-dlp's version, which is the release date
// ("2026.10.26", or "2026.10.26.234255" for nightlies).
func ytdlpReleaseDate(version string) (time.Time, bool) {
	parts := strings.Split(strings.TrimSpace(version), ".")
	if len(parts) < 3 {
		return time.Time{}, false
	}
	var ymd [3]int
	for i := range ymd {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return time.Time{}, false
		}
		ymd[i] = n
	}
	if ymd[1] < 1 || ymd[1] > 12 || ymd[2] < 1 || ymd[2] > 31 {
		return time.Time{}, false
	}
	return time.Date(ymd[0], time.Month(ymd[1]), ymd[2], 0, 0, 0, 0, time.UTC), true
}

// describeAge writes a version's age for people.
func describeAge(age time.Duration) string {
	switch days := int(age.Hours() / 24); {
	case days <= 0:
		return "today"
	case days == 1:
		return "1 day old"
	default:
		return fmt.Sprintf("%d days old", days)
	}
}

// versionAfter returns the word after prefix on the output's first line
// ("ffmpeg version 7.1.5-0+deb13u1 Copyright…" gives "7.1.5-0+deb13u1").
func versionAfter(out, prefix string) string {
	rest, ok := strings.CutPrefix(firstLine(out), prefix)
	if !ok {
		return ""
	}
	if i := strings.IndexByte(rest, ' '); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}
