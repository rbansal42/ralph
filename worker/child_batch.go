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
		tasks = append(tasks, Task{
			ID:    item.Path,
			Item:  item,
			Files: []string{item.Path},
		})
	}

	parallelTasks, serialTasks := PartitionDispatchable(tasks)
	ordered := append(parallelTasks, serialTasks...)

	results := make(chan childTaskResult, len(ordered))
	var wg sync.WaitGroup
	sem := make(chan struct{}, parallelism)

	for idx, task := range ordered {
		wg.Add(1)
		go func(idx int, task Task) {
			defer wg.Done()

			sem <- struct{}{}
			w.changeActiveChildren(1)
			defer func() {
				w.changeActiveChildren(-1)
				<-sem
			}()

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
				results <- childTaskResult{err: promptErr, exitCode: -1}
				return
			}

			childOutput, childExit, runErr := w.Backend.RunPrompt(ctx, promptFile, w.Config.Workdir, w.Config.Model)
			result := childTaskResult{
				output:   childOutput,
				exitCode: childExit,
				err:      runErr,
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

	go func() {
		wg.Wait()
		close(results)
	}()

	var batch acceptedBatch
	var outputs strings.Builder
	exitCode := 0
	var err error

	for result := range results {
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
