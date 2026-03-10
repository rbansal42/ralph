# Strict Task Shaping Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Enforce safety-first shared-workspace scheduling by allowing parallel child execution only for tasks that Ralph can shape into explicit claim sets before dispatch, with serial fallback for everything else.

**Architecture:** Add a worker-side shaping layer that converts checklist items into schedulable tasks with explicit claimed files, `serial_only`, and shaping reason metadata. Update the scheduler/runtime to admit only explicitly shaped tasks to the parallel lane, route all unknowns to the serial lane, and expose serial fallbacks plus claim conflicts in operator-visible state.

**Tech Stack:** Go 1.25, existing `worker`, `config`, `state`, `ui`, and `cmd` packages, standard library concurrency, `go test`

---

### Task 1: Extend schedulable tasks with shaping metadata

**Files:**
- Modify: `worker/scheduler.go`
- Modify: `worker/scheduler_test.go`

**Step 1: Write the failing tests**

```go
func TestTaskRequiresExplicitClaimsForParallelLane(t *testing.T) {
	task := Task{ID: "1", Files: []string{"a.go"}, ShapeReason: "exact path"}
	parallel, serial := PartitionDispatchable([]Task{task})
	if len(parallel) != 1 || len(serial) != 0 {
		t.Fatal("expected explicitly shaped task to stay parallel")
	}
}

func TestTaskWithoutShapeFallsBackToSerialLane(t *testing.T) {
	task := Task{ID: "1"}
	parallel, serial := PartitionDispatchable([]Task{task})
	if len(parallel) != 0 || len(serial) != 1 {
		t.Fatal("expected unshaped task to fall back to serial")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./worker`
Expected: FAIL with missing shaping metadata or stricter serial fallback logic.

**Step 3: Write the minimal implementation**

```go
type Task struct {
	ID          string
	Item        ChecklistItem
	Files       []string
	SerialOnly  bool
	ShapeReason string
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./worker`
Expected: PASS

**Step 5: Commit**

```bash
git add worker/scheduler.go worker/scheduler_test.go
git commit -m "feat: add shaping metadata to schedulable tasks"
```

### Task 2: Add deterministic worker-side task shaping

**Files:**
- Create: `worker/shaper.go`
- Create: `worker/shaper_test.go`

**Step 1: Write the failing tests**

```go
func TestShapeTaskUsesExactPathClaim(t *testing.T) {
	item := ChecklistItem{Path: "app/Tasks/A.php"}
	task := ShapeChecklistItem(item)
	if len(task.Files) != 1 || task.Files[0] != "app/Tasks/A.php" {
		t.Fatal("expected exact-path claim")
	}
}

func TestShapeTaskFallsBackToSerialWhenNoRuleApplies(t *testing.T) {
	item := ChecklistItem{}
	task := ShapeChecklistItem(item)
	if !task.SerialOnly {
		t.Fatal("expected serial fallback for unshapeable item")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./worker`
Expected: FAIL with missing shaping function.

**Step 3: Write the minimal implementation**

```go
func ShapeChecklistItem(item ChecklistItem) Task {
	if item.Path == "" {
		return Task{ID: item.Line, SerialOnly: true, ShapeReason: "missing path"}
	}
	return Task{ID: item.Path, Item: item, Files: []string{item.Path}, ShapeReason: "exact path"}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./worker`
Expected: PASS

**Step 5: Commit**

```bash
git add worker/shaper.go worker/shaper_test.go
git commit -m "feat: add strict task shaping"
```

### Task 3: Add deterministic companion-file shaping rules

**Files:**
- Modify: `worker/shaper.go`
- Modify: `worker/shaper_test.go`

**Step 1: Write the failing tests**

```go
func TestShapeTaskClaimsCompanionTestFileForGoSource(t *testing.T) {
	item := ChecklistItem{Path: "worker/foo.go"}
	task := ShapeChecklistItem(item)
	if len(task.Files) != 2 {
		t.Fatal("expected source and companion test file claims")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./worker`
Expected: FAIL because shaping only claims the exact path today.

**Step 3: Write the minimal implementation**

```go
// for .go source files, include same-package *_test.go when deterministically known
```

**Step 4: Run tests to verify they pass**

Run: `go test ./worker`
Expected: PASS

**Step 5: Commit**

```bash
git add worker/shaper.go worker/shaper_test.go
git commit -m "feat: add deterministic companion-file shaping rules"
```

### Task 4: Route only explicitly shaped tasks into the live parallel lane

**Files:**
- Modify: `worker/child_batch.go`
- Modify: `worker/worker_parallel_test.go`

**Step 1: Write the failing tests**

```go
func TestCollectChildBatchKeepsUnshapedTaskSerial(t *testing.T) {
	// build one shaped task and one unshaped task
	// assert the unshaped task never contributes to max parallelism
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./worker`
Expected: FAIL because the live batch path still builds claim sets directly from item paths.

**Step 3: Write the minimal implementation**

```go
// use ShapeChecklistItem before scheduling
// only tasks with explicit claims and not serial_only may enter parallelTasks
```

**Step 4: Run tests to verify they pass**

Run: `go test ./worker`
Expected: PASS

**Step 5: Commit**

```bash
git add worker/child_batch.go worker/worker_parallel_test.go
git commit -m "feat: enforce strict shaping in live child scheduling"
```

### Task 5: Track serial fallbacks and claim conflicts in worker state

**Files:**
- Modify: `state/state.go`
- Modify: `worker/worker.go`
- Create: `worker/scheduler_metrics_test.go`

**Step 1: Write the failing tests**

```go
func TestWorkerTracksSerialFallbacksAndClaimConflicts(t *testing.T) {
	// drive one serial fallback and one overlap
	// assert counters update in worker state
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./worker ./state`
Expected: FAIL with missing counters/state fields.

**Step 3: Write the minimal implementation**

```go
type WorkerState struct {
	// existing fields...
	SerialFallbackCount int `json:"serial_fallback_count,omitempty"`
	ClaimConflictCount  int `json:"claim_conflict_count,omitempty"`
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./worker ./state`
Expected: PASS

**Step 5: Commit**

```bash
git add state/state.go worker/worker.go worker/scheduler_metrics_test.go
git commit -m "feat: track serial fallbacks and claim conflicts"
```

### Task 6: Surface shaping safety metrics in operator views

**Files:**
- Modify: `ui/dashboard.go`
- Modify: `ui/tui.go`
- Modify: `cmd/run.go`
- Modify: `cmd/status.go`
- Create: `ui/shaping_metrics_test.go`

**Step 1: Write the failing tests**

```go
func TestWorkerSafetyLabel(t *testing.T) {
	info := WorkerInfo{SerialFallbacks: 3, ClaimConflicts: 2}
	if got := workerSafetyLabel(info); got != "serial 3 | conflicts 2" {
		t.Fatalf("unexpected label: %s", got)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./ui`
Expected: FAIL with missing metrics fields/label.

**Step 3: Write the minimal implementation**

```go
type WorkerInfo struct {
	// existing fields...
	SerialFallbacks int
	ClaimConflicts  int
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./ui`
Expected: PASS

**Step 5: Commit**

```bash
git add ui/dashboard.go ui/tui.go cmd/run.go cmd/status.go ui/shaping_metrics_test.go
git commit -m "feat: show shaping safety metrics in status views"
```

### Task 7: Run full verification and prepare final integration

**Files:**
- Modify: `docs/plans/2026-03-10-strict-task-shaping-design.md`
- Modify: `docs/plans/2026-03-10-strict-task-shaping.md`

**Step 1: Run focused suites**

Run: `go test ./worker ./state ./ui`
Expected: PASS

**Step 2: Run full suite**

Run: `go test ./...`
Expected: PASS

**Step 3: Capture final phase notes**

```markdown
- explicit pre-dispatch claim sets enforced
- unshaped tasks forced serial
- safety metrics exposed in status views
```

**Step 4: Review diff**

Run: `git diff --stat origin/main...HEAD`
Expected: shows only strict task shaping, safety metrics, tests, and docs.

**Step 5: Commit**

```bash
git add docs/plans/2026-03-10-strict-task-shaping-design.md docs/plans/2026-03-10-strict-task-shaping.md
git commit -m "docs: record verification notes for strict task shaping"
```
