# Parent Worker Child Agents Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Convert each Ralph worker into a parent coordinator that can dispatch multiple child agents in parallel inside one shared workspace, while enforcing non-overlapping file access and resetting from compact machine state only.

**Architecture:** Keep the existing top-level worker ownership model, but refactor each worker into a parent event loop with a bounded child-agent pool. Add scheduler primitives for task shaping and file claims, persist only machine state needed for reset/resume, and keep commits parent-controlled and batched so git does not become the new bottleneck.

**Tech Stack:** Go 1.25, Cobra CLI, existing `backend`, `worker`, `state`, and `ui` packages, standard library concurrency primitives, `go test`

---

### Task 1: Add config and state primitives for parent/child execution

**Files:**
- Modify: `config/config.go`
- Modify: `config/config_test.go`
- Modify: `state/state.go`
- Create: `state/state_test.go`

**Step 1: Write the failing tests**

```go
func TestLoadParentWorkerDefaults(t *testing.T) {
	cfg := mustLoadConfigFromTOML(t, `
backend = "opencode"
checklist = "CHECKLIST.md"
prompt = "prompt.md"

[[worker]]
name = "tasks"
pattern = "app/Tasks"
`)

	if cfg.WorkerParallelism != 2 {
		t.Fatalf("WorkerParallelism = %d, want 2", cfg.WorkerParallelism)
	}
	if cfg.ParentResetAfterRuns != 25 {
		t.Fatalf("ParentResetAfterRuns = %d, want 25", cfg.ParentResetAfterRuns)
	}
	if cfg.ParentResetAfterMinutes != 30 {
		t.Fatalf("ParentResetAfterMinutes = %d, want 30", cfg.ParentResetAfterMinutes)
	}
	if cfg.ChildMaxRetries != 2 {
		t.Fatalf("ChildMaxRetries = %d, want 2", cfg.ChildMaxRetries)
	}
	if cfg.ChildQuarantineAfter != 3 {
		t.Fatalf("ChildQuarantineAfter = %d, want 3", cfg.ChildQuarantineAfter)
	}
}

func TestStateRoundTripParentFields(t *testing.T) {
	s := &State{
		Workers: map[string]*WorkerState{
			"tasks": {
				Parent: &ParentState{
					ParentGeneration: 2,
					PendingTaskIDs:   []string{"task-1"},
					InFlight: map[string]InFlightChildState{
						"child-1": {TaskID: "task-2", Files: []string{"app/Tasks/Foo.php"}},
					},
					FailedTaskIDs: []string{"task-3"},
					RetryCountByItem: map[string]int{
						"task-3": 2,
					},
					QuarantinedTaskIDs: []string{"task-4"},
					ClaimedFiles: map[string]string{
						"app/Tasks/Foo.php": "child-1",
					},
					LastCommit: "abc123",
				},
			},
		},
	}

	path := filepath.Join(t.TempDir(), "state.json")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	parent := loaded.Workers["tasks"].Parent
	if parent.ParentGeneration != 2 {
		t.Fatalf("ParentGeneration = %d, want 2", parent.ParentGeneration)
	}
	if parent.LastCommit != "abc123" {
		t.Fatalf("LastCommit = %q, want abc123", parent.LastCommit)
	}
	if got := parent.RetryCountByItem["task-3"]; got != 2 {
		t.Fatalf("RetryCountByItem[task-3] = %d, want 2", got)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./config ./state`
Expected: FAIL with unknown config/state fields and missing test helpers.

**Step 3: Write the minimal implementation**

```go
type Config struct {
	// existing fields...
	WorkerParallelism        int `toml:"worker_parallelism"`
	ParentResetAfterRuns     int `toml:"parent_reset_after_runs"`
	ParentResetAfterMinutes  int `toml:"parent_reset_after_minutes"`
	CommitBatchSize          int `toml:"commit_batch_size"`
	ChildMaxRetries          int `toml:"child_max_retries"`
	ChildQuarantineAfter     int `toml:"child_quarantine_after"`
}

type WorkerState struct {
	Iteration int               `json:"iteration"`
	Completed int               `json:"completed"`
	History   []IterationRecord `json:"history"`
	Parent    *ParentState      `json:"parent,omitempty"`
}

type ParentState struct {
	PendingTaskIDs      []string                  `json:"pending_task_ids,omitempty"`
	InFlight            map[string]InFlightChildState `json:"in_flight,omitempty"`
	CompletedTaskIDs    []string                  `json:"completed_task_ids,omitempty"`
	FailedTaskIDs       []string                  `json:"failed_task_ids,omitempty"`
	RetryCountByItem    map[string]int            `json:"retry_count_by_item,omitempty"`
	QuarantinedTaskIDs  []string                  `json:"quarantined_task_ids,omitempty"`
	ClaimedFiles        map[string]string `json:"claimed_files,omitempty"`
	LastCommit          string           `json:"last_commit,omitempty"`
	ParentGeneration    int              `json:"parent_generation,omitempty"`
}

type InFlightChildState struct {
	TaskID string   `json:"task_id"`
	Files  []string `json:"files"`
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./config ./state`
Expected: PASS

**Step 5: Commit**

```bash
git add config/config.go config/config_test.go state/state.go state/state_test.go
git commit -m "feat: add parent worker config and state primitives"
```

### Task 2: Add task-shaping and file-claim scheduler primitives

**Files:**
- Create: `worker/scheduler.go`
- Create: `worker/scheduler_test.go`
- Modify: `worker/checklist.go`

**Step 1: Write the failing tests**

```go
func TestClaimTableRejectsOverlappingFiles(t *testing.T) {
	table := NewClaimTable()
	if !table.TryClaim("child-1", []string{"app/Tasks/A.php"}) {
		t.Fatal("first claim should succeed")
	}
	if table.TryClaim("child-2", []string{"app/Tasks/A.php"}) {
		t.Fatal("overlapping claim should fail")
	}
}

func TestSchedulerRoutesUnknownFootprintToSerialLane(t *testing.T) {
	task := Task{ID: "1", Files: nil}
	parallel, serial := PartitionDispatchable([]Task{task})
	if len(parallel) != 0 || len(serial) != 1 {
		t.Fatalf("parallel=%d serial=%d, want 0 and 1", len(parallel), len(serial))
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./worker`
Expected: FAIL with undefined `NewClaimTable`, `Task`, and `PartitionDispatchable`.

**Step 3: Write the minimal implementation**

```go
type Task struct {
	ID       string
	Item     ChecklistItem
	Files    []string
	SerialOnly bool
}

type ClaimTable struct {
	owners map[string]string
}

func (c *ClaimTable) TryClaim(childID string, files []string) bool {
	for _, file := range files {
		if _, exists := c.owners[file]; exists {
			return false
		}
	}
	for _, file := range files {
		c.owners[file] = childID
	}
	return true
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./worker`
Expected: PASS

**Step 5: Commit**

```bash
git add worker/checklist.go worker/scheduler.go worker/scheduler_test.go
git commit -m "feat: add scheduler and file claim primitives"
```

### Task 3: Define the child result contract, child prompt, and child-run wrapper

**Files:**
- Create: `worker/child.go`
- Create: `worker/child_test.go`
- Modify: `worker/prompt.go`
- Modify: `backend/backend.go`

**Step 1: Write the failing tests**

```go
func TestParseChildResultSummary(t *testing.T) {
	output := `RALPH_CHILD_RESULT {"completed":["app/Tasks/A.php"],"files_changed":["app/Tasks/A.php"],"exit_code":0}`
	result, err := ParseChildResult(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.CompletedItems) != 1 {
		t.Fatalf("CompletedItems = %d, want 1", len(result.CompletedItems))
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}
}

func TestParseChildResultRequiresMarker(t *testing.T) {
	if _, err := ParseChildResult("plain log"); err == nil {
		t.Fatal("expected error for missing marker")
	}
}

func TestBuildChildPromptRestrictsAllowedFiles(t *testing.T) {
	path, err := BuildChildPrompt(t.TempDir(), Task{
		ID:    "1",
		Item:  ChecklistItem{Line: "- [~] app/Tasks/A.php"},
		Files: []string{"app/Tasks/A.php"},
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "Allowed files: app/Tasks/A.php") {
		t.Fatal("expected allowed files section")
	}
	if !strings.Contains(text, "Output RALPH_CHILD_RESULT") {
		t.Fatal("expected child result marker instruction")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./worker ./backend`
Expected: FAIL with undefined child result parsing types/functions and missing child prompt builder.

**Step 3: Write the minimal implementation**

```go
type ChildResult struct {
	AttemptedItems []string `json:"attempted"`
	CompletedItems []string `json:"completed"`
	FilesChanged   []string `json:"files_changed"`
	ChecklistLines []int    `json:"checklist_lines"`
	ExitCode       int      `json:"exit_code"`
	FailureReason  string   `json:"failure_reason,omitempty"`
}

func ParseChildResult(output string) (ChildResult, error) {
	const marker = "RALPH_CHILD_RESULT "
	idx := strings.LastIndex(output, marker)
	if idx == -1 {
		return ChildResult{}, ErrMissingChildResult
	}
	var result ChildResult
	err := json.Unmarshal([]byte(output[idx+len(marker):]), &result)
	return result, err
}

func BuildChildPrompt(logDir string, task Task) (string, error) {
	// Write a child-specific prompt that includes the exact allowed file set,
	// checklist update rules, and requires a RALPH_CHILD_RESULT payload.
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./worker ./backend`
Expected: PASS

**Step 5: Commit**

```bash
git add backend/backend.go worker/child.go worker/child_test.go worker/prompt.go
git commit -m "feat: add child result contract"
```

### Task 4: Refactor the worker loop into a parent event loop with child slots

**Files:**
- Modify: `worker/worker.go`
- Create: `worker/parent.go`
- Create: `worker/parent_test.go`

**Step 1: Write the failing tests**

```go
func TestParentDispatchesUpToParallelism(t *testing.T) {
	parent := newTestParentWorker(t, 2)
	parent.Enqueue(
		Task{ID: "1", Files: []string{"a.go"}},
		Task{ID: "2", Files: []string{"b.go"}},
		Task{ID: "3", Files: []string{"c.go"}},
	)

	parent.FillChildSlots()

	if got := parent.InFlightCount(); got != 2 {
		t.Fatalf("InFlightCount = %d, want 2", got)
	}
}

func TestParentSkipsBlockedTaskAndDispatchesCompatibleTask(t *testing.T) {
	parent := newTestParentWorker(t, 2)
	parent.claims.TryClaim("child-1", []string{"a.go"})
	parent.Enqueue(Task{ID: "blocked", Files: []string{"a.go"}})
	parent.Enqueue(Task{ID: "free", Files: []string{"b.go"}})

	parent.FillChildSlots()

	if !parent.IsInFlight("free") {
		t.Fatal("expected compatible task to be dispatched")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./worker`
Expected: FAIL with missing parent worker orchestration methods.

**Step 3: Write the minimal implementation**

```go
type ParentWorker struct {
	parallelism int
	queue       []Task
	inFlight    map[string]Task
	claims      *ClaimTable
}

func (p *ParentWorker) FillChildSlots() {
	for len(p.inFlight) < p.parallelism {
		task, ok := p.nextDispatchableTask()
		if !ok {
			return
		}
		p.inFlight[task.ID] = task
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./worker`
Expected: PASS

**Step 5: Commit**

```bash
git add worker/worker.go worker/parent.go worker/parent_test.go
git commit -m "feat: add parent worker event loop"
```

### Task 5: Add parent-controlled reconciliation, retries/quarantine, and batched commits

**Files:**
- Create: `worker/commit_batch.go`
- Create: `worker/commit_batch_test.go`
- Modify: `worker/parent.go`
- Modify: `worker/worker.go`

**Step 1: Write the failing tests**

```go
func TestCommitBatchFlushesAfterThreshold(t *testing.T) {
	batch := NewCommitBatcher(2)
	if batch.Add(successfulResult("1")) {
		t.Fatal("first result should not flush")
	}
	if !batch.Add(successfulResult("2")) {
		t.Fatal("second result should trigger flush")
	}
}

func TestCommitBatchRejectsFailedResult(t *testing.T) {
	batch := NewCommitBatcher(2)
	if batch.Add(failedResult("1")) {
		t.Fatal("failed result must not trigger batch acceptance")
	}
}

func TestReconcileFailedChildRequeuesUntilRetryLimit(t *testing.T) {
	parent := newTestParentWorker(t, 2)
	parent.retryLimit = 2
	parent.MarkInFlight(Task{ID: "1", Files: []string{"a.go"}})

	parent.ReconcileChildResult("child-1", ChildResult{ExitCode: 1, FailureReason: "boom"})

	if got := parent.RetryCount("1"); got != 1 {
		t.Fatalf("RetryCount = %d, want 1", got)
	}
	if !parent.IsQueued("1") {
		t.Fatal("expected task to be requeued")
	}
}

func TestReconcileFailedChildQuarantinesAfterRetryLimit(t *testing.T) {
	parent := newTestParentWorker(t, 2)
	parent.retryLimit = 1
	parent.MarkInFlight(Task{ID: "1", Files: []string{"a.go"}})
	parent.retryCount["1"] = 1

	parent.ReconcileChildResult("child-1", ChildResult{ExitCode: 1, FailureReason: "boom"})

	if !parent.IsQuarantined("1") {
		t.Fatal("expected task to be quarantined")
	}
	if parent.claims.IsClaimed("a.go") {
		t.Fatal("expected claims to be released")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./worker`
Expected: FAIL with undefined commit batcher helpers and missing reconciliation/retry logic.

**Step 3: Write the minimal implementation**

```go
type CommitBatcher struct {
	threshold int
	results   []ChildResult
}

func (b *CommitBatcher) Add(result ChildResult) bool {
	if result.ExitCode != 0 || result.FailureReason != "" {
		return false
	}
	b.results = append(b.results, result)
	return len(b.results) >= b.threshold
}

func (p *ParentWorker) ReconcileChildResult(childID string, result ChildResult) {
	// Release claims, record completion, or requeue/quarantine based on retry budget.
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./worker`
Expected: PASS

**Step 5: Commit**

```bash
git add worker/worker.go worker/parent.go worker/commit_batch.go worker/commit_batch_test.go
git commit -m "feat: add batched parent commit integration"
```

### Task 6: Add reset and resume behavior from compact machine state

**Files:**
- Modify: `worker/parent.go`
- Modify: `state/state.go`
- Create: `worker/parent_reset_test.go`

**Step 1: Write the failing tests**

```go
func TestParentResetPreservesQueueAndGeneration(t *testing.T) {
	parent := newTestParentWorker(t, 2)
	parent.Enqueue(Task{ID: "1", Files: []string{"a.go"}})
	parent.Completed["done"] = true
	parent.retryCount["1"] = 2
	parent.quarantined["2"] = true
	parent.Generation = 3
	parent.lastCommit = "abc123"

	snapshot := parent.Snapshot()
	reloaded := ParentWorkerFromSnapshot(snapshot)

	if reloaded.Generation != 3 {
		t.Fatalf("Generation = %d, want 3", reloaded.Generation)
	}
	if len(reloaded.Queue()) != 1 {
		t.Fatalf("queue length = %d, want 1", len(reloaded.Queue()))
	}
	if got := reloaded.retryCount["1"]; got != 2 {
		t.Fatalf("retryCount = %d, want 2", got)
	}
	if !reloaded.quarantined["2"] {
		t.Fatal("expected quarantined task to persist")
	}
	if reloaded.lastCommit != "abc123" {
		t.Fatalf("lastCommit = %q, want abc123", reloaded.lastCommit)
	}
}

func TestParentResetWaitsForInflightChildren(t *testing.T) {
	parent := newTestParentWorker(t, 1)
	parent.MarkInFlight(Task{ID: "1", Files: []string{"a.go"}})
	if parent.CanResetNow() {
		t.Fatal("expected reset to be blocked while child is in flight")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./worker ./state`
Expected: FAIL with missing snapshot/reset behavior.

**Step 3: Write the minimal implementation**

```go
type ParentSnapshot struct {
	Generation          int
	PendingTaskIDs      []string
	InFlight            map[string]TaskLease
	CompletedTaskIDs    []string
	FailedTaskIDs       []string
	RetryCountByItem    map[string]int
	QuarantinedTaskIDs  []string
	ClaimedFiles        map[string]string
	LastCommit          string
}

func (p *ParentWorker) Snapshot() ParentSnapshot {
	return ParentSnapshot{
		Generation:       p.Generation,
		InFlight:         maps.Clone(p.inFlightLeases),
		RetryCountByItem: maps.Clone(p.retryCount),
		ClaimedFiles:     maps.Clone(p.claims.owners),
		LastCommit:       p.lastCommit,
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./worker ./state`
Expected: PASS

**Step 5: Commit**

```bash
git add state/state.go worker/parent.go worker/parent_reset_test.go
git commit -m "feat: add parent reset and resume state"
```

### Task 7: Surface child-pool progress in the dashboard and status output

**Files:**
- Modify: `cmd/run.go`
- Modify: `cmd/status.go`
- Create: `cmd/run_test.go`
- Modify: `ui/dashboard.go`
- Modify: `ui/tui.go`
- Modify: `worker/worker.go`

**Step 1: Write the failing tests**

```go
func TestWorkerInfoIncludesChildPoolStats(t *testing.T) {
	info := WorkerInfo{
		Num: 1,
		Name: "tasks",
		ActiveChildren: 2,
		ChildCapacity: 4,
	}
	if info.ActiveChildren != 2 {
		t.Fatalf("ActiveChildren = %d, want 2", info.ActiveChildren)
	}
}

func TestBuildWorkerInfosIncludesChildPoolSettings(t *testing.T) {
	cfg := mustLoadConfigFromTOML(t, `
backend = "opencode"
checklist = "CHECKLIST.md"
prompt = "prompt.md"
worker_parallelism = 3

[[worker]]
name = "tasks"
pattern = "app/Tasks"
`)

	infos := buildWorkerInfos(cfg)
	if infos[0].ChildCapacity != 3 {
		t.Fatalf("ChildCapacity = %d, want 3", infos[0].ChildCapacity)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./cmd ./ui ./worker`
Expected: FAIL with missing worker info fields and missing command-layer population.

**Step 3: Write the minimal implementation**

```go
type WorkerInfo struct {
	Num            int
	Name           string
	Remaining      int
	Status         string
	ActiveChildren int
	ChildCapacity  int
	ParentGeneration int
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./cmd ./ui ./worker`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/run.go cmd/status.go cmd/run_test.go ui/dashboard.go ui/tui.go worker/worker.go
git commit -m "feat: show child pool progress in status views"
```

### Task 8: Add scheduler and throughput verification coverage

**Files:**
- Create: `worker/simulation_test.go`
- Modify: `docs/plans/2026-03-09-parent-worker-child-agents-design.md`

**Step 1: Write the failing tests**

```go
func TestSchedulerKeepsWorkersBusyWithoutOverlap(t *testing.T) {
	sim := NewSchedulerSimulation(
		Task{ID: "1", Files: []string{"a.go"}},
		Task{ID: "2", Files: []string{"b.go"}},
		Task{ID: "3", Files: []string{"a.go"}},
	)

	report := sim.Run(2)

	if report.MaxConcurrentConflicts != 0 {
		t.Fatalf("MaxConcurrentConflicts = %d, want 0", report.MaxConcurrentConflicts)
	}
	if report.MaxActiveChildren < 2 {
		t.Fatalf("MaxActiveChildren = %d, want at least 2", report.MaxActiveChildren)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./worker`
Expected: FAIL with missing simulation harness.

**Step 3: Write the minimal implementation**

```go
type SimulationReport struct {
	MaxActiveChildren     int
	MaxConcurrentConflicts int
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./worker`
Expected: PASS

**Step 5: Commit**

```bash
git add worker/simulation_test.go docs/plans/2026-03-09-parent-worker-child-agents-design.md
git commit -m "test: add scheduler throughput simulation coverage"
```

### Task 9: Run full verification and capture rollout notes

**Files:**
- Modify: `docs/plans/2026-03-09-parent-worker-child-agents-design.md`
- Modify: `docs/plans/2026-03-09-parent-worker-child-agents.md`

**Step 1: Run the focused test suites**

Run: `go test ./config ./state ./worker ./ui ./cmd`
Expected: PASS

**Step 2: Run the full repository test suite**

Run: `go test ./...`
Expected: PASS

**Step 3: Dry-run the CLI with the new config fields**

Run:

```bash
tmpdir=$(mktemp -d)
cat >"$tmpdir/ralph.toml" <<EOF
backend = "opencode"
checklist = "$tmpdir/CHECKLIST.md"
prompt = "$tmpdir/prompt.md"
worker_parallelism = 3
parent_reset_after_runs = 25
parent_reset_after_minutes = 30
child_max_retries = 2
child_quarantine_after = 3

[[worker]]
name = "tasks"
pattern = "app/Tasks"
EOF

cat >"$tmpdir/CHECKLIST.md" <<'EOF'
- [~] app/Tasks/Foo.php
EOF

cat >"$tmpdir/prompt.md" <<'EOF'
Follow the checklist exactly.
EOF

go run . run --dry-run --config "$tmpdir/ralph.toml"
```

Expected: PASS with worker child-pool settings rendered and no config validation errors.

**Step 4: Update rollout notes**

```markdown
- default child parallelism validated
- reset thresholds validated
- shared-workspace conflict checks validated
```

**Step 5: Commit**

```bash
git add docs/plans/2026-03-09-parent-worker-child-agents-design.md docs/plans/2026-03-09-parent-worker-child-agents.md
git commit -m "docs: record verification notes for parent child agents"
```
