package worker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPromptUsesChildResultContract(t *testing.T) {
	dir := t.TempDir()
	basePrompt := filepath.Join(dir, "prompt.md")
	if err := os.WriteFile(basePrompt, []byte("base instructions"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	promptPath, err := BuildPrompt(
		basePrompt,
		"tasks",
		1,
		1,
		"app/Tasks",
		1,
		[]ChecklistItem{{Line: "- [~] app/Tasks/A.php — pending", Path: "app/Tasks/A.php"}},
		1,
		filepath.Join(dir, "logs"),
		nil,
		false,
	)
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}

	promptBytes, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	prompt := string(promptBytes)
	if !strings.Contains(prompt, "Do NOT update the checklist directly.") {
		t.Fatalf("prompt missing parent-owned checklist instruction:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Output RALPH_CHILD_RESULT") {
		t.Fatalf("prompt missing child result marker instruction:\n%s", prompt)
	}
}
