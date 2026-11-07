// Package ui is dotdl's look: a coral and green Lip Gloss theme, the
// Bubble Tea progress view of a download, and the printers for help,
// results, errors, the doctor report and the settings. Printed output goes
// through lipgloss.Fprint, which downgrades colors to what the terminal
// supports and drops them when the output is not a terminal.
package ui

import "charm.land/lipgloss/v2"

// Palette, made for dark terminals: coral is the accent (bars, numbers,
// headings, errors), green marks success and amber warnings. Text is
// near-white, secondary text gray.
var (
	colorAccent = lipgloss.Color("#FF8771")
	colorGreen  = lipgloss.Color("#56D58E")
	colorAmber  = lipgloss.Color("#F2C46D")
	colorText   = lipgloss.Color("#F0F0F2")
	colorMuted  = lipgloss.Color("#7C7C80")
	colorTrack  = lipgloss.Color("#383838")
	colorInk    = lipgloss.Color("#131313")
)

var (
	badgeStyle   = lipgloss.NewStyle().Bold(true).Foreground(colorInk).Background(colorAccent).Padding(0, 1)
	accentStyle  = lipgloss.NewStyle().Foreground(colorAccent)
	numberStyle  = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	strongStyle  = lipgloss.NewStyle().Bold(true).Foreground(colorText)
	headingStyle = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	mutedStyle   = lipgloss.NewStyle().Foreground(colorMuted)
	okStyle      = lipgloss.NewStyle().Foreground(colorGreen)
	warnStyle    = lipgloss.NewStyle().Foreground(colorAmber)
	errStyle     = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	trackStyle   = lipgloss.NewStyle().Foreground(colorTrack)
)

// Status icons.
const (
	iconOK   = "✓"
	iconWarn = "!"
	iconFail = "✗"
	iconNote = "›"
)

// badge renders the coral "dotdl" tag that opens every screen.
func badge() string {
	return badgeStyle.Render("dotdl")
}
