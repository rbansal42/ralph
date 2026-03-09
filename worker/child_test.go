package worker

import "testing"

func TestParseChildResultSummary(t *testing.T) {
	output := `RALPH_CHILD_RESULT {"completed":["app/Tasks/A.php"],"files_changed":["app/Tasks/A.php"]}`

	result, err := ParseChildResult(output)
	if err != nil {
		t.Fatalf("ParseChildResult() error = %v", err)
	}

	if len(result.CompletedItems) != 1 {
		t.Fatalf("CompletedItems = %d, want 1", len(result.CompletedItems))
	}

	if len(result.FilesChanged) != 1 {
		t.Fatalf("FilesChanged = %d, want 1", len(result.FilesChanged))
	}
}

func TestParseChildResultRequiresMarker(t *testing.T) {
	if _, err := ParseChildResult("plain log"); err == nil {
		t.Fatal("ParseChildResult() error = nil, want marker error")
	}
}
