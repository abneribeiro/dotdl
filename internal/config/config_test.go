package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/joho/godotenv"
)

// envMap returns a getenv backed by a map, for deterministic tests.
func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadFromDefaults(t *testing.T) {
	cfg, err := loadFrom(envMap(map[string]string{"HOME": "/home/ana"}))
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want info", cfg.LogLevel)
	}
	if cfg.OutputDir != "" || cfg.CookiesFile != "" || cfg.JSRuntimes != "" {
		t.Errorf("output, cookies and js runtime should be empty: %+v", cfg)
	}
}

func TestLoadFromOverrides(t *testing.T) {
	cookies := filepath.Join(t.TempDir(), "cookies.txt")
	if err := os.WriteFile(cookies, []byte("# Netscape HTTP Cookie File\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadFrom(envMap(map[string]string{
		KeyOutputDir:   "/videos",
		KeyTempDir:     "/scratch",
		KeyLogLevel:    "debug",
		KeyCookiesFile: cookies,
		KeyJSRuntimes:  "node",
	}))
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.OutputDir != "/videos" || cfg.TempDir != "/scratch" {
		t.Errorf("paths = %q %q", cfg.OutputDir, cfg.TempDir)
	}
	if cfg.LogLevel != slog.LevelDebug || cfg.CookiesFile != cookies || cfg.JSRuntimes != "node" {
		t.Errorf("config = %+v", cfg)
	}
}

// In a .env the ~ arrives literally: without expansion, dotdl created a
// folder named "~".
func TestLoadFromExpandsTilde(t *testing.T) {
	cfg, err := loadFrom(envMap(map[string]string{
		"HOME":       "/home/ana",
		KeyOutputDir: "~/Videos",
		KeyTempDir:   "~",
	}))
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.OutputDir != "/home/ana/Videos" || cfg.TempDir != "/home/ana" {
		t.Errorf("OutputDir/TempDir = %q/%q", cfg.OutputDir, cfg.TempDir)
	}

	// Only a leading ~ is special.
	cfg, err = loadFrom(envMap(map[string]string{"HOME": "/home/ana", KeyOutputDir: "/data/~backup"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OutputDir != "/data/~backup" {
		t.Errorf("OutputDir = %q, want it unchanged", cfg.OutputDir)
	}
}

func TestLoadFromInvalidValues(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"unknown log level", map[string]string{KeyLogLevel: "verbose"}},
		{"missing cookies", map[string]string{KeyCookiesFile: filepath.Join(dir, "missing.txt")}},
		{"cookies is a folder", map[string]string{KeyCookiesFile: dir}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := loadFrom(envMap(tt.env)); err == nil {
				t.Errorf("loadFrom(%v) should fail", tt.env)
			}
		})
	}
}

func TestSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.env")

	// New file: creates the folder and writes it with 0600.
	if err := Save(path, map[string]string{KeyOutputDir: "/home/ana/My Videos", KeyCookiesFile: "/c"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("permissions = %o, want 600", info.Mode().Perm())
	}

	// Second write: keeps the rest, replaces, and removes on an empty value.
	if err := os.WriteFile(path, []byte("# comment\nOTHER=1\n"+KeyOutputDir+"=/old\n"+KeyCookiesFile+"=/c\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, map[string]string{KeyOutputDir: "/new", KeyCookiesFile: ""}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	got := string(data)
	for _, want := range []string{"# comment", "OTHER=1", KeyOutputDir + "=/new"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, KeyCookiesFile) || strings.Contains(got, "/old") {
		t.Errorf("the empty key should be removed and the old value replaced:\n%s", got)
	}

	// A value with spaces and quotes must read back the same with godotenv.
	if err := Save(path, map[string]string{KeyOutputDir: `/home/ana/My "Best" Videos`}); err != nil {
		t.Fatal(err)
	}
	env, err := godotenv.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if env[KeyOutputDir] != `/home/ana/My "Best" Videos` {
		t.Errorf("read back %q", env[KeyOutputDir])
	}

	// godotenv expands variables: a "$" in a path must survive.
	if err := Save(path, map[string]string{KeyOutputDir: `/mnt/c/$RECYCLE\bin`}); err != nil {
		t.Fatal(err)
	}
	if env, err = godotenv.Read(path); err != nil {
		t.Fatal(err)
	}
	if env[KeyOutputDir] != `/mnt/c/$RECYCLE\bin` {
		t.Errorf("read back %q", env[KeyOutputDir])
	}
}

func TestFilePath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if got := FilePath(); got != "/xdg/dotdl/config.env" {
		t.Errorf("with XDG_CONFIG_HOME: FilePath = %q", got)
	}

	// The docs and the Docker wrapper promise ~/.config on Linux and macOS.
	if runtime.GOOS == "windows" {
		return
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/ana")
	if got := FilePath(); got != "/home/ana/.config/dotdl/config.env" {
		t.Errorf("without XDG_CONFIG_HOME: FilePath = %q", got)
	}
}

func TestLoadFiles(t *testing.T) {
	writeFile := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// t.Setenv restores the variables LoadFiles sets when the test ends.
	for _, key := range []string{KeyOutputDir, KeyTempDir} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
	t.Setenv(KeyLogLevel, "warn")
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Chdir(t.TempDir())

	writeFile(".env", KeyOutputDir+"=/local\n"+KeyLogLevel+"=debug\nFOREIGN_TEST_SECRET=1\n")
	writeFile(FilePath(), KeyOutputDir+"=/global\n"+KeyTempDir+"=/global-tmp\n")
	if err := LoadFiles(); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		KeyLogLevel:  "warn",
		KeyOutputDir: "/local",
		KeyTempDir:   "/global-tmp",
	} {
		if got := os.Getenv(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if _, set := os.LookupEnv("FOREIGN_TEST_SECRET"); set {
		t.Error("a variable without the DOTDL_ prefix must not reach the environment")
	}

	// Another project's .env may not even be a .env; a broken config.env is
	// dotdl's own file and must be reported.
	writeFile(".env", "not a dotenv file {\n")
	if err := LoadFiles(); err != nil {
		t.Errorf("an unreadable ./.env must be ignored: %v", err)
	}
	writeFile(FilePath(), "not a dotenv file {\n")
	if err := LoadFiles(); err == nil {
		t.Error("an unreadable config.env must be an error")
	}
}
