# Ralph Design: Parent Workers With Parallel Child Agents

## Summary

Ralph's current execution model runs one agent per worker iteration. This keeps the implementation simple, but it leaves wall-clock performance on the table because each worker can only make progress on one task batch at a time.

This design changes each configured worker into a long-lived parent coordinator that dispatches multiple child agents in parallel. The parent carries only compact machine state and can be reset when its context grows too large. Child agents share a single workspace, but they may only run on non-overlapping file sets enforced by strict admission control.

## Goals

1. Reduce total wall-clock time for large checklist runs.
2. Let one configured worker complete multiple independent tasks concurrently.
3. Keep reset and resume behavior deterministic by persisting machine state only.
4. Avoid the I/O cost and operational complexity of per-child worktrees.
5. Preserve checklist ownership boundaries between top-level workers.

## Non-Goals

1. Preserving full parent conversational history across resets.
2. Building backend-specific session continuity as a first step.
3. Parallelizing tasks whose file impact cannot be bounded safely.

## Recommended Approach

Each Ralph worker becomes a parent coordinator with a bounded pool of child agent slots. The parent owns a disjoint subset of checklist items based on the existing worker pattern. Inside that boundary, it repeatedly:

1. Reads pending items it owns.
2. Shapes them into dispatchable tasks.
3. Launches child agents for tasks with non-overlapping file sets.
4. Reconciles completed child results into shared state.
5. Commits accepted changes in batches.
6. Resets itself from compact machine state when configured thresholds are hit.

This keeps the external configuration model recognizable while creating a second layer of concurrency inside each worker.

## Architecture

### Parent worker

The parent worker is a long-lived coordinator process for one configured worker. It is responsible for:

1. Loading and maintaining the worker-local task queue.
2. Tracking in-flight children.
3. Enforcing file-claim exclusivity.
4. Accepting or rejecting child results.
5. Batching commits and state persistence.
6. Triggering and recovering from parent resets.

The parent does not rely on long conversational memory. Its durable state is explicit and serializable.

### Child agents

Each child agent is a narrowly scoped backend run responsible for one item or a very small compatible bundle. A child receives:

1. The task or task bundle it owns.
2. Its allowed file set.
3. The local rules for checklist updates and output formatting.
4. A prohibition against touching files outside its assigned claims.

Child agents are disposable. They may fail independently without taking down the parent.

## Parent State Model

The parent reset boundary carries machine state only. Minimum state to persist:

1. `pending`
2. `in_flight`
3. `completed`
4. `failed`
5. `retry_count_by_item`
6. `claimed_files`
7. `last_commit`
8. `parent_generation`

This state must be sufficient to recreate the queue, avoid duplicate dispatch, and continue after reset without replaying old logs or reasoning.

## Execution Flow

For one parent worker cycle:

1. Load pending checklist items owned by the worker.
2. Partition them into dispatchable tasks.
3. For each task, estimate or derive its file footprint.
4. Admit the task only if none of its files are currently claimed.
5. Launch the child agent and mark its files as claimed.
6. On child completion, reconcile its result, release claims, and update queue state.
7. Dispatch the next compatible task immediately.
8. Periodically batch successful work into a parent-controlled commit.
9. If a reset threshold is reached, persist parent state and start a fresh parent generation from that state.

## Shared Workspace Coordination

All child agents share one workspace. Safety depends on hard scheduling rules, not prompt instructions alone.

### File claims

The parent maintains a file-claim table. A task may launch only when its entire claimed file set is free. Claimed files stay reserved until the child finishes and the parent reconciles the result.

### Dispatch lanes

The scheduler should maintain at least two effective lanes:

1. Parallel lane for tasks with a bounded, non-overlapping file footprint.
2. Serial lane for tasks whose footprint is broad, ambiguous, or historically conflict-prone.

This keeps shared-workspace parallelism productive without allowing unsafe overlap.

## Child Result Contract

Child results should be structured and machine-readable. Minimum result fields:

1. Items attempted
2. Items completed
3. Files changed
4. Checklist lines updated
5. Exit status
6. Short failure reason

The parent should schedule and recover from these summaries, not from parsing large free-form logs whenever possible.

## Reset Policy

Parent resets are an explicit lifecycle mechanism. They should be triggered by configurable thresholds such as:

1. Estimated parent token/context size
2. Parent lifetime in minutes
3. Number of child completions since last reset

On reset:

1. No full conversational history is preserved.
2. Compact machine state is saved.
3. A fresh parent generation is started from that state.
4. In-flight children must be reconciled or waited out before reset completes.

## Failure Handling

Failures should be isolated and non-blocking:

1. A child failure does not fail the parent worker.
2. Failed tasks return to the queue with capped retries.
3. Repeated failures move to quarantine so they do not starve throughput.
4. Reset preserves retry counters and quarantine state.
5. Unsafe or ambiguous tasks can be forced into the serial lane.

## Commit Strategy

Per-child commits would serialize the shared workspace too often. The parent should own integration and commit in batches. This reduces time spent waiting on git operations and makes the top-level workflow easier to reason about.

The parent remains the only actor that decides when accepted work becomes a shared commit boundary.

## Configuration Impact

Likely additions to `ralph.toml`:

1. `worker_parallelism` or per-worker child slot count
2. Parent reset thresholds
3. Commit batch thresholds
4. Serial-lane fallback controls
5. Retry and quarantine limits for child tasks

The exact config shape belongs in implementation planning, but these controls are required by the design.

## Observability

The new model needs metrics that show whether it is actually reducing wall-clock time. At minimum, Ralph should track:

1. Child slot utilization
2. Queue wait time per task
3. Child run duration
4. Claimed-file conflicts
5. Serial-lane fallback count
6. Items completed per minute
7. Parent reset count and reset reason

## Testing Strategy

Priority should be scheduler correctness and recovery behavior:

1. Overlapping file sets are never dispatched concurrently.
2. File claims are always released after reconciliation.
3. Child failures do not corrupt the queue.
4. Parent reset preserves machine state exactly enough to continue safely.
5. Batched commits do not lose accepted child results.

A simulation-style scheduler test harness is recommended. Fake child tasks with declared file footprints and durations can validate dispatch order, contention handling, and throughput behavior without invoking real backends.

## Trade-Offs

### Benefits

1. Better wall-clock performance from intra-worker parallelism.
2. Lower idle time while waiting for one large agent run to finish.
3. Deterministic reset behavior because state is explicit and compact.
4. Lower I/O overhead than a worktree-per-child design.

### Costs

1. More scheduler complexity inside each worker.
2. Accurate file-footprint estimation becomes important.
3. Shared-workspace safety depends on strong admission control.
4. Parent-controlled batching adds integration logic and new failure modes.

## Recommendation

Implement the parent-worker/parallel-child-agent model in a shared workspace with strict file-claim admission control, parent-batched commits, and reset from compact machine state only.

This is the best fit for the stated priority of reducing wall-clock time while avoiding excessive I/O from many worktrees.
