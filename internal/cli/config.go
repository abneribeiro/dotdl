package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/abneribeiro/dotdl/internal/config"
	"github.com/abneribeiro/dotdl/internal/ui"
)

// setting is a value the config command can change.
type setting struct {
	key         string
	env         string
	description string
	// value reads the effective value from the loaded configuration.
	value func(*config.Config) string
	// check validates a new, already expanded, value.
	check func(string) error
}

var settings = []setting{
	{
		key:         "output",
		env:         config.KeyOutputDir,
		description: "Where downloads go when -o is not given (default: the current folder)",
		value:       func(c *config.Config) string { return c.OutputDir },
		check:       func(string) error { return nil },
	},
	{
		key:         "cookies",
		env:         config.KeyCookiesFile,
		description: "Netscape cookies file used by every download (for logins and bot checks)",
		value:       func(c *config.Config) string { return c.CookiesFile },
		check:       config.CheckCookiesFile,
	},
}

var configHelp = ui.Help{
	Usage:       "dotdl config [key] [value]",
	Description: "show or change the settings",
	Sections: []ui.HelpSection{
		{Title: "Keys", Rows: [][2]string{
			{"output <dir>", "Default output folder"},
			{"cookies <file>", "Cookies file for every download"},
		}},
		{Title: "Examples", Rows: [][2]string{
			{"dotdl config", "  show every setting"},
			{"dotdl config output ~/Videos", "  set a value"},
			{`dotdl config cookies ""`, "  clear a value"},
		}},
	},
	Footer: "Environment variables (DOTDL_OUTPUT_DIR, DOTDL_COOKIES_FILE) take priority over the file.",
}

// config runs the config command: no arguments lists the settings, a key
// alone shows one, a key and a value saves it (an empty value clears it).
func (a *App) config(args []string) int {
	switch len(args) {
	case 0:
		rows := make([]ui.Setting, len(settings))
		for i, s := range settings {
			rows[i] = ui.Setting{Key: s.key, Value: s.value(a.Config), Description: s.description}
		}
		ui.PrintConfig(a.Stdout, config.FilePath(), rows)
		return ExitOK
	case 1, 2:
	default:
		ui.PrintUsageError(a.Stderr, errors.New("too many arguments"), "dotdl help config")
		return ExitUsage
	}

	s, ok := findSetting(args[0])
	if !ok {
		ui.PrintUsageError(a.Stderr, fmt.Errorf("unknown setting %q (use output or cookies)", args[0]),
			"dotdl help config")
		return ExitUsage
	}
	if len(args) == 1 {
		fmt.Fprintln(a.Stdout, s.value(a.Config))
		return ExitOK
	}

	value := args[1]
	if value != "" {
		expanded, err := config.ExpandHome(value, os.Getenv)
		if err == nil {
			err = s.check(expanded)
		}
		if err != nil {
			ui.PrintError(a.Stderr, fmt.Errorf("invalid %s: %w", s.key, err))
			return ExitFailure
		}
	}
	if err := config.Save(config.FilePath(), map[string]string{s.env: value}); err != nil {
		ui.PrintError(a.Stderr, err)
		return ExitFailure
	}
	ui.PrintSaved(a.Stdout, s.key, value)
	return ExitOK
}

func findSetting(key string) (setting, bool) {
	for _, s := range settings {
		if s.key == key {
			return s, true
		}
	}
	return setting{}, false
}
