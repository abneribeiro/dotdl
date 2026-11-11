// Package config loads dotdl's configuration from environment variables.
// Invalid values are errors, never a silent fallback.
//
// Variables can come from the environment, from a .env in the current folder
// or from the global config.env (see FilePath and LoadFiles), in that order
// of priority.
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
	"runtime"
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

	// CookiesFile is a Netscape-format cookies file with a browser session
	// (DOTDL_COOKIES_FILE), passed to yt-dlp with --cookies. It gets past
	// YouTube's "confirm you're not a bot" check, age restrictions and
	// content behind a login. It must exist when set.
	CookiesFile string

	// JSRuntimes is passed to yt-dlp as --js-runtimes (DOTDL_JS_RUNTIMES,
	// e.g. "node"): the runtime that solves the JavaScript challenges YouTube
	// protects its formats with. Empty uses yt-dlp's default, Deno; the
	// download commands pick "node" when Deno is missing and node is there
	// (cli.pickJSRuntime).
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

// envPrefix is what every dotdl variable starts with.
const envPrefix = "DOTDL_"

// FilePath returns the global configuration file. It is
// $XDG_CONFIG_HOME/dotdl/config.env or, without the variable,
// ~/.config/dotdl/config.env on Linux and macOS (the same file for the native
// and the Docker install) and %AppData%\dotdl\config.env on Windows.
func FilePath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		if runtime.GOOS == "windows" {
			// Without a profile folder the path stays relative to the current one.
			dir, _ = os.UserConfigDir()
		} else if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, ".config")
		}
	}
	return filepath.Join(dir, "dotdl", "config.env")
}

// LoadFiles loads the DOTDL_* variables of ./.env and then of the global
// file into the environment. A variable that is already set is never
// overridden, so the priority is: environment > ./.env > config.env. Both
// files are optional.
func LoadFiles() error {
	// A ./.env may belong to another project: its syntax is not ours to judge.
	local, _ := godotenv.Read(".env")
	setMissing(local)

	path := FilePath()
	global, err := godotenv.Read(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	setMissing(global)
	return nil
}

// setMissing puts the DOTDL_* values that are not in the environment yet
// into it. The other variables of a .env are not dotdl's to export to
// yt-dlp and ffmpeg.
func setMissing(values map[string]string) {
	for key, value := range values {
		if _, set := os.LookupEnv(key); set || !strings.HasPrefix(key, envPrefix) {
			continue
		}
		_ = os.Setenv(key, value) // only fails for a malformed key, which the prefix rules out
	}
}

// Load reads the configuration from the process environment.
func Load() (*Config, error) {
	return loadFrom(os.Getenv)
}

// loadFrom is Load with the environment injected.
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

	// The shell only expands ~ on the command line: in a .env it is literal.
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
// godotenv reads it back as is (e.g. "/home/ana/My Videos"). The "$" is
// escaped because godotenv expands variables inside double quotes.
func quote(v string) string {
	if strings.ContainsAny(v, " \t#\"'\\$") {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`).Replace(v) + `"`
	}
	return v
}
