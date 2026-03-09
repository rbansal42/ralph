package worker

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

type childTaskResult struct {
	output    string
	exitCode  int
	err       error
	completed []string
}

// runChildBatch launches one child agent per item, up to the configured worker
// parallelism, and lets the parent reconcile checklist changes.
func (w *Worker) runChildBatch(
	ctx context.Context,
	iteration int,
	items []ChecklistItem,
	partialItems []ChecklistItem,
) (completed int, output string, exitCode int, err error) {
	if len(items) == 0 {
		return 0, "", 0, nil
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

	var outputs strings.Builder
	exitCode = 0

	for result := range results {
		if outputs.Len() > 0 {
			outputs.WriteString("\n")
		}
		outputs.WriteString(result.output)

		if len(result.completed) > 0 {
			if markErr := MarkCompletedPaths(w.Config.Checklist, result.completed); markErr != nil && err == nil {
				err = markErr
			}
			completed += len(result.completed)
		}

		if result.err != nil && err == nil {
			err = result.err
		}
		if result.exitCode != 0 && exitCode == 0 {
			exitCode = result.exitCode
		}
	}

	w.setActiveChildren(0)

	return completed, outputs.String(), exitCode, err
}
