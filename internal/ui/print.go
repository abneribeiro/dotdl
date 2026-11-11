package ui

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/abneribeiro/dotdl/internal/doctor"
	"github.com/abneribeiro/dotdl/internal/downloader"
	"github.com/abneribeiro/dotdl/internal/jobs"
)

// Help is a command's help screen.
type Help struct {
	Usage       string
	Description string
	Sections    []HelpSection
	Footer      string
}

// HelpSection is a titled list of name/description rows (commands, flags,
// examples).
type HelpSection struct {
	Title string
	Rows  [][2]string
}

// PrintHelp writes a help screen with aligned columns.
func PrintHelp(w io.Writer, h Help) {
	var b strings.Builder
	b.WriteString(badge() + " " + mutedStyle.Render(h.Description) + "\n\n")
	b.WriteString(accentBoldStyle.Render("Usage") + "\n  " + h.Usage + "\n")
	for _, s := range h.Sections {
		b.WriteString("\n" + accentBoldStyle.Render(s.Title) + "\n")
		width := 0
		for _, r := range s.Rows {
			width = max(width, lipgloss.Width(r[0]))
		}
		for _, r := range s.Rows {
			if r[1] == "" {
				b.WriteString("  " + r[0] + "\n")
				continue
			}
			b.WriteString("  " + padRight(strongStyle.Render(r[0]), width) + "  " + mutedStyle.Render(r[1]) + "\n")
		}
	}
	if h.Footer != "" {
		b.WriteString("\n")
		for line := range strings.SplitSeq(h.Footer, "\n") {
			b.WriteString(mutedStyle.Render(line) + "\n")
		}
	}
	lipgloss.Fprint(w, b.String())
}

// Describer summarizes a downloaded file (see downloader.Describe).
type Describer func(path string) string

// PrintResult writes what a download produced: one line per file with its
// format, then where it all went.
func PrintResult(w io.Writer, res jobs.Result, describe Describer) {
	var b strings.Builder
	if len(res.Files) == 0 {
		b.WriteString(okStyle.Render(iconOK) + " Nothing new to download " +
			mutedStyle.Render("(everything was already in "+res.OutputDir+")") + "\n")
		lipgloss.Fprint(w, b.String())
		return
	}

	names := make([]string, len(res.Files))
	width := 0
	for i, f := range res.Files {
		names[i] = filepath.Base(f)
		width = max(width, lipgloss.Width(names[i]))
	}
	width = min(width, 60)
	for i, f := range res.Files {
		name := truncate(names[i], width)
		info := describe(f)
		if source, ok := res.Converted[f]; ok {
			info += " (converted from " + strings.ToUpper(source) + ")"
		}
		if info != "" {
			name = padRight(name, width) + "  " + mutedStyle.Render(info)
		}
		b.WriteString(okStyle.Render(iconOK) + " " + name + "\n")
	}
	b.WriteString(mutedStyle.Render("Saved to "+res.OutputDir) + "\n")
	lipgloss.Fprint(w, b.String())
}

// PrintError writes a failure with yt-dlp's reason and a hint when there is
// one. A partial result is a warning, since the rest was saved.
func PrintError(w io.Writer, err error) {
	var b strings.Builder
	if errors.Is(err, jobs.ErrPartial) {
		b.WriteString(warnStyle.Render(iconWarn+" Some items failed: ") + strings.TrimPrefix(err.Error(),
			jobs.ErrPartial.Error()+": ") + "\n")
	} else {
		b.WriteString(accentBoldStyle.Render(iconFail+" ") + err.Error() + "\n")
	}
	var dlErr *downloader.DownloadError
	if errors.As(err, &dlErr) {
		if hint := dlErr.Hint(); hint != "" {
			b.WriteString(mutedStyle.Render("  "+hint) + "\n")
		}
	}
	lipgloss.Fprint(w, b.String())
}

// PrintCanceled tells the user the download stopped and how to resume it.
func PrintCanceled(w io.Writer) {
	lipgloss.Fprint(w, warnStyle.Render(iconWarn+" Canceled.")+" "+
		mutedStyle.Render("Run the same command again to resume where it stopped.")+"\n")
}

// PrintDoctor writes the environment report.
func PrintDoctor(w io.Writer, checks []doctor.Check) {
	var b strings.Builder
	b.WriteString(badge() + " " + mutedStyle.Render("doctor") + "\n\n")
	width := 0
	for _, c := range checks {
		width = max(width, lipgloss.Width(c.Name))
	}
	var warnings, failures int
	for _, c := range checks {
		icon := okStyle.Render(iconOK)
		switch c.Level {
		case doctor.LevelWarn:
			icon = warnStyle.Render(iconWarn)
			warnings++
		case doctor.LevelFail:
			icon = accentBoldStyle.Render(iconFail)
			failures++
		}
		b.WriteString("  " + icon + " " + padRight(strongStyle.Render(c.Name), width) + "  " + c.Detail + "\n")
		if c.Fix != "" {
			b.WriteString("    " + strings.Repeat(" ", width+2) + mutedStyle.Render("→ "+c.Fix) + "\n")
		}
	}
	b.WriteString("\n")
	switch {
	case failures > 0:
		b.WriteString(accentBoldStyle.Render(plural(failures, "problem", "problems")+" to fix.") + "\n")
	case warnings > 0:
		b.WriteString(okStyle.Render("Ready") + mutedStyle.Render(" ("+plural(warnings, "warning", "warnings")+")") + "\n")
	default:
		b.WriteString(okStyle.Render("All good.") + "\n")
	}
	lipgloss.Fprint(w, b.String())
}

// Setting is one row of the settings screen.
type Setting struct {
	Key, Value, Description string
}

// PrintConfig writes the settings and where they are stored.
func PrintConfig(w io.Writer, path string, settings []Setting) {
	var b strings.Builder
	b.WriteString(badge() + " " + mutedStyle.Render("config") + "\n\n")
	width := 0
	for _, s := range settings {
		width = max(width, lipgloss.Width(s.Key))
	}
	for _, s := range settings {
		value := s.Value
		if value == "" {
			value = mutedStyle.Render("(not set)")
		}
		b.WriteString("  " + padRight(strongStyle.Render(s.Key), width) + "  " + value + "\n")
		b.WriteString("  " + strings.Repeat(" ", width+2) + mutedStyle.Render(s.Description) + "\n")
	}
	b.WriteString("\n" + mutedStyle.Render("Stored in "+path+". Change one with: dotdl config <key> <value>") + "\n")
	lipgloss.Fprint(w, b.String())
}

// PrintSaved confirms a settings change.
func PrintSaved(w io.Writer, key, value string) {
	if value == "" {
		lipgloss.Fprint(w, okStyle.Render(iconOK)+" Cleared "+strongStyle.Render(key)+"\n")
		return
	}
	lipgloss.Fprint(w, okStyle.Render(iconOK)+" Set "+strongStyle.Render(key)+" to "+value+"\n")
}

// PrintUsageError writes a usage mistake with a pointer to the help.
func PrintUsageError(w io.Writer, err error, helpCommand string) {
	lipgloss.Fprint(w, accentBoldStyle.Render(iconFail+" ")+err.Error()+"\n"+
		mutedStyle.Render("  See: "+helpCommand)+"\n")
}

// PrintVersion writes the version line.
func PrintVersion(w io.Writer, version string) {
	lipgloss.Fprint(w, badge()+" "+version+"\n")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// padRight pads s with spaces to width display columns, ignoring ANSI codes:
// it is what keeps the columns of every printer aligned.
func padRight(s string, width int) string {
	return s + strings.Repeat(" ", max(width-lipgloss.Width(s), 0))
}

// truncate cuts s to width display columns, ending with "…".
func truncate(s string, width int) string {
	return ansi.Truncate(s, width, "…")
}
