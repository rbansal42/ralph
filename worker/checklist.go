package worker

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// checklistRe matches lines like:
//
//   - [x] path/to/file.php — MATCH: implemented in equivalent.py
//   - [s] path/to/file.php — SKIP: framework boilerplate
//   - [~] path/to/file.php — PARTIAL: needs X, Y, Z
var checklistRe = regexp.MustCompile(`^- \[([xs~])\]\s+(\S+?)(?:\s+—\s+(.*))?$`)

// ChecklistItem represents a single parsed line from a checklist file.
type ChecklistItem struct {
	Line    string // full original line text
	Status  string // "x", "s", or "~"
	Path    string // extracted file path
	Note    string // text after " — " (empty if absent)
	LineNum int    // 1-based line number in file
}

// parseChecklist reads a checklist file and returns all parsed items.
func parseChecklist(path string) ([]ChecklistItem, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening checklist: %w", err)
	}
	defer f.Close()

	var items []ChecklistItem
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		m := checklistRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		items = append(items, ChecklistItem{
			Line:    line,
			Status:  m[1],
			Path:    m[2],
			Note:    m[3],
			LineNum: lineNum,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading checklist: %w", err)
	}
	return items, nil
}

// matchesPattern reports whether line contains any of the pipe-delimited
// segments in pattern. An empty pattern matches everything.
func matchesPattern(line string, pattern string) bool {
	if pattern == "" {
		return true
	}
	for _, seg := range strings.Split(pattern, "|") {
		seg = strings.TrimSpace(seg)
		if seg != "" && strings.Contains(line, seg) {
			return true
		}
	}
	return false
}

// GetPending reads the checklist file and returns all [~] items whose Line
// matches the given pattern. Pattern is pipe-delimited; an item matches if its
// Line contains ANY of the segments.
func GetPending(checklistPath string, pattern string) ([]ChecklistItem, error) {
	items, err := parseChecklist(checklistPath)
	if err != nil {
		return nil, err
	}

	var pending []ChecklistItem
	for _, item := range items {
		if item.Status == "~" && matchesPattern(item.Line, pattern) {
			pending = append(pending, item)
		}
	}
	return pending, nil
}

// CountByStatus reads the file and returns counts keyed by status character.
// The returned map always contains keys "x", "s", and "~" (defaulting to 0).
func CountByStatus(checklistPath string) (map[string]int, error) {
	items, err := parseChecklist(checklistPath)
	if err != nil {
		return nil, err
	}

	counts := map[string]int{"x": 0, "s": 0, "~": 0}
	for _, item := range items {
		counts[item.Status]++
	}
	return counts, nil
}

// CountPending returns the count of [~] items whose Line matches pattern.
func CountPending(checklistPath string, pattern string) (int, error) {
	pending, err := GetPending(checklistPath, pattern)
	if err != nil {
		return 0, err
	}
	return len(pending), nil
}
