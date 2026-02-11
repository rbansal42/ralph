package worker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BuildPrompt reads the base prompt template, appends iteration-specific context,
// writes to a deterministic file in logDir, and returns the file path.
func BuildPrompt(
	basePromptPath string,
	workerName string,
	workerNum int,
	totalWorkers int,
	pattern string,
	iteration int,
	items []ChecklistItem,
	totalRemaining int,
	logDir string,
) (string, error) {
	base, err := os.ReadFile(basePromptPath)
	if err != nil {
		return "", fmt.Errorf("reading base prompt %q: %w", basePromptPath, err)
	}

	var b strings.Builder

	// Base prompt template contents.
	b.Write(base)

	// Separator and iteration header.
	b.WriteString("\n\n---\n\n")
	b.WriteString(fmt.Sprintf("## THIS IS ITERATION #%d — WORKER %d (%s)\n\n",
		iteration, workerNum, strings.ToUpper(workerName)))

	b.WriteString(fmt.Sprintf(
		"You are Worker %d of %d parallel workers. You ONLY work on **%s** items.\n",
		workerNum, totalWorkers, strings.ToUpper(workerName)))
	b.WriteString(fmt.Sprintf(
		"There are **%d** items remaining in your section.\n", totalRemaining))

	// Scope restriction.
	b.WriteString("\n### IMPORTANT: SCOPE RESTRICTION\n")
	b.WriteString("You MUST only work on the items listed below. Do NOT touch items outside your section.\n")
	b.WriteString("Other workers are handling other sections simultaneously.\n")

	// Item list.
	b.WriteString("\n### Your items for this iteration:\n\n")
	for _, item := range items {
		b.WriteString(item.Line)
		b.WriteString("\n")
	}

	// Instructions.
	b.WriteString("\nWork on these items now. There is NO time limit — take as long as needed.\n")
	b.WriteString("\nFor each item:\n")
	b.WriteString("1. Read the ENTIRE source file\n")
	b.WriteString("2. Read any existing equivalent\n")
	b.WriteString("3. Implement COMPLETE business logic\n")
	b.WriteString("4. Update the checklist marking completed items as [x]\n")
	b.WriteString("\n**DO NOT run git add or git commit.** The runner handles commits externally.\n")
	b.WriteString("\nOutput RALPH_SUMMARY at the end.\n")

	// Write to deterministic path for debugging.
	outPath := filepath.Join(logDir, fmt.Sprintf("w%d_prompt_%04d.md", workerNum, iteration))
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return "", fmt.Errorf("creating log dir %q: %w", logDir, err)
	}
	if err := os.WriteFile(outPath, []byte(b.String()), 0o644); err != nil {
		return "", fmt.Errorf("writing prompt file %q: %w", outPath, err)
	}

	return outPath, nil
}
