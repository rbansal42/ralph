package worker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rahulvramesh/ralph/backend"
	"github.com/rahulvramesh/ralph/config"
	"github.com/rahulvramesh/ralph/state"
)

// Worker processes checklist items by spawning AI agent sessions in a loop.
// Each Worker runs as a goroutine, coordinating with others via shared mutexes.
type Worker struct {
	Num          int
	Name         string
	Pattern      string
	TotalWorkers int
	Backend      backend.Backend
	Config       *config.Config
	State        *state.State
	GitMutex     *sync.Mutex
	StateMutex   *sync.Mutex
	Shutdown     *atomic.Bool
	TokenManager interface{ RotateIfRateLimited(string) bool } // *permission.TokenManager
	PermHandler  func(output string) bool                      // called on permission block detection
	Notifier     interface {                                   // notify.Notifier — sends notifications on key events
		Send(event string, message string)
		ShouldNotify(event string) bool
	}
	Usage         *TokenUsage     // per-worker token usage
	GlobalUsage   *TokenUsage     // shared across all workers for budget checking
	ModifiedFiles map[string]bool // files modified by this worker (protected by GitMutex)
	CurrentStatus string
	StatusMutex   sync.RWMutex
	ActiveChildren int
	ChildrenMutex  sync.RWMutex
	CurrentGeneration int
	BufferedCompleted int
	BufferedPartial   int
	ResetCount        int
	BufferMutex       sync.RWMutex
}

// logf writes a formatted log line to stdout.
func (w *Worker) logf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stdout, format, args...)
}

// setStatus updates the worker's current status string in a thread-safe manner.
func (w *Worker) setStatus(status string) {
	w.StatusMutex.Lock()
	w.CurrentStatus = status
	w.StatusMutex.Unlock()
}

// GetStatus returns the worker's current status string in a thread-safe manner.
func (w *Worker) GetStatus() string {
	w.StatusMutex.RLock()
	defer w.StatusMutex.RUnlock()
	return w.CurrentStatus
}

// setActiveChildren updates the active child count in a thread-safe manner.
func (w *Worker) setActiveChildren(n int) {
	w.ChildrenMutex.Lock()
	w.ActiveChildren = n
	w.ChildrenMutex.Unlock()
}

// changeActiveChildren adjusts the active child count by delta.
func (w *Worker) changeActiveChildren(delta int) {
	w.ChildrenMutex.Lock()
	w.ActiveChildren += delta
	w.ChildrenMutex.Unlock()
}

// GetActiveChildren returns the current number of active child agents.
func (w *Worker) GetActiveChildren() int {
	w.ChildrenMutex.RLock()
	defer w.ChildrenMutex.RUnlock()
	return w.ActiveChildren
}

func (w *Worker) setBufferState(generation int, completed int, partial int, resetCount int) {
	w.BufferMutex.Lock()
	w.CurrentGeneration = generation
	w.BufferedCompleted = completed
	w.BufferedPartial = partial
	w.ResetCount = resetCount
	w.BufferMutex.Unlock()
}

func (w *Worker) GetBufferState() (generation int, completed int, partial int, resetCount int) {
	w.BufferMutex.RLock()
	defer w.BufferMutex.RUnlock()
	return w.CurrentGeneration, w.BufferedCompleted, w.BufferedPartial, w.ResetCount
}

func shouldCommitIteration(exitCode int, runErr error) bool {
	return exitCode == 0 && runErr == nil
}

func shouldRetryIteration(exitCode int, runErr error, completed int) bool {
	return (exitCode != 0 || runErr != nil) && completed == 0
}

func shouldAbortIteration(exitCode int, runErr error, completed int) bool {
	return (exitCode != 0 || runErr != nil) && completed > 0
}

func shouldResetGeneration(currentRuns int, threshold int) bool {
	return threshold > 0 && currentRuns >= threshold
}

// Run starts the worker loop. It blocks until all items are done, max
// iterations reached, or shutdown is signaled via the Shutdown flag or
// context cancellation.
func (w *Worker) Run(ctx context.Context) error {
	w.setStatus("idle")

	// Determine starting iteration from persisted state.
	w.StateMutex.Lock()
	ws, ok := w.State.Workers[w.Name]
	if !ok {
		ws = &state.WorkerState{ParentGeneration: 1}
		w.State.Workers[w.Name] = ws
	}
	startIter := ws.Iteration
	currentGeneration := ws.ParentGeneration
	if currentGeneration <= 0 {
		currentGeneration = 1
		ws.ParentGeneration = currentGeneration
	}
	generationRuns := ws.GenerationRuns
	buffer := ResultBufferFromWorkerState(ws)
	w.setBufferState(currentGeneration, buffer.CompletedCount(), buffer.PartialCount(), ws.ResetCount)
	w.StateMutex.Unlock()

	consecutiveStale := 0

	for i := startIter + 1; i <= w.Config.MaxIterations; i++ {
		// Check for shutdown signal.
		if w.Shutdown.Load() {
			w.logf("[W%d %s] Shutdown signaled, stopping\n", w.Num, w.Name)
			w.setStatus("shutdown")
			return nil
		}

		// Check context cancellation.
		select {
		case <-ctx.Done():
			w.logf("[W%d %s] Context cancelled, stopping\n", w.Num, w.Name)
			w.setStatus("shutdown")
			return ctx.Err()
		default:
		}

		// Get pending items for this worker's pattern.
		pending, err := GetPending(w.Config.Checklist, w.Pattern)
		if err != nil {
			return fmt.Errorf("[W%d %s] reading checklist: %w", w.Num, w.Name, err)
		}
		if len(pending) == 0 {
			w.logf("[W%d %s] No pending items remaining, done\n", w.Num, w.Name)
			w.setStatus("done")
			return nil
		}

		// Batch items for this iteration.
		var items []ChecklistItem
		if w.Config.BatchMode == "smart" {
			items = SelectBatchByComplexity(pending, w.Config.ComplexityBudget)
			w.logf("[W%d %s] Smart batch: %d items selected (budget: %d)\n",
				w.Num, w.Name, len(items), w.Config.ComplexityBudget)
		} else {
			items = pending
			if len(items) > w.Config.ItemsPerIteration {
				items = items[:w.Config.ItemsPerIteration]
			}
		}

		// Detect partially completed items for resume context.
		var partialItems []ChecklistItem
		if w.State.RunStartCommit != "" {
			workdir := w.Config.Workdir
			if workdir == "" {
				workdir = "."
			}
			partialItems, _ = DetectPartiallyCompleted(w.Config.Checklist, w.Pattern, w.State.RunStartCommit, workdir)
		}

		// Run the child batch with retry logic for failed iterations.
		var batch acceptedBatch
		var output string
		var exitCode int
		var runErr error
		var completed int
		var elapsed time.Duration

		for attempt := 0; attempt <= w.Config.MaxRetries; attempt++ {
			if attempt > 0 {
				w.logf("[W%d %s] Iteration #%d: retrying (%d/%d) after %s\n",
					w.Num, w.Name, i, attempt, w.Config.MaxRetries, w.Config.RetryDelay)
				w.setStatus(fmt.Sprintf("retry %d/%d iter #%d", attempt, w.Config.MaxRetries, i))
				select {
				case <-time.After(w.Config.RetryDelay):
				case <-ctx.Done():
					w.setStatus("shutdown")
					return ctx.Err()
				}
			}

			w.setStatus(fmt.Sprintf("running iter #%d", i))
			w.logf("[W%d %s] Starting iteration #%d (%d items, %d remaining)\n",
				w.Num, w.Name, i, len(items), len(pending))

			startTime := time.Now()
			batch, runErr = w.collectChildBatch(ctx, i, items, partialItems)
			completed = len(batch.completed)
			output = batch.output
			exitCode = batch.exitCode
			elapsed = time.Since(startTime)

			if runErr != nil {
				w.logf("[W%d %s] Iteration #%d: backend error: %v\n", w.Num, w.Name, i, runErr)
			}

			// Check for rate limiting — auto-rotate token if possible.
			if w.TokenManager != nil && w.TokenManager.RotateIfRateLimited(output) {
				w.logf("[W%d %s] Rate limit detected — rotated to next token\n", w.Num, w.Name)
			}

			// Check for permission blocks — ask user to approve external dirs.
			if w.PermHandler != nil && w.PermHandler(output) {
				w.logf("[W%d %s] Permission issue resolved — will retry on next iteration\n", w.Num, w.Name)
			}

			if shouldAbortIteration(exitCode, runErr, completed) {
				w.setStatus("error")
				return fmt.Errorf("[W%d %s] iteration #%d had mixed success and failure; workspace left uncommitted for inspection", w.Num, w.Name, i)
			}

			// Determine if iteration failed cleanly enough to retry.
			iterFailed := shouldRetryIteration(exitCode, runErr, completed)
			if !iterFailed || attempt >= w.Config.MaxRetries {
				if iterFailed {
					w.logf("[W%d %s] Iteration #%d: failed after %d retries\n",
						w.Num, w.Name, i, w.Config.MaxRetries)
				}
				break
			}

			w.logf("[W%d %s] Iteration #%d: failed (exit %d, %d completed) — will retry\n",
				w.Num, w.Name, i, exitCode, completed)
		}

		// Parse and track token usage.
		if w.Usage != nil {
			input, outp := ParseTokenUsage(output)
			if input > 0 || outp > 0 {
				w.Usage.Add(input, outp)
				if w.GlobalUsage != nil {
					w.GlobalUsage.Add(input, outp)
				}
				_, _, _, cost := w.Usage.Snapshot()
				w.logf("[W%d %s] Tokens: +%d in, +%d out (worker total: $%.2f)\n",
					w.Num, w.Name, input, outp, cost)
			}
		}

		// Check budget limit.
		if w.GlobalUsage != nil && w.Config.BudgetLimit > 0 {
			_, _, _, totalCost := w.GlobalUsage.Snapshot()
			if totalCost >= w.Config.BudgetLimit {
				w.logf("[W%d %s] Budget limit reached ($%.2f >= $%.2f) — stopping\n",
					w.Num, w.Name, totalCost, w.Config.BudgetLimit)
				w.Shutdown.Store(true)
			}
		}

		if shouldCommitIteration(exitCode, runErr) {
			acceptedRuns := acceptBatchIntoBuffer(buffer, batch, currentGeneration)
			generationRuns += acceptedRuns
			if shouldResetGeneration(generationRuns, w.Config.ParentResetAfterRuns) {
				currentGeneration++
				generationRuns = 0
				ws.ResetCount++
				ws.LastResetReason = "generation threshold"
				w.logf("[W%d %s] Parent generation rolled to %d\n", w.Num, w.Name, currentGeneration)
			}
		}

		flushCandidates := buffer.FlushCandidates(w.Config.CommitBatchSize)

		// Commit any changes.
		if shouldCommitIteration(exitCode, runErr) && len(flushCandidates) > 0 {
			w.setStatus("committing")
			paths := make([]string, 0, len(flushCandidates))
			for _, result := range flushCandidates {
				paths = append(paths, result.Path)
			}
			if markErr := MarkCompletedPaths(w.Config.Checklist, paths); markErr != nil {
				return fmt.Errorf("[W%d %s] updating checklist for flush: %w", w.Num, w.Name, markErr)
			}
			commitMsg := fmt.Sprintf("ralph: W%d %s iter #%d — flushed %d buffered items", w.Num, w.Name, i, len(flushCandidates))
			if commitErr := w.gitCommit(commitMsg); commitErr != nil {
				w.logf("[W%d %s] Iteration #%d: git commit warning: %v\n", w.Num, w.Name, i, commitErr)
				// Non-fatal — agent may not have produced changes.
			} else {
				buffer.FlushCompleted(w.Config.CommitBatchSize)
			}
		} else if !shouldCommitIteration(exitCode, runErr) {
			w.logf("[W%d %s] Iteration #%d: skipping git commit because batch did not finish cleanly\n", w.Num, w.Name, i)
		} else {
			w.logf("[W%d %s] Iteration #%d: buffered %d completed results waiting for flush threshold %d\n",
				w.Num, w.Name, i, buffer.CompletedCount(), w.Config.CommitBatchSize)
		}

		// Update state under lock.
		w.StateMutex.Lock()
		ws = w.State.Workers[w.Name]
		if ws == nil {
			ws = &state.WorkerState{}
			w.State.Workers[w.Name] = ws
		}
		ws.ParentGeneration = currentGeneration
		ws.GenerationRuns = generationRuns
		ws.PendingCommitCount = buffer.CompletedCount()
		buffer.WriteToWorkerState(ws)
		w.setBufferState(currentGeneration, buffer.CompletedCount(), buffer.PartialCount(), ws.ResetCount)
		w.State.RecordIteration(w.Name, i, completed, elapsed, exitCode)
		if saveErr := w.State.Save(w.Config.StateFile); saveErr != nil {
			w.StateMutex.Unlock()
			return fmt.Errorf("[W%d %s] saving state: %w", w.Num, w.Name, saveErr)
		}
		w.StateMutex.Unlock()

		// Log result.
		mins := int(elapsed.Minutes())
		secs := int(elapsed.Seconds()) % 60
		w.logf("[W%d %s] Iteration #%d: %d items | %dm%ds | exit %d\n",
			w.Num, w.Name, i, completed, mins, secs, exitCode)

		// Stale detection
		if completed == 0 {
			consecutiveStale++
			if w.Config.MaxStaleIterations > 0 && consecutiveStale >= w.Config.MaxStaleIterations {
				w.logf("[W%d %s] Stalled: 0 items completed for %d consecutive iterations — stopping\n",
					w.Num, w.Name, consecutiveStale)
				w.setStatus("stalled")
				return nil
			}
		} else {
			consecutiveStale = 0
		}

		// Cooldown between iterations.
		if w.Config.Cooldown > 0 && i < w.Config.MaxIterations {
			w.setStatus("cooling down")
			select {
			case <-time.After(w.Config.Cooldown):
			case <-ctx.Done():
				w.setStatus("shutdown")
				return ctx.Err()
			}
		}

		w.setStatus("idle")
	}

	w.logf("[W%d %s] Reached max iterations (%d)\n", w.Num, w.Name, w.Config.MaxIterations)
	w.setStatus("done")
	return nil
}

// gitCommit stages all changes and creates a commit while holding the git
// mutex to prevent concurrent git operations across workers.
func (w *Worker) gitCommit(msg string) error {
	w.GitMutex.Lock()
	defer w.GitMutex.Unlock()

	// git add -A
	addCmd := exec.Command("git", "add", "-A")
	addCmd.Dir = w.Config.Workdir
	if out, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %w\n%s", err, out)
	}

	// Track modified files for deduplication detection.
	diffCmd := exec.Command("git", "diff", "--cached", "--name-only")
	diffCmd.Dir = w.Config.Workdir
	if diffOut, err := diffCmd.Output(); err == nil {
		if w.ModifiedFiles == nil {
			w.ModifiedFiles = make(map[string]bool)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(diffOut)), "\n") {
			if line != "" {
				w.ModifiedFiles[line] = true
			}
		}
	}

	// git commit -m "msg"
	commitCmd := exec.Command("git", "commit", "-m", msg)
	commitCmd.Dir = w.Config.Workdir
	if out, err := commitCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %w\n%s", err, out)
	}

	return nil
}
