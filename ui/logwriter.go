package ui

import (
	"fmt"
	"os"
	"sync"
)

// LogWriter synchronizes log output with the TUI dashboard.
// All stdout writes during TUI mode go through this writer. Before writing,
// it clears the dashboard; after writing, it re-renders the dashboard.
// This prevents ANSI cursor corruption from interleaved writes.
type LogWriter struct {
	mu        sync.Mutex
	dashboard *InlineDashboard
}

// NewLogWriter creates a LogWriter. Use SetDashboard to activate
// dashboard coordination. Without a dashboard, writes pass through to os.Stdout.
func NewLogWriter() *LogWriter {
	return &LogWriter{}
}

// Write clears the dashboard, writes p to os.Stdout, and re-renders the dashboard.
// When no dashboard is active, writes directly to os.Stdout.
func (lw *LogWriter) Write(p []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	if lw.dashboard != nil && lw.dashboard.lastLines > 0 {
		// Clear dashboard before writing log content.
		fmt.Fprintf(os.Stdout, "\033[%dA\033[J", lw.dashboard.lastLines)
		lw.dashboard.lastLines = 0
	}
	n, err := os.Stdout.Write(p)
	if lw.dashboard != nil {
		lw.dashboard.renderUnsafe()
	}
	return n, err
}
