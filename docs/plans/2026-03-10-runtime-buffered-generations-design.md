# Ralph Design: Buffered Generations and Deferred Commit Flush

## Summary

Phase 1 introduced parallel child batches inside each worker, but several runtime controls remained structural only:

1. `commit_batch_size` exists in config but does not drive live durability.
2. `parent_reset_after_runs` exists in config but does not drive parent generations.
3. Parent generation state is persisted structurally but is not used in the live worker lifecycle.

This phase makes those controls real. Each worker becomes a generation-aware coordinator with a persisted accepted-results buffer that can survive multiple iterations and multiple parent generations. Resets no longer imply a commit. Completed work stays buffered until a normal future flush point is reached.

## Goals

1. Make `commit_batch_size` a live runtime control.
2. Make `parent_reset_after_runs` a live generation rollover control.
3. Persist enough buffer state to resume safely after resets or process restarts.
4. Preserve deferred commit semantics across generations.
5. Keep the worker loop safe in the shared-workspace model.

## Non-Goals

1. Forcing a commit on generation rollover.
2. Replacing the shared-workspace model with worktree-per-child isolation.
3. Expanding the scheduler into full multi-file impact analysis in this phase.

## Chosen Runtime Policy

The worker maintains a durable accepted-results buffer. Buffered results may include:

1. Fully completed items accepted by the parent.
2. Partial items that have not yet been completed and are not commit-eligible.
3. Metadata required to classify buffered work during the next generation.

`commit_batch_size` is the only normal commit trigger. `parent_reset_after_runs` rolls the generation and persists state, but does not force a flush. When a new generation starts:

1. Inherited completed results remain buffered.
2. Inherited partial results remain buffered.
3. Nothing is committed immediately just because the generation changed.
4. A later clean flush point may commit inherited completed work together with newly accepted completed work.

## Architecture

### Worker buffer state

Each worker needs persisted state beyond iteration history:

1. current parent generation
2. number of child runs executed in the current generation
3. buffered completed result records
4. buffered partial result records
5. pending commit count
6. reset count
7. last reset reason

The worker loop should treat this as part of its source of truth, not derived transient state.

### Accepted result records

The accepted-results buffer should store enough information to reconstruct commit eligibility and checklist reconciliation after restart:

1. checklist item path
2. files changed
3. whether the item is fully complete
4. whether the item is partial/incomplete
5. generation accepted

The buffer should not depend on logs or child reasoning history.

### Flush model

Flush should happen only when all of the following are true:

1. the buffer contains at least `commit_batch_size` completed results
2. the current batch finished cleanly
3. the worker is not in an abort state

When a flush happens:

1. parent updates the checklist for the completed items being flushed
2. parent performs a single git commit for the flushed accepted work
3. flushed completed records are removed from the buffer
4. partial buffered records remain until a later child run upgrades them to complete

## Generation Rollover

`parent_reset_after_runs` should count accepted child runs or completed worker cycles within the current generation. When the threshold is reached:

1. persist the buffer state
2. increment parent generation
3. reset the generation-local counter
4. record reset reason and count
5. continue execution without forcing a commit

Rollover is a coordination event only. Durability remains governed by the normal flush threshold.

## Failure Handling

The shared-workspace safety rules from phase 1 remain in force.

1. Mixed-success batches still abort the worker and do not commit.
2. Failed later batches must not force a commit of inherited buffered work.
3. Buffered completed work remains durable in state but not in git until a clean future flush point.
4. Partial buffered work must never be treated as commit-eligible.

If a worker restarts after a crash or intentional stop:

1. it reloads the generation
2. it reloads buffered completed and partial records
3. it resumes with the same pending commit count
4. it waits for a normal clean flush point before committing buffered completed work

## Observability

The status surfaces should expose:

1. current parent generation
2. buffered completed count
3. buffered partial count
4. pending commit count
5. reset count

This is required so operators can understand why work is intentionally uncommitted.

## Testing Strategy

The critical tests in this phase are state-transition tests:

1. accepted completed results persist across generation rollover without being committed
2. partial results persist across rollover and stay non-commit-eligible
3. later clean batches can flush inherited completed results when the threshold is reached
4. failed later batches do not commit inherited buffered results
5. restart from saved state recreates the same buffer and generation counters

Integration coverage should exercise:

1. multiple clean iterations before flush
2. rollover before flush
3. rollover plus later clean flush
4. mixed-success failure after inherited buffered work exists

## Recommendation

Implement buffered generations by making the accepted-results buffer durable across iterations and parent generations, while keeping commit durability controlled exclusively by the normal future flush threshold. This matches the chosen policy and preserves safe shared-workspace behavior without turning resets into implicit commits.
