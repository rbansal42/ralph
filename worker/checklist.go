package worker

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// checklistCache provides a time-based cache for parsed checklist data.
// This avoids re-reading and re-parsing the checklist file on every TUI tick
// (500ms refresh × N workers = many reads/sec).
var checklistCache struct {
	mu      sync.Mutex
	path    string
	items   []ChecklistItem
	modTime time.Time
	readAt  time.Time
	ttl     time.Duration
}

func init() {
	checklistCache.ttl = 2 * time.Second
}

// cachedParseChecklist returns parsed checklist items, using a cache with TTL.
// If the file hasn't been modified and the cache is fresh, returns cached data.
func cachedParseChecklist(path string) ([]ChecklistItem, error) {
	checklistCache.mu.Lock()
	defer checklistCache.mu.Unlock()

	now := time.Now()

	// Check if cache is still valid
	if checklistCache.path == path && now.Sub(checklistCache.readAt) < checklistCache.ttl {
		// Cache is fresh enough — check if file was modified
		info, err := os.Stat(path)
		if err == nil && info.ModTime().Equal(checklistCache.modTime) {
			// Return a copy to prevent mutation
			result := make([]ChecklistItem, len(checklistCache.items))
			copy(result, checklistCache.items)
			return result, nil
		}
	}

	// Cache miss — parse fresh
	items, err := parseChecklist(path)
	if err != nil {
		return nil, err
	}

	// Update cache
	info, _ := os.Stat(path)
	checklistCache.path = path
	checklistCache.items = items
	checklistCache.readAt = now
	if info != nil {
		checklistCache.modTime = info.ModTime()
	}

	// Return a copy
	result := make([]ChecklistItem, len(items))
	copy(result, items)
	return result, nil
}

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
	items, err := cachedParseChecklist(checklistPath)
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
	items, err := cachedParseChecklist(checklistPath)
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

// MarkCompletedPaths updates matching pending checklist lines from [~] to [x].
func MarkCompletedPaths(checklistPath string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}

	targets := make(map[string]bool, len(paths))
	for _, path := range paths {
		targets[path] = true
	}

	data, err := os.ReadFile(checklistPath)
	if err != nil {
		return fmt.Errorf("reading checklist for update: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		m := checklistRe.FindStringSubmatch(line)
		if m == nil || m[1] != "~" {
			continue
		}
		if !targets[m[2]] {
			continue
		}
		lines[i] = strings.Replace(line, "[~]", "[x]", 1)
	}

	if err := os.WriteFile(checklistPath, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		return fmt.Errorf("writing checklist update: %w", err)
	}

	checklistCache.mu.Lock()
	if checklistCache.path == checklistPath {
		checklistCache.readAt = time.Time{}
	}
	checklistCache.mu.Unlock()

	return nil
}

// DetectOverlaps checks all worker patterns against the checklist and returns
// pairs of workers that would match the same items. This is a pre-run validation.
func DetectOverlaps(checklistPath string, workers []struct{ Name, Pattern string }) []string {
	items, err := cachedParseChecklist(checklistPath)
	if err != nil {
		return nil
	}

	// Build map: item path -> list of worker names that match it
	pathWorkers := make(map[string][]string)
	for _, item := range items {
		if item.Status != "~" {
			continue // only check pending items
		}
		for _, w := range workers {
			if matchesPattern(item.Line, w.Pattern) {
				pathWorkers[item.Path] = append(pathWorkers[item.Path], w.Name)
			}
		}
	}

	// Find items matched by more than one worker
	var warnings []string
	seen := make(map[string]bool)
	for path, names := range pathWorkers {
		if len(names) > 1 {
			key := strings.Join(names, "+")
			if !seen[key] {
				seen[key] = true
				warnings = append(warnings, fmt.Sprintf(
					"workers %s overlap on %d+ items (e.g. %s)",
					strings.Join(names, ", "), countOverlap(pathWorkers, names), path))
			}
		}
	}
	return warnings
}

// changedFilesCache caches git diff results to avoid running a subprocess
// on every iteration for every worker. The cache is keyed by startCommit
// and invalidated after a short TTL.
var changedFilesCache struct {
	mu          sync.Mutex
	startCommit string
	workdir     string
	files       map[string]bool
	readAt      time.Time
	ttl         time.Duration
}

func init() {
	changedFilesCache.ttl = 5 * time.Second
}

// getChangedFiles returns the set of files changed since startCommit,
// using a cache to avoid repeated git subprocess calls.
func getChangedFiles(startCommit, workdir string) map[string]bool {
	changedFilesCache.mu.Lock()
	defer changedFilesCache.mu.Unlock()

	now := time.Now()
	if changedFilesCache.startCommit == startCommit &&
		changedFilesCache.workdir == workdir &&
		now.Sub(changedFilesCache.readAt) < changedFilesCache.ttl {
		// Return a copy so callers don't hold a reference to the cached map.
		cp := make(map[string]bool, len(changedFilesCache.files))
		for k, v := range changedFilesCache.files {
			cp[k] = v
		}
		return cp
	}

	// Cache miss — run git diff
	cmd := exec.Command("git", "diff", "--name-only", startCommit+"..HEAD")
	if workdir != "" {
		cmd.Dir = workdir
	}
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	files := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			files[line] = true
		}
	}

	changedFilesCache.startCommit = startCommit
	changedFilesCache.workdir = workdir
	changedFilesCache.files = files
	changedFilesCache.readAt = now
	return files
}

// DetectPartiallyCompleted checks pending items against git diff to find files
// that were modified since startCommit but are still marked as [~].
// Returns the items that appear partially completed.
func DetectPartiallyCompleted(checklistPath, pattern, startCommit, workdir string) ([]ChecklistItem, error) {
	if startCommit == "" {
		return nil, nil
	}

	changedFiles := getChangedFiles(startCommit, workdir)
	if len(changedFiles) == 0 {
		return nil, nil
	}

	// Check which pending items have their file already modified.
	pending, err := GetPending(checklistPath, pattern)
	if err != nil {
		return nil, err
	}

	var partial []ChecklistItem
	for _, item := range pending {
		if changedFiles[item.Path] {
			partial = append(partial, item)
		}
	}
	return partial, nil
}

// countOverlap counts how many paths are shared by all the given worker names.
func countOverlap(pathWorkers map[string][]string, names []string) int {
	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}
	count := 0
	for _, workers := range pathWorkers {
		if len(workers) < len(names) {
			continue
		}
		allMatch := true
		for _, n := range names {
			found := false
			for _, w := range workers {
				if w == n {
					found = true
					break
				}
			}
			allMatch = found
			if !allMatch {
				break
			}
		}
		if allMatch {
			count++
		}
	}
	return count
}
