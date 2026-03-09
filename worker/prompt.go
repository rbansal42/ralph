package worker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BuildPrompt reads the base prompt template, appends iteration-specific context,
// writes to a deterministic file in logDir, and returns the file path.
// When useSubagents is true and there are multiple items, a hint is added
// instructing the agent to use subagents for parallel execution of independent items.
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
	partialItems []ChecklistItem,
	useSubagents bool,
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

	// Partial completion warnings.
	if len(partialItems) > 0 {
		b.WriteString("\n### PARTIAL COMPLETIONS DETECTED\n")
		b.WriteString("The following items have already been modified in a previous run but are still marked as [~].\n")
		b.WriteString("Check existing changes before re-implementing from scratch:\n\n")
		for _, item := range partialItems {
			b.WriteString(fmt.Sprintf("  - %s (file already modified)\n", item.Path))
		}
		b.WriteString("\n")
	}

	// Subagent parallelism hint.
	if useSubagents && len(items) > 1 {
		b.WriteString("\n### PARALLEL EXECUTION\n")
		b.WriteString("You have multiple independent items. Use subagents (Task tool) to work on ")
		b.WriteString("items that touch different files in parallel. Each subagent should handle one ")
		b.WriteString("item completely — read the file, implement the change, and return a result summary.\n")
		b.WriteString("Only process items sequentially if they share dependencies or modify the same file.\n")
	}

	// Instructions.
	b.WriteString("\nWork on these items now. There is NO time limit — take as long as needed.\n")
	b.WriteString("\nFor each item:\n")
	b.WriteString("1. Read the ENTIRE source file\n")
	b.WriteString("2. Read any existing equivalent\n")
	b.WriteString("3. Implement COMPLETE business logic\n")
	b.WriteString("4. Do NOT update the checklist directly. The parent worker will reconcile results.\n")
	b.WriteString("\n**DO NOT run git add or git commit.** The runner handles commits externally.\n")
	b.WriteString("\nOutput RALPH_CHILD_RESULT as one JSON object on a single line.\n")
	b.WriteString("Include keys: attempted, completed, files_changed, checklist_lines, failure_reason.\n")
	b.WriteString("Set failure_reason to an empty string when the task succeeds.\n")

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
