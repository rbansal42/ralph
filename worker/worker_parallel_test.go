package worker

import (
	"context"
	"fmt"
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

type fakeParallelBackend struct {
	mu            sync.Mutex
	currentCalls  int
	maxConcurrent int
	delay         time.Duration
	failPaths     map[string]bool
	partialPaths  map[string]bool
}

func (b *fakeParallelBackend) Name() string { return "fake" }

func (b *fakeParallelBackend) CheckAuth(context.Context, string) error { return nil }

func (b *fakeParallelBackend) AuthGuide() string { return "" }

func (b *fakeParallelBackend) RunPrompt(_ context.Context, promptFile string, _ string, _ string) (string, int, error) {
	b.mu.Lock()
	b.currentCalls++
	if b.currentCalls > b.maxConcurrent {
		b.maxConcurrent = b.currentCalls
	}
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		b.currentCalls--
		b.mu.Unlock()
	}()

	time.Sleep(b.delay)

	promptBytes, err := os.ReadFile(promptFile)
	if err != nil {
		return "", -1, err
	}

	var targetPath string
	for _, line := range strings.Split(string(promptBytes), "\n") {
		if strings.HasPrefix(line, "- [~] ") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				targetPath = fields[2]
				break
			}
		}
	}

	if targetPath == "" {
		return "", 1, fmt.Errorf("missing checklist item in prompt")
	}

	if b.failPaths[targetPath] {
		output := fmt.Sprintf(
			"%s{\"attempted\":[%q],\"completed\":[],\"files_changed\":[],\"checklist_lines\":[],\"failure_reason\":\"backend error\"}",
			childResultMarker,
			targetPath,
		)
		return output, 1, fmt.Errorf("backend error")
	}

	if b.partialPaths[targetPath] {
		output := fmt.Sprintf(
			"%s{\"attempted\":[%q],\"completed\":[],\"partial\":[%q],\"files_changed\":[%q],\"checklist_lines\":[],\"failure_reason\":\"\"}",
			childResultMarker,
			targetPath,
			targetPath,
			targetPath,
		)
		return output, 0, nil
	}

	output := fmt.Sprintf(
		"%s{\"attempted\":[%q],\"completed\":[%q],\"files_changed\":[%q],\"checklist_lines\":[],\"failure_reason\":\"\"}",
		childResultMarker,
		targetPath,
		targetPath,
		targetPath,
	)

	return output, 0, nil
}

func TestRunChildBatchRunsMultipleAgentsAndMarksChecklist(t *testing.T) {
	dir := t.TempDir()
	checklistPath := filepath.Join(dir, "CHECKLIST.md")
	basePrompt := filepath.Join(dir, "prompt.md")

	if err := os.WriteFile(basePrompt, []byte("base instructions"), 0o644); err != nil {
		t.Fatalf("WriteFile(prompt) error = %v", err)
	}

	checklist := strings.Join([]string{
		"- [~] app/Tasks/A.php — pending",
		"- [~] app/Tasks/B.php — pending",
		"",
	}, "\n")
	if err := os.WriteFile(checklistPath, []byte(checklist), 0o644); err != nil {
		t.Fatalf("WriteFile(checklist) error = %v", err)
	}

	backend := &fakeParallelBackend{delay: 50 * time.Millisecond}
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

	completed, output, exitCode, err := worker.runChildBatch(context.Background(), 1, items, nil)
	if err != nil {
		t.Fatalf("runChildBatch() error = %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0", exitCode)
	}
	if completed != 2 {
		t.Fatalf("completed = %d, want 2", completed)
	}
	if backend.maxConcurrent < 2 {
		t.Fatalf("maxConcurrent = %d, want at least 2", backend.maxConcurrent)
	}
	if strings.Count(output, childResultMarker) != 2 {
		t.Fatalf("output marker count = %d, want 2", strings.Count(output, childResultMarker))
	}
	if worker.GetActiveChildren() != 0 {
		t.Fatalf("GetActiveChildren() = %d, want 0", worker.GetActiveChildren())
	}

	remaining, err := CountPending(checklistPath, "app/Tasks")
	if err != nil {
		t.Fatalf("CountPending() error = %v", err)
	}
	if remaining != 0 {
		t.Fatalf("remaining = %d, want 0", remaining)
	}
}

func TestRunChildBatchDoesNotUpdateChecklistOnMixedFailure(t *testing.T) {
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
		delay:     10 * time.Millisecond,
		failPaths: map[string]bool{"app/Tasks/B.php": true},
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

	completed, _, exitCode, err := worker.runChildBatch(context.Background(), 1, items, nil)
	if err == nil {
		t.Fatal("runChildBatch() error = nil, want failure")
	}
	if exitCode == 0 {
		t.Fatalf("exitCode = %d, want non-zero", exitCode)
	}
	if completed != 1 {
		t.Fatalf("completed = %d, want 1 successful child before batch rejection", completed)
	}

	updated, readErr := os.ReadFile(checklistPath)
	if readErr != nil {
		t.Fatalf("ReadFile() error = %v", readErr)
	}
	if got := string(updated); got != raw {
		t.Fatalf("checklist changed on mixed failure:\n%s", got)
	}
}

func TestCollectChildBatchUsesShapedCompanionClaims(t *testing.T) {
	dir := t.TempDir()
	checklistPath := filepath.Join(dir, "CHECKLIST.md")
	basePrompt := filepath.Join(dir, "prompt.md")

	if err := os.WriteFile(basePrompt, []byte("base instructions"), 0o644); err != nil {
		t.Fatalf("WriteFile(prompt) error = %v", err)
	}

	raw := strings.Join([]string{
		"- [~] worker/foo.go — pending",
		"- [~] worker/foo_test.go — pending",
		"",
	}, "\n")
	if err := os.WriteFile(checklistPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile(checklist) error = %v", err)
	}

	backend := &fakeParallelBackend{delay: 10 * time.Millisecond}
	worker := &Worker{
		Num:          1,
		Name:         "tasks",
		Pattern:      "worker/",
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

	items, err := GetPending(checklistPath, "worker/")
	if err != nil {
		t.Fatalf("GetPending() error = %v", err)
	}

	batch, err := worker.collectChildBatch(context.Background(), 1, items, nil)
	if err != nil {
		t.Fatalf("collectChildBatch() error = %v", err)
	}
	if len(batch.completed) != 2 {
		t.Fatalf("len(batch.completed) = %d, want 2", len(batch.completed))
	}
	if backend.maxConcurrent != 1 {
		t.Fatalf("maxConcurrent = %d, want 1 because companion claims overlap", backend.maxConcurrent)
	}
}
