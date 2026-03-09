# Buffered Generations Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make `commit_batch_size` and `parent_reset_after_runs` active in the live worker loop by persisting accepted result buffers across iterations and generations, while only flushing on a later clean threshold.

**Architecture:** Extend worker state with durable buffered completed/partial result records and generation metadata. Refactor the live worker loop so child batch results are accepted into that buffer, generation rollover persists state without committing, and clean future flush points reconcile checklist updates plus a single git commit.

**Tech Stack:** Go 1.25, Cobra CLI, existing `worker`, `state`, `config`, and `ui` packages, standard library concurrency, `go test`

---

### Task 1: Add durable buffered-result state schema

**Files:**
- Modify: `state/state.go`
- Create: `state/buffer_state_test.go`

**Step 1: Write the failing tests**

```go
func TestStateRoundTripBufferedWorkerState(t *testing.T) {
	s := &State{
		Workers: map[string]*WorkerState{
			"tasks": {
				ParentGeneration:   2,
				GenerationRuns:     4,
				PendingCommitCount: 3,
				BufferedCompleted: []BufferedResult{{Path: "app/Tasks/A.php", Complete: true}},
				BufferedPartial:   []BufferedResult{{Path: "app/Tasks/B.php", Complete: false}},
			},
		},
	}
	// save, load, assert all fields survive
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./state`
Expected: FAIL with missing buffered state fields/types.

**Step 3: Write the minimal implementation**

```go
type BufferedResult struct {
	Path        string   `json:"path"`
	FilesChanged []string `json:"files_changed,omitempty"`
	Complete    bool     `json:"complete"`
	Generation  int      `json:"generation"`
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./state`
Expected: PASS

**Step 5: Commit**

```bash
git add state/state.go state/buffer_state_test.go
git commit -m "feat: persist buffered worker state"
```

### Task 2: Add buffer manager primitives for completed vs partial results

**Files:**
- Create: `worker/buffer.go`
- Create: `worker/buffer_test.go`

**Step 1: Write the failing tests**

```go
func TestBufferAcceptsCompletedAndPartialResultsSeparately(t *testing.T) {
	buf := NewResultBuffer()
	buf.Accept(BufferedResult{Path: "a", Complete: true})
	buf.Accept(BufferedResult{Path: "b", Complete: false})
	if buf.CompletedCount() != 1 || buf.PartialCount() != 1 {
		t.Fatal("expected completed and partial results to be tracked separately")
	}
}

func TestBufferFlushCandidatesRequireThreshold(t *testing.T) {
	buf := NewResultBuffer()
	buf.Accept(BufferedResult{Path: "a", Complete: true})
	if len(buf.FlushCandidates(2)) != 0 {
		t.Fatal("unexpected flush candidates below threshold")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./worker`
Expected: FAIL with missing buffer manager primitives.

**Step 3: Write the minimal implementation**

```go
type ResultBuffer struct {
	completed []BufferedResult
	partial   []BufferedResult
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./worker`
Expected: PASS

**Step 5: Commit**

```bash
git add worker/buffer.go worker/buffer_test.go
git commit -m "feat: add result buffer primitives"
```

### Task 3: Add generation rollover policy helpers

**Files:**
- Modify: `worker/worker.go`
- Create: `worker/generation_policy_test.go`

**Step 1: Write the failing tests**

```go
func TestShouldResetGenerationAfterConfiguredRuns(t *testing.T) {
	if !shouldResetGeneration(3, 3) {
		t.Fatal("expected rollover at threshold")
	}
}

func TestShouldNotResetGenerationBelowThreshold(t *testing.T) {
	if shouldResetGeneration(2, 3) {
		t.Fatal("unexpected rollover below threshold")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./worker`
Expected: FAIL with missing generation helper.

**Step 3: Write the minimal implementation**

```go
func shouldResetGeneration(currentRuns int, threshold int) bool {
	return threshold > 0 && currentRuns >= threshold
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./worker`
Expected: PASS

**Step 5: Commit**

```bash
git add worker/worker.go worker/generation_policy_test.go
git commit -m "feat: add generation rollover policy helpers"
```

### Task 4: Wire accepted result buffering into the live child-batch flow

**Files:**
- Modify: `worker/child_batch.go`
- Modify: `worker/child.go`
- Create: `worker/child_batch_buffer_test.go`

**Step 1: Write the failing tests**

```go
func TestRunChildBatchReturnsAcceptedCompletedAndPartialResults(t *testing.T) {
	// fake backend returns one completed and one partial child result
	// assert child batch exposes both to parent without forcing checklist writes
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./worker`
Expected: FAIL because child batch only reports completed count/output today.

**Step 3: Write the minimal implementation**

```go
type acceptedBatch struct {
	completed []BufferedResult
	partial   []BufferedResult
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./worker`
Expected: PASS

**Step 5: Commit**

```bash
git add worker/child.go worker/child_batch.go worker/child_batch_buffer_test.go
git commit -m "feat: return accepted result buffers from child batches"
```

### Task 5: Buffer accepted results across worker iterations and reset generations without flushing

**Files:**
- Modify: `worker/worker.go`
- Modify: `state/state.go`
- Create: `worker/worker_generation_buffer_test.go`

**Step 1: Write the failing tests**

```go
func TestWorkerCarriesCompletedBufferAcrossGenerationReset(t *testing.T) {
	// seed worker state with completed buffer just below flush threshold
	// trigger generation rollover
	// assert buffer survives, generation increments, and no commit happens
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./worker ./state`
Expected: FAIL because rollover does not preserve buffered results yet.

**Step 3: Write the minimal implementation**

```go
// in live worker loop:
// 1. accept results into durable buffer
// 2. persist generation run count
// 3. roll generation when threshold reached
// 4. do not flush on reset
```

**Step 4: Run tests to verify they pass**

Run: `go test ./worker ./state`
Expected: PASS

**Step 5: Commit**

```bash
git add worker/worker.go state/state.go worker/worker_generation_buffer_test.go
git commit -m "feat: carry accepted buffers across worker generations"
```

### Task 6: Flush inherited completed results only at a later clean threshold

**Files:**
- Modify: `worker/worker.go`
- Modify: `worker/checklist.go`
- Create: `worker/worker_flush_test.go`

**Step 1: Write the failing tests**

```go
func TestInheritedCompletedResultsFlushOnlyWhenThresholdReachedLater(t *testing.T) {
	// seed buffered completed results below threshold
	// accept one later clean completed result
	// assert checklist update + single commit happen only at threshold
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./worker`
Expected: FAIL because inherited buffered results are not flushed through the live threshold path.

**Step 3: Write the minimal implementation**

```go
// compute flush candidates from durable buffer
// on clean threshold only:
// 1. update checklist
// 2. commit
// 3. remove flushed completed records
```

**Step 4: Run tests to verify they pass**

Run: `go test ./worker`
Expected: PASS

**Step 5: Commit**

```bash
git add worker/worker.go worker/checklist.go worker/worker_flush_test.go
git commit -m "feat: flush inherited completed results at later thresholds"
```

### Task 7: Surface generation/buffer status in operator views

**Files:**
- Modify: `ui/dashboard.go`
- Modify: `ui/tui.go`
- Modify: `cmd/run.go`
- Modify: `cmd/status.go`
- Create: `ui/generation_status_test.go`

**Step 1: Write the failing tests**

```go
func TestWorkerGenerationLabel(t *testing.T) {
	info := WorkerInfo{Generation: 3, BufferedCompleted: 2, BufferedPartial: 1}
	if got := workerGenerationLabel(info); got != "gen 3 | buf 2c/1p" {
		t.Fatalf("unexpected label: %s", got)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./ui`
Expected: FAIL with missing generation/buffer status fields.

**Step 3: Write the minimal implementation**

```go
type WorkerInfo struct {
	// existing fields...
	Generation        int
	BufferedCompleted int
	BufferedPartial   int
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./ui`
Expected: PASS

**Step 5: Commit**

```bash
git add ui/dashboard.go ui/tui.go cmd/run.go cmd/status.go ui/generation_status_test.go
git commit -m "feat: show generation and buffer state in status views"
```

### Task 8: Run full verification and prepare integration review

**Files:**
- Modify: `docs/plans/2026-03-10-runtime-buffered-generations-design.md`
- Modify: `docs/plans/2026-03-10-runtime-buffered-generations.md`

**Step 1: Run targeted suites**

Run: `go test ./worker ./state ./ui`
Expected: PASS

**Step 2: Run the full suite**

Run: `go test ./...`
Expected: PASS

**Step 3: Capture phase notes**

```markdown
- deferred flush across generations verified
- rollover without forced commit verified
- inherited completed results flush only on later clean threshold
```

**Step 4: Review branch diff against base**

Run: `git diff --stat origin/main...HEAD`
Expected: shows only phase-2 runtime buffering/generation changes plus tests/docs.

**Step 5: Commit**

```bash
git add docs/plans/2026-03-10-runtime-buffered-generations-design.md docs/plans/2026-03-10-runtime-buffered-generations.md
git commit -m "docs: record verification notes for buffered generations"
```
