package ui

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// TUIConfig holds the configuration needed by the dashboard.
type TUIConfig struct {
	Backend     string
	Model       string
	Concurrency int
	Checklist   string
}

// tuiStyles holds cached lipgloss styles to avoid recreating them every render.
type tuiStyles struct {
	title  lipgloss.Style
	header lipgloss.Style
	green  lipgloss.Style
	yellow lipgloss.Style
	blue   lipgloss.Style
	dim    lipgloss.Style
}

func newTUIStyles() tuiStyles {
	return tuiStyles{
		title:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")),
		header: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5")),
		green:  lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		yellow: lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		blue:   lipgloss.NewStyle().Foreground(lipgloss.Color("4")),
		dim:    lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
	}
}

// InlineDashboard renders a fixed-height status block at the bottom of the
// terminal, rclone-style. Log output from workers and agents flows to stdout
// normally (scrolling up), and the dashboard is periodically overwritten in
// place using ANSI cursor movement.
type InlineDashboard struct {
	mu         sync.Mutex
	config     TUIConfig
	getWorkers func() []WorkerInfo
	getUsage   func() float64
	startTime  time.Time
	lastLines  int // number of lines rendered in the last frame
	styles     tuiStyles
	stopped    bool
	stopCh     chan struct{}
	doneCh     chan struct{}
}

// NewInlineDashboard creates a new inline dashboard.
func NewInlineDashboard(cfg TUIConfig, getWorkers func() []WorkerInfo, getUsage func() float64) *InlineDashboard {
	return &InlineDashboard{
		config:     cfg,
		getWorkers: getWorkers,
		getUsage:   getUsage,
		startTime:  time.Now(),
		styles:     newTUIStyles(),
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
	}
}

// Run starts the dashboard refresh loop. It blocks until Stop() is called.
// Refreshes every 500ms.
func (d *InlineDashboard) Run() {
	defer close(d.doneCh)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			d.render()
		case <-d.stopCh:
			// Final render to show completed state, then clear the dashboard
			d.render()
			return
		}
	}
}

// Stop signals the dashboard to stop refreshing and cleans up.
func (d *InlineDashboard) Stop() {
	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return
	}
	d.stopped = true
	d.mu.Unlock()
	close(d.stopCh)
	<-d.doneCh

	// Clear the dashboard from terminal so final summary prints cleanly
	d.clearLastRender()
}

// clearLastRender moves cursor up and clears the lines from the last render.
func (d *InlineDashboard) clearLastRender() {
	if d.lastLines > 0 {
		// Move cursor up N lines and clear to end of screen
		fmt.Fprintf(os.Stdout, "\033[%dA\033[J", d.lastLines)
		d.lastLines = 0
	}
}

// render draws the dashboard, overwriting the previous frame.
func (d *InlineDashboard) render() {
	d.mu.Lock()
	defer d.mu.Unlock()

	workers := d.getWorkers()
	totalCost := d.getUsage()
	s := d.styles

	var b strings.Builder

	// Header
	b.WriteString(s.title.Render("  RALPH") + " — Parallel Coding Agent Runner\n")
	b.WriteString(s.dim.Render(fmt.Sprintf("  %s | %s | elapsed: %s",
		d.config.Backend, d.config.Model, formatDuration(time.Since(d.startTime)))))
	b.WriteString("\n")

	// Separator
	b.WriteString("  " + strings.Repeat("─", 55) + "\n")

	// Worker rows
	workerColorStyles := []tuiStyleEntry{
		{s.yellow}, {s.blue}, {s.green}, {s.header},
	}
	for i, w := range workers {
		clr := workerColorStyles[i%len(workerColorStyles)].style
		label := clr.Render(fmt.Sprintf("  W%d %-8s", w.Num, strings.ToUpper(w.Name)))

		// Status indicator
		var statusIcon string
		switch {
		case strings.HasPrefix(w.Status, "running"):
			statusIcon = s.green.Render("● ")
		case w.Status == "done":
			statusIcon = s.green.Render("✓ ")
		case w.Status == "stalled":
			statusIcon = s.yellow.Render("⚠ ")
		case w.Status == "cooling down":
			statusIcon = s.dim.Render("◌ ")
		case w.Status == "committing":
			statusIcon = s.blue.Render("↑ ")
		case strings.HasPrefix(w.Status, "retry"):
			statusIcon = s.yellow.Render("↻ ")
		default:
			statusIcon = s.dim.Render("○ ")
		}

		remaining := s.dim.Render(fmt.Sprintf("%3d remaining", w.Remaining))
		statusText := w.Status
		if childLabel := workerChildPoolLabel(w); childLabel != "" {
			statusText += " | " + childLabel
		}
		if generationLabel := workerGenerationLabel(w); generationLabel != "" {
			statusText += " | " + generationLabel
		}
		status := s.dim.Render(statusText)

		b.WriteString(fmt.Sprintf("%s %s %s | %s\n", label, statusIcon, remaining, status))
	}

	// Bottom separator
	b.WriteString("  " + strings.Repeat("─", 55) + "\n")

	// Footer
	var footer strings.Builder
	if totalCost > 0 {
		footer.WriteString(s.dim.Render(fmt.Sprintf("  Cost: $%.2f  ", totalCost)))
	}
	footer.WriteString(s.dim.Render("  [Ctrl+C] quit"))
	b.WriteString(footer.String())
	// No trailing newline — keeps cursor at end of last line

	output := b.String()
	lineCount := strings.Count(output, "\n") + 1

	// Move cursor up to overwrite previous frame
	if d.lastLines > 0 {
		fmt.Fprintf(os.Stdout, "\033[%dA\033[J", d.lastLines)
	}

	fmt.Fprint(os.Stdout, output)
	d.lastLines = lineCount
}

// tuiStyleEntry wraps a lipgloss style for use in a slice.
type tuiStyleEntry struct {
	style lipgloss.Style
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm%02ds", h, m, s)
	}
	return fmt.Sprintf("%dm%02ds", m, s)
}
