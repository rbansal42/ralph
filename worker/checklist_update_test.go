package worker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarkCompletedPathsMarksMatchingPendingItems(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CHECKLIST.md")
	raw := strings.Join([]string{
		"- [~] app/Tasks/A.php — pending",
		"- [~] app/Tasks/B.php — pending",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := MarkCompletedPaths(path, []string{"app/Tasks/A.php"}); err != nil {
		t.Fatalf("MarkCompletedPaths() error = %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	got := string(updated)
	if !strings.Contains(got, "- [x] app/Tasks/A.php — pending") {
		t.Fatalf("updated checklist missing completed item:\n%s", got)
	}
	if !strings.Contains(got, "- [~] app/Tasks/B.php — pending") {
		t.Fatalf("updated checklist should retain untouched item:\n%s", got)
	}
}

func TestMarkCompletedPathsIgnoresUnknownPaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CHECKLIST.md")
	raw := "- [~] app/Tasks/A.php — pending\n"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := MarkCompletedPaths(path, []string{"app/Tasks/Missing.php"}); err != nil {
		t.Fatalf("MarkCompletedPaths() error = %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if got := string(updated); got != raw {
		t.Fatalf("checklist changed unexpectedly:\n%s", got)
	}
}
