package worker

import (
	"context"
	"fmt"
	"os/exec"
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
	Num           int
	Name          string
	Pattern       string
	TotalWorkers  int
	Backend       backend.Backend
	Config        *config.Config
	State         *state.State
	GitMutex      *sync.Mutex
	StateMutex    *sync.Mutex
	Shutdown      *atomic.Bool
	CurrentStatus string
	StatusMutex   sync.RWMutex
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

// Run starts the worker loop. It blocks until all items are done, max
// iterations reached, or shutdown is signaled via the Shutdown flag or
// context cancellation.
func (w *Worker) Run(ctx context.Context) error {
	w.setStatus("idle")

	// Determine starting iteration from persisted state.
	w.StateMutex.Lock()
	startIter := w.State.GetWorkerIteration(w.Name)
	w.StateMutex.Unlock()

	logDir := "logs"

	for i := startIter + 1; i <= w.Config.MaxIterations; i++ {
		// Check for shutdown signal.
		if w.Shutdown.Load() {
			fmt.Printf("[W%d %s] Shutdown signaled, stopping\n", w.Num, w.Name)
			w.setStatus("shutdown")
			return nil
		}

		// Check context cancellation.
		select {
		case <-ctx.Done():
			fmt.Printf("[W%d %s] Context cancelled, stopping\n", w.Num, w.Name)
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
			fmt.Printf("[W%d %s] No pending items remaining, done\n", w.Num, w.Name)
			w.setStatus("done")
			return nil
		}

		// Take first N items for this iteration.
		items := pending
		if len(items) > w.Config.ItemsPerIteration {
			items = items[:w.Config.ItemsPerIteration]
		}

		// Build the prompt file.
		promptFile, err := BuildPrompt(
			w.Config.Prompt,
			w.Name,
			w.Num,
			w.TotalWorkers,
			w.Pattern,
			i,
			items,
			len(pending),
			logDir,
		)
		if err != nil {
			return fmt.Errorf("[W%d %s] building prompt: %w", w.Num, w.Name, err)
		}

		// Run the AI agent.
		w.setStatus(fmt.Sprintf("running iter #%d", i))
		fmt.Printf("[W%d %s] Starting iteration #%d (%d items, %d remaining)\n",
			w.Num, w.Name, i, len(items), len(pending))

		startTime := time.Now()
		_, exitCode, runErr := w.Backend.RunPrompt(ctx, promptFile, w.Config.Workdir, w.Config.Model)
		elapsed := time.Since(startTime)

		if runErr != nil {
			fmt.Printf("[W%d %s] Iteration #%d: backend error: %v\n", w.Num, w.Name, i, runErr)
			// Continue to commit and record state even on error — the agent
			// may have made partial progress.
		}

		// Count remaining items after the agent ran.
		newRemaining, err := CountPending(w.Config.Checklist, w.Pattern)
		if err != nil {
			return fmt.Errorf("[W%d %s] counting remaining: %w", w.Num, w.Name, err)
		}
		completed := len(pending) - newRemaining

		// Commit any changes.
		w.setStatus("committing")
		commitMsg := fmt.Sprintf("ralph: W%d %s iter #%d — %d items completed", w.Num, w.Name, i, completed)
		if commitErr := w.gitCommit(commitMsg); commitErr != nil {
			fmt.Printf("[W%d %s] Iteration #%d: git commit warning: %v\n", w.Num, w.Name, i, commitErr)
			// Non-fatal — agent may not have produced changes.
		}

		// Update state under lock.
		w.StateMutex.Lock()
		w.State.RecordIteration(w.Name, i, completed, elapsed, exitCode)
		if saveErr := w.State.Save(w.Config.StateFile); saveErr != nil {
			w.StateMutex.Unlock()
			return fmt.Errorf("[W%d %s] saving state: %w", w.Num, w.Name, saveErr)
		}
		w.StateMutex.Unlock()

		// Log result.
		mins := int(elapsed.Minutes())
		secs := int(elapsed.Seconds()) % 60
		fmt.Printf("[W%d %s] Iteration #%d: %d items | %dm%ds | exit %d\n",
			w.Num, w.Name, i, completed, mins, secs, exitCode)

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

	fmt.Printf("[W%d %s] Reached max iterations (%d)\n", w.Num, w.Name, w.Config.MaxIterations)
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

	// git commit -m "msg"
	commitCmd := exec.Command("git", "commit", "-m", msg)
	commitCmd.Dir = w.Config.Workdir
	if out, err := commitCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %w\n%s", err, out)
	}

	return nil
}
