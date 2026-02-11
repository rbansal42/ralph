# Ralph — Parallel Coding Agent Runner

## Overview

Ralph is a Go CLI tool that manages parallel coding agent workers. It takes a checklist of work items, a prompt template, and worker definitions, then spawns multiple headless AI coding sessions (via `opencode run` or `claude -p`) to work through the checklist concurrently.

Named after Ralph Wiggum from The Simpsons ("I'm helping!").

## Problem

Running bulk automated coding tasks (migrations, refactors, large-scale changes) with AI agents requires:
- Partitioning work across parallel workers
- Managing concurrent git commits without conflicts
- Tracking progress across hundreds of items
- Handling auth, retries, and graceful shutdown
- Resuming after interruptions

Bash scripts are fragile for this: escaping issues, unreliable locks, no proper concurrency.

## Design Goals

1. **General-purpose** — not tied to any specific migration; works with any checklist + prompt
2. **Multi-backend** — supports both `opencode` and `claude` CLI tools
3. **Parallel by default** — N workers as goroutines, shared git mutex
4. **Resumable** — state persisted to JSON, picks up where it left off
5. **Observable** — live terminal dashboard showing all workers' status
6. **Guided setup** — `ralph generate` walks users through creating config + checklist + prompt

## CLI Interface

```
ralph run                     # start all workers
ralph run --worker 1          # start only worker 1
ralph run --model opus        # override model
ralph run --dry-run           # show what each worker would do
ralph status                  # show current progress from state file
ralph stop                    # graceful shutdown (signal running instance)
ralph auth                    # test and configure authentication
ralph generate                # interactive setup wizard
```

## Configuration

`ralph.toml` at project root:

```toml
backend = "opencode"                       # or "claude"
checklist = "REVIEW_CHECKLIST.md"
prompt = "ralph_prompt.md"
model = "anthropic/claude-opus-4-6"
cooldown = "10s"
max_iterations = 80
items_per_iteration = 5

[[worker]]
name = "TASKS"
pattern = "app/Tasks/|Console/Commands"

[[worker]]
name = "MODELS"
pattern = "app/Models|app/Observers/|app/Traits/|app/Presenters/"

[[worker]]
name = "REST"
pattern = "app/Transformers/|app/Validators/|app/Repositories/"
```

### Model aliases

Short names resolve to full model IDs:
- `opus` -> `anthropic/claude-opus-4-6`
- `sonnet` -> `anthropic/claude-sonnet-4-5`

## Agent Backends

### Interface

```go
type Backend interface {
    Name() string
    CheckAuth(model string) error
    RunPrompt(ctx context.Context, promptFile string, workdir string, model string) (output string, exitCode int, err error)
    AuthGuide() string  // human-readable auth fix instructions
}
```

### opencode backend

- Invokes: `opencode run -m <model> < prompt.md`
- Auth check: sends "respond with RALPH_OK", checks for error patterns
- Requires: `opencode` in PATH, valid API key or Zen auth
- Project needs `opencode.json` with `permission: "allow"` and `external_directory: "*": "allow"` for external file access

### claude backend

- Invokes: `claude -p --model <model> --permission-mode bypassPermissions < prompt.md`
- Auth check: same quick test
- Requires: `claude` in PATH, `CLAUDE_CODE_OAUTH_TOKEN` or `claude setup-token`

### Auth flow

On `ralph run` or `ralph auth`:

1. Call `backend.CheckAuth(model)`
2. If it fails, print `backend.AuthGuide()` with step-by-step instructions
3. Prompt user to paste a token interactively
4. Set as env var for child processes (session only, not persisted to disk)
5. Retry check
6. If still fails, exit with clear error

## Checklist Format

Markdown file with one item per line, status prefix:

```markdown
- [x] path/to/file.php — MATCH: implemented in equivalent.py
- [s] path/to/file.php — SKIP: framework boilerplate
- [~] path/to/file.php — PARTIAL: needs X, Y, Z
```

Statuses:
- `[~]` = pending work (picked up by workers)
- `[x]` = completed
- `[s]` = skipped

Workers filter `[~]` lines by their grep pattern to get their partition.

## Worker Loop

Each worker runs as a goroutine:

```
for iteration := 1; iteration <= maxIterations; iteration++ {
    // 1. Read checklist, filter by pattern, get remaining [~] items
    items := checklist.GetPending(worker.pattern)
    if len(items) == 0 { break }

    // 2. Build prompt (template + first N items)
    promptFile := prompt.Build(worker, items[:min(N, len(items))], iteration)

    // 3. Spawn agent
    startTime := time.Now()
    output, exitCode, err := backend.RunPrompt(ctx, promptFile, workdir, model)
    elapsed := time.Since(startTime)

    // 4. Count completed (diff checklist before/after)
    newRemaining := checklist.CountPending(worker.pattern)
    completed := previousRemaining - newRemaining

    // 5. Atomic git commit (shared mutex)
    gitMutex.Lock()
    exec("git", "add", "-A")
    exec("git", "commit", "-m", fmt.Sprintf("Ralph W%d iteration %d: ...", worker.id, iteration))
    gitMutex.Unlock()

    // 6. Update state
    state.RecordIteration(worker.name, iteration, completed, elapsed)

    // 7. Log
    log.Printf("[Worker %d] Iteration %d: %d items | %s | exit %d", ...)

    // 8. Cooldown
    time.Sleep(cooldown)
}
```

### Git commit strategy

All workers share a `sync.Mutex`. When a worker is ready to commit:
1. Lock mutex
2. `git add -A` (stages everything in working tree)
3. `git commit -m "..."`
4. Unlock mutex

This is safe because:
- Workers touch different files (different checklist sections, different code modules)
- `git add -A` captures whatever is in the working tree at lock time
- The mutex ensures only one commit happens at a time

Note: the agent itself should NOT run `git commit`. The prompt instructs the agent to make file changes and update the checklist, but ralph handles the commit externally after the agent exits.

## Prompt Builder

Takes the base prompt template and appends dynamic context per iteration:

```markdown
{contents of ralph_prompt.md}

---

## THIS IS ITERATION #3 — WORKER 1 (TASKS)

You are Worker 1 of 3 parallel workers. You ONLY work on **TASKS** items.
There are **175** items remaining in your section.

### IMPORTANT: SCOPE RESTRICTION
You MUST only work on the items listed below. Do NOT touch items outside your section.

### Your items for this iteration:

- [~] app/Tasks/Albums/CreateAlbum.php — PARTIAL: ...
- [~] app/Tasks/Albums/InviteUser.php — MISSING: ...
- [~] app/Tasks/Auth/Login.php — PARTIAL: ...

Work on these items now. There is NO time limit.

**DO NOT run git add or git commit.** The runner handles commits externally.

Output RALPH_SUMMARY at the end.
```

Key change from bash version: the prompt tells the agent NOT to commit. Ralph commits after the agent exits.

## State Persistence

`ralph_state.json`:

```json
{
  "started_at": "2026-02-11T14:00:00Z",
  "last_updated": "2026-02-11T14:45:00Z",
  "workers": {
    "TASKS": {
      "iteration": 5,
      "completed": 18,
      "skipped": 1,
      "history": [
        {"iteration": 1, "completed": 4, "elapsed_s": 423, "exit_code": 0},
        {"iteration": 2, "completed": 3, "elapsed_s": 512, "exit_code": 0}
      ]
    },
    "MODELS": { ... },
    "REST": { ... }
  }
}
```

On startup, ralph reads the state file to determine where each worker left off.

## `ralph generate` — Interactive Setup Wizard

Guided flow:

1. **Backend selection** — opencode or claude
2. **Model selection** — with sensible default
3. **Checklist** — point to existing file or create empty one
4. **Prompt** — point to existing file, or describe the task and have AI generate a starter prompt (uses configured backend to generate)
5. **Workers** — how many, name + pattern for each. Validates no overlaps.
6. **Write files** — creates `ralph.toml`, checklist (if new), prompt (if generated)
7. **Next steps** — prints what to do next

## Live Dashboard

During `ralph run`, a terminal dashboard updates every second:

```
  Ralph — 3 Workers Running                    14:32:07

  ╔═══════════════════════════════════════════════╗
  ║  MATCH [x]: 849    SKIP [s]: 155             ║
  ║  REMAIN[~]: 406    Progress: 71%             ║
  ╠═══════════════════════════════════════════════╣
  ║  W1 TASKS  : 175 remaining | iter 3 | 7m23s  ║
  ║  W2 MODELS : 101 remaining | iter 2 | 5m41s  ║
  ║  W3 REST   : 126 remaining | iter 2 | 6m12s  ║
  ╚═══════════════════════════════════════════════╝

  [W1] Iteration 3: working on app/Tasks/Booking/BookVenue.php...
  [W2] Iteration 2: completed 4 items in 5m41s
  [W3] Iteration 2: working on app/Transformers/BookingTransformer.php...
```

Uses lipgloss for styling. Non-interactive (no bubbletea needed) — just periodic re-renders.

## Graceful Shutdown

On SIGINT/SIGTERM:
1. Set `shutdown` flag (atomic bool)
2. Each worker checks flag at start of each iteration
3. Workers finish current iteration (don't kill the agent mid-work)
4. Save final state
5. Exit cleanly

On double Ctrl+C: force kill all child processes immediately.

## Project Structure

```
ralph/
├── main.go
├── cmd/
│   ├── root.go
│   ├── run.go
│   ├── status.go
│   ├── stop.go
│   ├── auth.go
│   └── generate.go
├── config/
│   └── config.go
├── backend/
│   ├── backend.go
│   ├── opencode.go
│   └── claude.go
├── worker/
│   ├── worker.go
│   ├── checklist.go
│   └── prompt.go
├── state/
│   └── state.go
├── ui/
│   └── dashboard.go
├── ralph.toml            (example config, gitignored in user projects)
├── docs/
│   └── plans/
│       └── 2026-02-11-ralph-design.md
├── go.mod
└── go.sum
```

## Dependencies

- `github.com/spf13/cobra` — CLI framework
- `github.com/BurntSushi/toml` — TOML config parsing
- `github.com/charmbracelet/lipgloss` — terminal styling
- Standard library for everything else (os/exec, sync, encoding/json, regexp)

## Future Ideas (Not in V1)

- Web dashboard via `ralph serve`
- Multiple checklist file support
- Worker auto-scaling (spawn more workers if one finishes early)
- Cost tracking (parse token usage from agent output)
- Retry logic for failed iterations
- `ralph review` — validate completed items against source
