package worker

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rahulvramesh/ralph/state"
)

type childTaskResult struct {
	taskID    string
	files     []string
	serialOnly bool
	output    string
	exitCode  int
	err       error
	completed []string
	partial   []string
}

type acceptedBatch struct {
	completed []state.BufferedResult
	partial   []state.BufferedResult
	output    string
	exitCode  int
}

// collectChildBatch launches one child agent per item and returns the accepted
// completed/partial result buffers without mutating the checklist.
func (w *Worker) collectChildBatch(
	ctx context.Context,
	iteration int,
	items []ChecklistItem,
	partialItems []ChecklistItem,
) (acceptedBatch, error) {
	if len(items) == 0 {
		return acceptedBatch{}, nil
	}

	parallelism := w.Config.WorkerParallelism
	if parallelism <= 0 {
		parallelism = 1
	}

	tasks := make([]Task, 0, len(items))
	for _, item := range items {
		task := ShapeChecklistItem(item)
		task.Item = item
		tasks = append(tasks, task)
	}

	parallelTasks, serialTasks := PartitionDispatchable(tasks)
	ordered := append(parallelTasks, serialTasks...)

	results := make(chan childTaskResult, len(ordered))
	var wg sync.WaitGroup
	claims := NewClaimTable()
	pendingParallel := append([]Task(nil), parallelTasks...)
	pendingSerial := append([]Task(nil), serialTasks...)
	activeTasks := 0
	taskIndex := 0
	var batch acceptedBatch
	var outputs strings.Builder
	exitCode := 0
	var err error

	launchTask := func(idx int, task Task) {
		activeTasks++
		wg.Add(1)
		go func(idx int, task Task) {
			defer wg.Done()
			w.changeActiveChildren(1)
			defer w.changeActiveChildren(-1)

			logDir := filepath.Join("logs", fmt.Sprintf("w%d_iter_%04d_task_%02d", w.Num, iteration, idx+1))
			promptFile, promptErr := BuildPrompt(
				w.Config.Prompt,
				w.Name,
				w.Num,
				w.TotalWorkers,
				w.Pattern,
				iteration,
				[]ChecklistItem{task.Item},
				len(items),
				logDir,
				partialItems,
				false,
			)
			if promptErr != nil {
				results <- childTaskResult{taskID: task.ID, files: task.Files, serialOnly: task.SerialOnly, err: promptErr, exitCode: -1}
				return
			}

			childOutput, childExit, runErr := w.Backend.RunPrompt(ctx, promptFile, w.Config.Workdir, w.Config.Model)
			result := childTaskResult{
				taskID:     task.ID,
				files:      task.Files,
				serialOnly: task.SerialOnly,
				output:     childOutput,
				exitCode:   childExit,
				err:        runErr,
			}

			if parsed, parseErr := ParseChildResult(childOutput); parseErr == nil {
				result.completed = parsed.CompletedItems
				result.partial = parsed.PartialItems
			} else if runErr == nil && childExit == 0 {
				result.err = parseErr
				result.exitCode = -1
			}

			results <- result
		}(idx, task)
	}

	tryLaunchParallel := func() bool {
		if activeTasks >= parallelism {
			return false
		}
		for idx, task := range pendingParallel {
			if claims.TryClaim(task.ID, task.Files) {
				pendingParallel = append(pendingParallel[:idx], pendingParallel[idx+1:]...)
				launchTask(taskIndex, task)
				taskIndex++
				return true
			}
		}
		return false
	}

	for len(pendingParallel) > 0 || len(pendingSerial) > 0 || activeTasks > 0 {
		progress := false
		for activeTasks < parallelism && tryLaunchParallel() {
			progress = true
		}
		if activeTasks == 0 && len(pendingSerial) > 0 {
			task := pendingSerial[0]
			pendingSerial = pendingSerial[1:]
			launchTask(taskIndex, task)
			taskIndex++
			progress = true
		}
		if progress {
			continue
		}

		result := <-results
		activeTasks--
		if !result.serialOnly {
			claims.Release(result.taskID)
		}

		if outputs.Len() > 0 {
			outputs.WriteString("\n")
		}
		outputs.WriteString(result.output)

		if len(result.completed) > 0 {
			for _, path := range result.completed {
				batch.completed = append(batch.completed, state.BufferedResult{
					Path:       path,
					Complete:   true,
					Generation: 0,
				})
			}
		}

		if len(result.partial) > 0 {
			for _, path := range result.partial {
				batch.partial = append(batch.partial, state.BufferedResult{
					Path:       path,
					Complete:   false,
					Generation: 0,
				})
			}
		}

		if result.err != nil && err == nil {
			err = result.err
		}
		if result.exitCode != 0 && exitCode == 0 {
			exitCode = result.exitCode
		}
	}

	wg.Wait()

	w.setActiveChildren(0)

	batch.output = outputs.String()
	batch.exitCode = exitCode
	return batch, err
}

// runChildBatch launches one child agent per item, up to the configured worker
// parallelism, and lets the parent reconcile checklist changes.
func (w *Worker) runChildBatch(
	ctx context.Context,
	iteration int,
	items []ChecklistItem,
	partialItems []ChecklistItem,
) (completed int, output string, exitCode int, err error) {
	batch, err := w.collectChildBatch(ctx, iteration, items, partialItems)
	completedPaths := make([]string, 0, len(batch.completed))
	for _, result := range batch.completed {
		completed++
		completedPaths = append(completedPaths, result.Path)
	}

	if len(completedPaths) > 0 && err == nil && batch.exitCode == 0 {
		if markErr := MarkCompletedPaths(w.Config.Checklist, completedPaths); markErr != nil && err == nil {
			err = markErr
		}
	}

	return completed, batch.output, batch.exitCode, err
}
