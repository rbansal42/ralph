package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

var (
	genChecklistGlobs    []string
	genChecklistExcludes []string
	genChecklistTemplate string
	genChecklistOutput   string
	genChecklistHeader   string
	genChecklistAppend   bool
)

// genChecklistRe matches checklist lines like "- [x] path/to/file — note"
var genChecklistRe = regexp.MustCompile(`^- \[([xs~])\]\s+(\S+?)(?:\s+—\s+(.*))?$`)

var generateChecklistCmd = &cobra.Command{
	Use:   "checklist",
	Short: "Generate a checklist file from glob patterns",
	Long: `Generates a checklist file by matching files against glob patterns.

Examples:
  ralph generate checklist --glob "app/Models/**/*.php" --template "- [~] {path} — add typed properties"
  ralph generate checklist --glob "src/**/*.ts" --exclude "src/**/*.test.ts" --output CHECKLIST.md
  ralph generate checklist --glob "app/**/*.php" --append --output CHECKLIST.md`,
	RunE: runGenerateChecklist,
}

func init() {
	generateChecklistCmd.Flags().StringArrayVar(&genChecklistGlobs, "glob", nil, "glob pattern(s) to match files (required, can be repeated)")
	generateChecklistCmd.Flags().StringArrayVar(&genChecklistExcludes, "exclude", nil, "glob pattern(s) to exclude (can be repeated)")
	generateChecklistCmd.Flags().StringVar(&genChecklistTemplate, "template", "- [~] {path}", "template for each checklist item ({path}, {name}, {dir} placeholders)")
	generateChecklistCmd.Flags().StringVar(&genChecklistOutput, "output", "", "output file (default: stdout)")
	generateChecklistCmd.Flags().StringVar(&genChecklistHeader, "header", "", "header line to add at top of checklist")
	generateChecklistCmd.Flags().BoolVar(&genChecklistAppend, "append", false, "append to existing file instead of overwriting")
	generateChecklistCmd.MarkFlagRequired("glob")

	generateCmd.AddCommand(generateChecklistCmd)
}

func runGenerateChecklist(cmd *cobra.Command, args []string) error {
	// Collect matching files from all glob patterns
	matchSet := make(map[string]bool)
	for _, pattern := range genChecklistGlobs {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return fmt.Errorf("invalid glob pattern %q: %w", pattern, err)
		}
		for _, m := range matches {
			// Only include files, not directories
			info, err := os.Stat(m)
			if err != nil {
				continue
			}
			if info.IsDir() {
				continue
			}
			matchSet[m] = true
		}
	}

	// Remove excluded files
	for _, pattern := range genChecklistExcludes {
		excludes, err := filepath.Glob(pattern)
		if err != nil {
			return fmt.Errorf("invalid exclude pattern %q: %w", pattern, err)
		}
		for _, e := range excludes {
			delete(matchSet, e)
		}
	}

	// If appending, load existing paths to deduplicate
	existingPaths := make(map[string]bool)
	if genChecklistAppend && genChecklistOutput != "" {
		if f, err := os.Open(genChecklistOutput); err == nil {
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				line := scanner.Text()
				// Extract path from checklist line using the same regex as worker/checklist.go
				if m := genChecklistRe.FindStringSubmatch(line); m != nil {
					existingPaths[m[2]] = true
				}
			}
			f.Close()
		}
	}

	// Sort matched files
	paths := make([]string, 0, len(matchSet))
	for p := range matchSet {
		if !existingPaths[p] {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)

	if len(paths) == 0 {
		fmt.Fprintln(os.Stderr, "No matching files found.")
		return nil
	}

	// Generate checklist lines
	var lines []string
	for _, p := range paths {
		line := genChecklistTemplate
		line = strings.ReplaceAll(line, "{path}", p)
		line = strings.ReplaceAll(line, "{name}", filepath.Base(p))
		line = strings.ReplaceAll(line, "{dir}", filepath.Dir(p))
		lines = append(lines, line)
	}

	// Write output
	var w *os.File
	if genChecklistOutput == "" {
		w = os.Stdout
	} else {
		flag := os.O_WRONLY | os.O_CREATE
		if genChecklistAppend {
			flag |= os.O_APPEND
		} else {
			flag |= os.O_TRUNC
		}
		var err error
		w, err = os.OpenFile(genChecklistOutput, flag, 0644)
		if err != nil {
			return fmt.Errorf("opening output file: %w", err)
		}
		defer w.Close()
	}

	// Write header if not appending and header is set
	if genChecklistHeader != "" && !genChecklistAppend {
		fmt.Fprintln(w, genChecklistHeader)
		fmt.Fprintln(w)
	}

	// Add newline separator before appended items
	if genChecklistAppend {
		fmt.Fprintln(w)
	}

	for _, line := range lines {
		fmt.Fprintln(w, line)
	}

	if genChecklistOutput != "" {
		fmt.Fprintf(os.Stderr, "Generated %d checklist items", len(lines))
		if genChecklistAppend {
			fmt.Fprintf(os.Stderr, " (appended to %s)\n", genChecklistOutput)
		} else {
			fmt.Fprintf(os.Stderr, " → %s\n", genChecklistOutput)
		}
	}

	return nil
}
