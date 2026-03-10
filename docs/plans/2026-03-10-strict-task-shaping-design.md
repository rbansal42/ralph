# Ralph Design: Strict Task Shaping for Shared-Workspace Safety

## Summary

Ralph currently allows child-batch parallelism inside a worker, but the runtime still treats most checklist items as if their claimed file set were just the checklist item path. That is a safe first approximation, but it leaves two gaps:

1. unshaped tasks can still look parallel-safe when they are not
2. broader-impact tasks do not have a deterministic parent-owned claim set before dispatch

This phase makes scheduling strict and explicit. A task may run in the parallel lane only when Ralph can shape it into an explicit claimed file set before the child starts. If that cannot be done, the task is routed to the serial lane by default.

## Goals

1. Make explicit claimed file sets the only admission path to parallel execution.
2. Add deterministic worker-side task shaping rules that run before dispatch.
3. Route all unshaped or ambiguous tasks to the serial lane automatically.
4. Surface shaping reasons and serial fallbacks in operator-visible status.
5. Close the remaining shared-workspace scheduling gap tracked in issue `#11`.

## Non-Goals

1. Extending checklist syntax to declare claimed files.
2. Allowing children to declare or widen claims after launch.
3. Building a full semantic impact analyzer in this phase.

## Chosen Policy

Parallel execution is allowed only when Ralph already knows the complete claim set before dispatch.

1. A shaped task with a non-empty explicit claim set may enter the parallel lane.
2. A shaped task with overlapping claims must wait until those claims are free.
3. Any unshaped task or task with an empty/unknown claim set is forced into the serial lane.
4. There is no runtime upgrade from serial to parallel after a child starts.

This makes the parent scheduler the sole source of truth for shared-workspace safety.

## Architecture

### Task shaping stage

Before scheduling, Ralph converts each checklist item into a schedulable task record:

1. source checklist item
2. task ID
3. explicit claimed file set
4. `serial_only` flag
5. shaping reason

The shaping stage is deterministic and runs entirely inside Ralph.

### Worker-side shaping rules

Claim sets come from worker-side shaping rules, not from checklist syntax.

The initial rule set should stay simple:

1. exact item path claim
2. optional deterministic companion-file expansions owned by Ralph
3. explicit serial fallback when no rule applies

This gives us strict safety without forcing the checklist format to change.

### Scheduler behavior

The scheduler becomes strict:

1. parallel lane only for tasks with non-empty explicit claim sets
2. serial lane for `serial_only` tasks and any task without a claim set
3. overlapping claim sets block dispatch until claims are released

Child behavior does not affect admission. The parent decides before launch.

## Config Direction

The configuration should stay parent-owned and minimal. The likely additions are:

1. worker-level shaping mode
2. optional deterministic companion-file rules
3. operator-visible counters for serial fallback and claim conflicts

Checklist files remain unchanged in this phase.

## Failure Handling

The shaping layer should fail closed:

1. if shaping returns no claim set, route to serial
2. if shaping produces overlapping claims, queue until the overlap clears
3. if shaping cannot confidently classify a task, mark `serial_only`
4. if a child fails, existing batch failure rules still apply

This phase should never make an ambiguous task more parallel than before.

## Observability

Status and logs should expose:

1. shaped-parallel task count
2. serial fallback count
3. claim-conflict count
4. per-task shaping reason when useful for debugging

Operators need to see when safety is trading off against throughput.

## Testing Strategy

The critical tests in this phase are scheduler correctness tests:

1. shaped tasks with disjoint explicit claim sets run in parallel
2. overlapping shaped tasks do not run concurrently
3. unshaped tasks always route to the serial lane
4. companion-file shaping rules produce the expected explicit claim sets
5. status surfaces report serial fallbacks and claim conflicts

Integration tests should cover mixed workloads where some tasks are parallel-safe and some are forced serial.

## Recommendation

Implement a strict parent-owned task-shaping stage with serial fallback by default. Parallelism should only happen for tasks whose explicit claim set is known before launch. This closes the remaining shared-workspace scheduling hole without expanding the checklist format or trusting child behavior after dispatch.
