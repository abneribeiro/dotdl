// Package config loads dotdl's configuration from environment variables,
// with sensible defaults. Invalid values are errors, never a silent
// fallback.
//
// Variables can come from the environment, from a .env in the current folder
// or from the global ~/.config/dotdl/config.env (see LoadFiles), in that
// order of priority.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds dotdl's settings.
type Config struct {
	// OutputDir is the base output folder (DOTDL_OUTPUT_DIR): used when -o
	// is not given, and the root of a relative -o. An absolute -o ignores it.
	// Empty means the current folder.
	OutputDir string

	// TempDir holds the downloads' temporary files (DOTDL_TEMP_DIR, default
	// os.TempDir()).
	TempDir string

	// LogLevel controls the verbosity (DOTDL_LOG_LEVEL: debug|info|warn|error).
	LogLevel slog.Level

	// CookiesFile is a Netscape-format cookies file (DOTDL_COOKIES_FILE),
	// passed to yt-dlp with --cookies. It gets past YouTube's "confirm you're
	// not a bot" check, age restrictions and content behind a login. It must
	// exist when set.
	CookiesFile string

	// JSRuntimes is passed to yt-dlp as --js-runtimes (DOTDL_JS_RUNTIMES,
	// e.g. "node"). Empty uses yt-dlp's default, Deno; the download commands
	// pick "node" when Deno is missing and node is there (cli.pickJSRuntime).
	JSRuntimes string
}

// Environment variable names.
const (
	KeyOutputDir   = "DOTDL_OUTPUT_DIR"
	KeyTempDir     = "DOTDL_TEMP_DIR"
	KeyLogLevel    = "DOTDL_LOG_LEVEL"
	KeyCookiesFile = "DOTDL_COOKIES_FILE"
	KeyJSRuntimes  = "DOTDL_JS_RUNTIMES"
)

// FilePath returns the global configuration file:
// $XDG_CONFIG_HOME/dotdl/config.env, or ~/.config/dotdl/config.env.
func FilePath() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "dotdl", "config.env")
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "dotdl", "config.env")
	}
	return filepath.Join(os.TempDir(), "dotdl", "config.env")
}

// LoadFiles loads ./.env and then the global file into the environment.
// godotenv never overrides a variable that is already set, so the priority
// is: environment > ./.env > config.env. Both are optional.
func LoadFiles() {
	_ = godotenv.Load(".env")     // optional: only exists in a project
	_ = godotenv.Load(FilePath()) // optional: only exists once something was configured
}

// Load reads the configuration from the process environment.
func Load() (*Config, error) {
	return loadFrom(os.Getenv)
}

// loadFrom is the testable implementation of Load, with the environment
// injected.
func loadFrom(getenv func(string) string) (*Config, error) {
	cfg := &Config{
		OutputDir:   getenv(KeyOutputDir),
		TempDir:     os.TempDir(),
		LogLevel:    slog.LevelInfo,
		JSRuntimes:  getenv(KeyJSRuntimes),
		CookiesFile: getenv(KeyCookiesFile),
	}

	if v := getenv(KeyTempDir); v != "" {
		cfg.TempDir = v
	}

	if v := getenv(KeyLogLevel); v != "" {
		switch strings.ToLower(v) {
		case "debug":
			cfg.LogLevel = slog.LevelDebug
		case "info":
			cfg.LogLevel = slog.LevelInfo
		case "warn", "warning":
			cfg.LogLevel = slog.LevelWarn
		case "error":
			cfg.LogLevel = slog.LevelError
		default:
			return nil, fmt.Errorf("invalid %s %q (use debug, info, warn or error)", KeyLogLevel, v)
		}
	}

	// The shell only expands ~ on the command line; in a .env it arrived
	// literally and created a folder named "~".
	for _, p := range []*string{&cfg.OutputDir, &cfg.TempDir, &cfg.CookiesFile} {
		expanded, err := ExpandHome(*p, getenv)
		if err != nil {
			return nil, err
		}
		*p = expanded
	}

	if cfg.CookiesFile != "" {
		if err := CheckCookiesFile(cfg.CookiesFile); err != nil {
			return nil, fmt.Errorf("invalid %s: %w", KeyCookiesFile, err)
		}
	}

	return cfg, nil
}

// CheckCookiesFile checks that path is an existing file.
func CheckCookiesFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a file", path)
	}
	return nil
}

// ExpandHome expands a leading "~" using $HOME.
func ExpandHome(path string, getenv func(string) string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home := getenv("HOME")
	if home == "" {
		var err error
		if home, err = os.UserHomeDir(); err != nil {
			return "", fmt.Errorf("cannot expand %q: %w", path, err)
		}
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
}

// Save writes values (key to value) to path in .env format. It keeps the
// other lines and comments and replaces only the given keys; a key with an
// empty value is removed. It creates the folder and leaves the file at 0600
// (it may point to cookies, which are credentials).
func Save(path string, values map[string]string) error {
	var lines []string
	if f, err := os.Open(path); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		f.Close()
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	done := make(map[string]bool, len(values))
	var out []string
	for _, line := range lines {
		key, _, ok := strings.Cut(strings.TrimSpace(line), "=")
		value, wanted := values[key]
		if !ok || !wanted {
			out = append(out, line)
			continue
		}
		done[key] = true
		if value != "" {
			out = append(out, key+"="+quote(value))
		}
	}

	for _, k := range slices.Sorted(maps.Keys(values)) {
		if !done[k] && values[k] != "" {
			out = append(out, k+"="+quote(values[k]))
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("cannot create %s: %w", filepath.Dir(path), err)
	}
	tmp := path + ".tmp"
	content := strings.Join(out, "\n")
	if content != "" {
		content += "\n"
	}
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp) // the old file is still in place; the copy is useless
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	return nil
}

// quote wraps a value with spaces or special characters in quotes, so
// godotenv reads it back as is (e.g. "/home/ana/My Videos").
func quote(v string) string {
	if strings.ContainsAny(v, " \t#\"'\\$") {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`
	}
	return v
}
