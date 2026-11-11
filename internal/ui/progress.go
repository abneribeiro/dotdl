package ui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/abneribeiro/dotdl/internal/jobs"
)

// RunFunc is a job to run while showing its progress.
type RunFunc func(ctx context.Context, report jobs.Reporter) (jobs.Result, error)

// Messages from the job to the view.
type (
	eventMsg struct{ event jobs.Event }
	doneMsg  struct{}
)

// pulseWidth is the size of the moving block on an indeterminate bar.
const pulseWidth = 8

// maxBarWidth keeps the bar readable on very wide terminals.
const maxBarWidth = 48

// progressModel is the Bubble Tea view of a running download.
type progressModel struct {
	title  string
	cancel context.CancelFunc
	width  int

	stage    jobs.Stage
	fraction float64
	speed    float64
	eta      time.Duration
	item     string
	index    int
	count    int
	note     string
	warning  bool

	spinner   spinner.Model
	bar       progress.Model
	ticks     int
	canceling bool
	done      bool
}

func newProgressModel(title string, cancel context.CancelFunc) *progressModel {
	return &progressModel{
		title:    title,
		cancel:   cancel,
		width:    80,
		stage:    jobs.StageInfo,
		fraction: -1,
		eta:      -1,
		spinner:  spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(accentStyle)),
		bar:      newBar(),
	}
}

// newBar is the determinate bar: solid coral over a dark track.
func newBar() progress.Model {
	bar := progress.New(
		progress.WithColors(colorAccent),
		progress.WithFillCharacters('█', '░'),
		progress.WithoutPercentage(),
	)
	bar.EmptyColor = colorTrack
	return bar
}

func (m *progressModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m *progressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" && !m.canceling {
			// The context interrupts yt-dlp and ffmpeg.
			m.canceling = true
			m.cancel()
		}
	case eventMsg:
		m.apply(msg.event)
	case doneMsg:
		m.done = true
		return m, tea.Quit
	case spinner.TickMsg:
		m.ticks++
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

// apply folds an event into the view's state.
func (m *progressModel) apply(e jobs.Event) {
	if e.Stage != "" && e.Stage != m.stage {
		m.stage = e.Stage
		// A new stage starts without known progress, until the first update.
		m.fraction, m.speed, m.eta = -1, 0, -1
	}
	if e.Progress {
		m.fraction, m.speed, m.eta = e.Fraction, e.Speed, e.ETA
	}
	if e.Item != "" {
		m.item = e.Item
	}
	if e.Count > 0 {
		m.index, m.count = e.Index, e.Count
	}
	if note, warning := noteFrom(e); note != "" {
		m.note, m.warning = note, warning
	}
}

// noteFrom returns what an event has worth showing under the bar: a yt-dlp
// warning or a note from dotdl itself. The rest of yt-dlp's chatter is
// hidden.
func noteFrom(e jobs.Event) (note string, warning bool) {
	if text, ok := strings.CutPrefix(e.Log, "WARNING:"); ok {
		return strings.TrimSpace(text), true
	}
	return e.Note, false
}

func (m *progressModel) View() tea.View {
	if m.done {
		return tea.NewView("")
	}
	return tea.NewView(m.render())
}

// render draws the view: a header, the stage with the current item, the bar
// with its numbers, and the latest note.
func (m *progressModel) render() string {
	width := max(m.width-2, 20)
	var b strings.Builder

	header := badge() + " " + mutedStyle.Render(m.title)
	if m.count > 1 {
		pos := mutedStyle.Render(fmt.Sprintf("%d of %d", m.index, m.count))
		gap := width - lipgloss.Width(header) - lipgloss.Width(pos)
		header += strings.Repeat(" ", max(gap, 1)) + pos
	}
	b.WriteString(header + "\n")

	stage := string(m.stage)
	if m.canceling {
		stage = "Canceling"
	}
	line := m.spinner.View() + " " + strongStyle.Render(stage)
	if m.item != "" {
		line += "  " + mutedStyle.Render(m.item)
	}
	b.WriteString(truncate(line, width) + "\n")

	stats := m.stats()
	barWidth := min(width-lipgloss.Width(stats)-2, maxBarWidth)
	b.WriteString(m.renderBar(max(barWidth, 10)) + "  " + stats)

	if m.note != "" {
		note := mutedStyle.Render(iconNote + " " + m.note)
		if m.warning {
			note = warnStyle.Render(iconWarn + " " + m.note)
		}
		b.WriteString("\n" + truncate(note, width))
	}
	return b.String()
}

// stats is the text after the bar: percentage, speed and time left.
func (m *progressModel) stats() string {
	var parts []string
	if m.fraction >= 0 {
		parts = append(parts, accentBoldStyle.Render(fmt.Sprintf("%3.0f%%", m.fraction*100)))
	}
	if m.speed > 0 {
		parts = append(parts, mutedStyle.Render(humanBytes(m.speed)+"/s"))
	}
	if left := remaining(m.eta); left != "" && m.speed > 0 {
		parts = append(parts, mutedStyle.Render(left+" left"))
	}
	return strings.Join(parts, "  ")
}

// renderBar draws the progress bar, or a block sweeping across the track
// when the stage has no known progress (merging, extracting).
func (m *progressModel) renderBar(width int) string {
	if m.fraction >= 0 {
		m.bar.SetWidth(width)
		return m.bar.ViewAs(m.fraction)
	}
	// The pulse enters from the left and leaves on the right; the visible
	// part is [start, end).
	pos := m.ticks%(width+pulseWidth) - pulseWidth
	start, end := max(pos, 0), min(pos+pulseWidth, width)
	return trackStyle.Render(strings.Repeat("░", start)) +
		accentStyle.Render(strings.Repeat("█", max(end-start, 0))) +
		trackStyle.Render(strings.Repeat("░", width-max(end, start)))
}

// RunWithProgress runs the job while drawing its progress on out, a
// terminal. Ctrl+C cancels the job (it still has to finish cleanly) instead
// of killing the program.
func RunWithProgress(ctx context.Context, out io.Writer, title string, run RunFunc) (jobs.Result, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	m := newProgressModel(title, cancel)
	// Signals belong to main: the program only handles Ctrl+C as a key.
	p := tea.NewProgram(m, tea.WithOutput(out), tea.WithoutSignalHandler())

	type outcome struct {
		res jobs.Result
		err error
	}
	finished := make(chan outcome, 1)
	go func() {
		res, err := run(ctx, func(e jobs.Event) { p.Send(eventMsg{e}) })
		finished <- outcome{res, err}
		p.Send(doneMsg{})
	}()

	if _, err := p.Run(); err != nil {
		// The terminal went away: stop the job and wait for it anyway, so
		// no yt-dlp or ffmpeg is left running.
		cancel()
	}
	o := <-finished
	return o.res, o.err
}

// RunPlain runs the job writing plain lines to out: stage changes, items
// and notes, without colors or redrawing. It is the output for pipes, cron
// and containers without a terminal.
func RunPlain(ctx context.Context, out io.Writer, run RunFunc) (jobs.Result, error) {
	var stage jobs.Stage
	var item string
	return run(ctx, func(e jobs.Event) {
		if e.Stage != "" && e.Stage != stage || e.Item != "" && e.Item != item {
			if e.Stage != "" {
				stage = e.Stage
			}
			if e.Item != "" {
				item = e.Item
			}
			line := string(stage)
			if item != "" {
				line += ": " + item
			}
			fmt.Fprintln(out, line)
		}
		if note, warning := noteFrom(e); note != "" {
			if warning {
				note = "warning: " + note
			}
			fmt.Fprintln(out, "  "+note)
		}
	})
}
