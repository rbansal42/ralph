package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/rahulvramesh/ralph/worker"
)

// ANSI color codes.
const (
	colorReset   = "\033[0m"
	colorGreen   = "\033[32m"
	colorYellow  = "\033[33m"
	colorBlue    = "\033[34m"
	colorMagenta = "\033[35m"
	colorCyan    = "\033[36m"
	colorBold    = "\033[1m"
)

// workerColors cycles through colors for each worker line.
var workerColors = []string{colorYellow, colorCyan, colorMagenta, colorGreen, colorBlue}

// WorkerInfo holds display info for one worker.
type WorkerInfo struct {
	Num         int
	Name        string
	Pattern     string
	Remaining   int
	Status      string // "idle", "running iter #3", "committing", etc.
	LastElapsed string // "7m23s"
	ActiveChildren int
	ChildCapacity  int
}

func workerChildPoolLabel(info WorkerInfo) string {
	if info.ChildCapacity <= 0 {
		return ""
	}
	return fmt.Sprintf("children %d/%d", info.ActiveChildren, info.ChildCapacity)
}

// boxWidth is the inner content width between the left and right border characters.
// The total line width is boxWidth + 2 (for the ║ on each side).
const boxWidth = 51

// PrintBanner prints the Ralph ASCII art header.
func PrintBanner() {
	fmt.Fprintf(os.Stdout, "%s", colorBold)
	fmt.Fprintln(os.Stdout, `  ╱|、`)
	fmt.Fprintf(os.Stdout, " (˚ˎ 。7      %sRALPH%s%s\n", colorCyan, colorReset, colorBold)
	fmt.Fprintf(os.Stdout, "  |、˜〵      %sParallel Coding Agent Runner%s%s\n", colorYellow, colorReset, colorBold)
	fmt.Fprintln(os.Stdout, `  じしˍ,)ノ`)
	fmt.Fprintf(os.Stdout, "%s\n", colorReset)
}

// PrintStatus prints a formatted status box with checklist counts and per-worker status.
func PrintStatus(checklistPath string, workers []WorkerInfo) {
	counts, err := worker.CountByStatus(checklistPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ui: failed to read checklist: %v\n", err)
		return
	}

	matched := counts["x"]
	skipped := counts["s"]
	remaining := counts["~"]
	total := matched + skipped + remaining

	var pct int
	if total > 0 {
		pct = (matched + skipped) * 100 / total
	}

	// Top border
	fmt.Fprintf(os.Stdout, "╔%s╗\n", strings.Repeat("═", boxWidth))

	// Summary lines
	line1 := fmt.Sprintf("  %sMATCH [x]: %-6d%s %sSKIP [s]: %-6d%s",
		colorGreen, matched, colorReset,
		colorBlue, skipped, colorReset)
	printBoxLine(line1, countVisibleLen(line1))

	line2 := fmt.Sprintf("  %sREMAIN[~]: %-6d%s Progress: %d%%",
		colorYellow, remaining, colorReset, pct)
	printBoxLine(line2, countVisibleLen(line2))

	// Middle divider
	fmt.Fprintf(os.Stdout, "╠%s╣\n", strings.Repeat("═", boxWidth))

	// Worker lines
	for i, w := range workers {
		clr := workerColors[i%len(workerColors)]

		// Build the status suffix — prefer LastElapsed if present, otherwise Status.
		statusStr := w.Status
		if w.LastElapsed != "" && statusStr == "" {
			statusStr = w.LastElapsed
		}

		label := fmt.Sprintf("W%d", w.Num)
		name := strings.ToUpper(w.Name)
		// Truncate name to 6 chars for alignment.
		if len(name) > 6 {
			name = name[:6]
		}

		content := fmt.Sprintf("  %s%s %-6s%s: %3d remaining | %s",
			clr, label, name, colorReset, w.Remaining, statusStr)
		if childLabel := workerChildPoolLabel(w); childLabel != "" {
			content += " | " + childLabel
		}
		printBoxLine(content, countVisibleLen(content))
	}

	// Bottom border
	fmt.Fprintf(os.Stdout, "╚%s╝\n", strings.Repeat("═", boxWidth))
}

// printBoxLine prints a single line inside the box, right-padded to fill boxWidth.
// visibleLen is the count of visible (non-ANSI) characters in content.
func printBoxLine(content string, visibleLen int) {
	pad := boxWidth - visibleLen
	if pad < 0 {
		pad = 0
	}
	fmt.Fprintf(os.Stdout, "║%s%s║\n", content, strings.Repeat(" ", pad))
}

// countVisibleLen returns the length of s after stripping ANSI escape sequences.
func countVisibleLen(s string) int {
	inEscape := false
	n := 0
	for _, r := range s {
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		if r == '\033' {
			inEscape = true
			continue
		}
		n++
	}
	return n
}
