package worker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rahulvramesh/ralph/config"
	"github.com/rahulvramesh/ralph/state"
)

func TestCollectChildBatchReturnsCompletedAndPartialBuffers(t *testing.T) {
	dir := t.TempDir()
	checklistPath := filepath.Join(dir, "CHECKLIST.md")
	basePrompt := filepath.Join(dir, "prompt.md")

	if err := os.WriteFile(basePrompt, []byte("base instructions"), 0o644); err != nil {
		t.Fatalf("WriteFile(prompt) error = %v", err)
	}

	raw := strings.Join([]string{
		"- [~] app/Tasks/A.php — pending",
		"- [~] app/Tasks/B.php — pending",
		"",
	}, "\n")
	if err := os.WriteFile(checklistPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile(checklist) error = %v", err)
	}

	backend := &fakeParallelBackend{
		delay:        10 * time.Millisecond,
		partialPaths: map[string]bool{"app/Tasks/B.php": true},
	}
	worker := &Worker{
		Num:          1,
		Name:         "tasks",
		Pattern:      "app/Tasks",
		TotalWorkers: 1,
		Backend:      backend,
		Config: &config.Config{
			Checklist:         checklistPath,
			Prompt:            basePrompt,
			Model:             "fake-model",
			Workdir:           dir,
			WorkerParallelism: 2,
		},
		State:      &state.State{},
		GitMutex:   &sync.Mutex{},
		StateMutex: &sync.Mutex{},
		Shutdown:   &atomic.Bool{},
	}

	items, err := GetPending(checklistPath, "app/Tasks")
	if err != nil {
		t.Fatalf("GetPending() error = %v", err)
	}

	batch, err := worker.collectChildBatch(context.Background(), 1, items, nil)
	if err != nil {
		t.Fatalf("collectChildBatch() error = %v", err)
	}
	if len(batch.completed) != 1 {
		t.Fatalf("len(batch.completed) = %d, want 1", len(batch.completed))
	}
	if len(batch.partial) != 1 {
		t.Fatalf("len(batch.partial) = %d, want 1", len(batch.partial))
	}

	updated, readErr := os.ReadFile(checklistPath)
	if readErr != nil {
		t.Fatalf("ReadFile() error = %v", readErr)
	}
	if got := string(updated); got != raw {
		t.Fatalf("checklist changed unexpectedly:\n%s", got)
	}
}
