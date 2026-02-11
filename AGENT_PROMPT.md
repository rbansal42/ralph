# Ralph — Parallel Coding Agent Runner

## What is Ralph?

Ralph is a Go CLI tool that orchestrates multiple AI coding agents working in parallel. You give it a checklist of work items, a prompt template describing the task, and worker definitions that partition the checklist — ralph then spawns headless AI sessions (via `opencode run` or `claude -p`) to work through everything concurrently.

**Repository:** https://github.com/rbansal42/ralph  
**Binary location:** `/Volumes/Code/ralph/` (source) and `ralph` (installed to PATH)

## Core Concepts

### The Three Files

Ralph needs three things to operate:

1. **`ralph.toml`** — Configuration file defining backend, model, workers, and settings
2. **Checklist file** — Markdown file with work items, each prefixed with a status marker
3. **Prompt file** — Instructions for the AI agent on how to process each batch of items

### Checklist Format

Each line is a work item with a status prefix:

```markdown
- [~] path/to/file.php — PARTIAL: needs X, Y, Z
- [x] path/to/file.php — MATCH: implemented in equivalent.py
- [s] path/to/file.php — SKIP: framework boilerplate
```

- `[~]` = pending work (workers pick these up)
- `[x]` = completed
- `[s]` = skipped

### Workers

Workers are named partitions of the checklist. Each worker has a **pattern** (pipe-delimited substrings) that determines which checklist lines it owns. Workers run as parallel goroutines, each spawning its own AI agent sessions.

Example: 3 workers splitting a PHP-to-Python migration:

```toml
[[worker]]
name = "TASKS"
pattern = "Console/Commands|app/Tasks/"

[[worker]]
name = "MODELS"
pattern = "app/Models|app/Observers/|app/Traits/"

[[worker]]
name = "REST"
pattern = "app/Transformers/|app/Validators/|app/Repositories/"
```

Patterns must be mutually exclusive — each `[~]` line should match exactly one worker.

### The Loop

Each worker repeats this cycle until all its items are done:

1. Read checklist, filter `[~]` items matching this worker's pattern
2. Take first N items (configurable via `items_per_iteration`)
3. Build prompt = base prompt template + dynamic iteration context + item list
4. Spawn AI agent (`opencode run` or `claude -p`) with the prompt piped to stdin
5. Agent reads source files, implements changes, updates checklist `[~]` → `[x]`
6. After agent exits, ralph commits all changes atomically (shared git mutex)
7. Record iteration stats (time, items completed, exit code) to state file
8. Cooldown, then repeat

The agent does NOT run `git commit` — ralph handles that after the agent exits, using a shared mutex so parallel workers don't conflict.

## Installation

```bash
# From source
cd /Volumes/Code/ralph
go install .

# Verify
ralph --help
```

## CLI Reference

### `ralph run`

Start workers to process checklist items.

```bash
ralph run                     # start all workers in parallel
ralph run --worker 1          # start only worker 1
ralph run --model opus        # Anthropic alias
ralph run --model gpt5        # OpenAI alias
ralph run --model openai/gpt-5-mini
ralph run --dry-run           # show what each worker would do, don't execute
ralph run --backend claude    # override backend
```

**What happens on `ralph run`:**
1. Loads `ralph.toml`
2. Prints banner and status dashboard
3. Sets up permissions (auto-creates `opencode.json` for opencode backend)
4. Initializes token pool from config
5. Tests authentication (prompts for token interactively if failed)
6. Loads state from `ralph_state.json` (resumes from last iteration)
7. Spawns workers as goroutines (or single worker if `--worker N`)
8. Prints progress every 60 seconds
9. On Ctrl+C: finishes current iteration, saves state, exits cleanly
10. On double Ctrl+C: force kills immediately

### `ralph status`

Show progress without running workers.

```bash
ralph status
```

Outputs a dashboard with checklist counts, per-worker remaining items, iteration history, and estimated time remaining.

### `ralph auth`

Test and configure authentication.

```bash
ralph auth
ralph auth --backend opencode --model anthropic/claude-opus-4-6
```

Tests the configured backend by sending a quick probe. If auth fails, prints setup instructions and prompts for a token interactively.

### `ralph token`

Manage API tokens for switching between accounts.

```bash
ralph token list              # show all configured tokens (keys masked)
ralph token switch work       # switch to the "work" token
ralph token add temp sk-...   # add + persist token in ralph.toml
```

### `ralph generate`

Interactive setup wizard. Walks you through creating `ralph.toml`, a checklist file, and a prompt template.

```bash
ralph generate
```

The wizard:
1. Asks which backend (opencode/claude)
2. Asks for model
3. Creates or links a checklist file
4. Creates a prompt template (can use AI to generate one from a task description)
5. Configures workers (name + pattern for each)
6. Writes `ralph.toml`

## Configuration Reference

### `ralph.toml`

```toml
# Backend: "opencode" or "claude"
backend = "opencode"

# Path to the checklist file (relative to project root)
checklist = "REVIEW_CHECKLIST.md"

# Path to the prompt template file
prompt = "ralph_prompt.md"

# Model ID or alias (opus, sonnet, gpt5, o3, o4mini)
model = "anthropic/claude-opus-4-6"

# Pause between iterations
cooldown = "10s"

# Max iterations per worker before stopping
max_iterations = 80

# How many checklist items to include in each iteration's prompt
items_per_iteration = 5

# State file for resume support
state_file = "ralph_state.json"

# Directories outside the project that agents need to read
# (ralph auto-configures opencode.json permissions for these)
external_dirs = ["/Volumes/Code/some_other_project"]

# Token pool — multiple API keys for rotation on rate limiting
[[token]]
name = "personal"
key = "sk-ant-api03-..."

[[token]]
name = "work"
key = "sk-ant-api03-..."

# Worker definitions (at least one required)
[[worker]]
name = "TASKS"
pattern = "app/Tasks/|Console/Commands"

[[worker]]
name = "MODELS"
pattern = "app/Models|app/Observers/"
```

## Permission Handling

### opencode backend

Ralph automatically creates/updates `opencode.json` in the project directory on `ralph run`:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "permission": {
    "*": "allow",
    "external_directory": {
      "/Volumes/Code/other_project/**": "allow",
      "*": "allow"
    }
  }
}
```

This grants the agent full tool permissions and access to external directories listed in `external_dirs`.

**Runtime permission detection:** If the agent encounters a permission block during an iteration, ralph detects it in the output, prompts the user to approve the directory, adds it to `opencode.json`, and the next iteration will have access.

### claude backend

Uses `--permission-mode bypassPermissions` flag — no additional setup needed.

## Token Management

Ralph supports a pool of API tokens for handling rate limits or switching between accounts mid-run.

**Auto-rotation:** After each iteration, ralph scans the agent's output for rate limit signals (HTTP 429, "rate limit", "quota exceeded", etc.). If detected and multiple tokens are configured, it automatically rotates to the next token.

**Manual switching:** Use `ralph token switch <name>` to switch between configured tokens.

### Live Hotkeys During `ralph run`

When multiple tokens are configured, ralph enables live hotkey support during a run:

| Key | Action |
|-----|--------|
| `t` | Rotate to the next token in the pool — new iterations will use it |
| `s` | Print current token pool status (which token is active, masked keys) |

Hotkeys require a real terminal (not piped stdin). If stdin is not a TTY, hotkeys are silently disabled. The terminal is set to raw mode to read single keypresses and restored on exit.

## State & Resume

Ralph saves progress to `ralph_state.json` after every iteration. If you stop ralph (Ctrl+C, crash, or manual kill), running `ralph run` again picks up where each worker left off — it reads the last iteration number from the state file and continues from there.

## Prompt Template

The prompt template (`ralph_prompt.md`) contains your instructions to the AI agent. Ralph appends dynamic context to it per iteration:

```markdown
{your prompt template contents}

---

## THIS IS ITERATION #3 — WORKER 1 (TASKS)

You are Worker 1 of 3 parallel workers. You ONLY work on **TASKS** items.
There are **175** items remaining in your section.

### IMPORTANT: SCOPE RESTRICTION
You MUST only work on the items listed below. Do NOT touch items outside your section.

### Your items for this iteration:

- [~] app/Tasks/Albums/CreateAlbum.php — PARTIAL: ...
- [~] app/Tasks/Albums/InviteUser.php — MISSING: ...

Work on these items now. There is NO time limit.

**DO NOT run git add or git commit.** The runner handles commits externally.

Output RALPH_SUMMARY at the end.
```

Your prompt template should explain the task, provide quality guidelines, and tell the agent how to process each item. Ralph handles the per-iteration dynamic context automatically.

## Project Architecture

```
ralph/
├── main.go                    # Entry point
├── cmd/
│   ├── root.go                # Root cobra command, global flags
│   ├── run.go                 # ralph run — main worker orchestration
│   ├── status.go              # ralph status — progress display
│   ├── auth.go                # ralph auth — authentication testing
│   ├── token.go               # ralph token — token pool management
│   ├── generate.go            # ralph generate — setup wizard
│   └── terminal_unix.go       # Raw terminal mode for hotkey support (unix only)
├── config/
│   └── config.go              # ralph.toml parsing (TOML → Config struct)
├── backend/
│   ├── backend.go             # Backend interface + factory
│   ├── opencode.go            # opencode run implementation
│   └── claude.go              # claude -p implementation
├── worker/
│   ├── worker.go              # Worker goroutine loop + git commit
│   ├── checklist.go           # Checklist parser ([~]/[x]/[s] items)
│   └── prompt.go              # Prompt builder (template + dynamic context)
├── permission/
│   ├── permission.go          # opencode.json auto-creation, permission detection
│   └── token.go               # Token pool manager, rate limit detection
├── state/
│   └── state.go               # JSON state persistence
├── ui/
│   └── dashboard.go           # Terminal status display
├── go.mod
└── go.sum
```

## Quick Start for a New Project

```bash
# 1. Navigate to your project
cd /path/to/your/project

# 2. Run the setup wizard
ralph generate

# 3. Add items to your checklist
# Edit the checklist file with [~] items

# 4. Write/edit your prompt template
# Describe the task, quality requirements, and per-item workflow

# 5. Test with dry-run
ralph run --dry-run

# 6. Test with a single worker
ralph run --worker 1

# 7. Run all workers
ralph run
```

## Example: Using Ralph for an Existing Migration

This is how ralph is configured for a Laravel-to-FastAPI migration at `/Volumes/Code/fastapi/hudle-backend`:

```toml
backend = "opencode"
checklist = "REVIEW_CHECKLIST.md"
prompt = "ralph_prompt.md"
model = "anthropic/claude-opus-4-6"
cooldown = "10s"
max_iterations = 80
items_per_iteration = 5
state_file = "ralph_state.json"
external_dirs = ["/Volumes/Code/hudle_backend"]

[[worker]]
name = "TASKS"
pattern = "Console/Commands|app/Tasks/"

[[worker]]
name = "MODELS"
pattern = "app/Models|app/Observers/|app/Traits/|app/Presenters/"

[[worker]]
name = "REST"
pattern = "app/Transformers/|app/Validators/|app/Repositories/|app/Utilities/|app/Notifications/|app/Providers/|app/ViewComposers/"
```

The `external_dirs` entry grants agent access to the PHP source code at `/Volumes/Code/hudle_backend` so it can read the original Laravel files while writing Python equivalents in the FastAPI project.
