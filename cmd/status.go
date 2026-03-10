package cmd

import (
	"fmt"
	"time"

	"github.com/rahulvramesh/ralph/config"
	"github.com/rahulvramesh/ralph/state"
	"github.com/rahulvramesh/ralph/ui"
	"github.com/rahulvramesh/ralph/worker"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current progress without running workers",
	RunE:  runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	st, err := state.Load(cfg.StateFile)
	if err != nil {
		return fmt.Errorf("loading state: %w", err)
	}

	// Build WorkerInfo slice for the status box.
	var workerInfos []ui.WorkerInfo
	for i, wc := range cfg.Workers {
		remaining, err := worker.CountPending(cfg.Checklist, wc.Pattern)
		if err != nil {
			return fmt.Errorf("counting pending for worker %s: %w", wc.Name, err)
		}

		ws := st.Workers[wc.Name]
		var lastElapsed string
		var generation int
		var bufferedCompleted int
		var bufferedPartial int
		var resetCount int
		var serialFallbacks int
		var claimConflicts int
		if ws != nil && len(ws.History) > 0 {
			last := ws.History[len(ws.History)-1]
			lastElapsed = formatDuration(time.Duration(last.ElapsedS * float64(time.Second)))
		}
		if ws != nil {
			generation = ws.ParentGeneration
			bufferedCompleted = len(ws.BufferedCompleted)
			bufferedPartial = len(ws.BufferedPartial)
			resetCount = ws.ResetCount
			serialFallbacks = ws.SerialFallbackCount
			claimConflicts = ws.ClaimConflictCount
		}

		workerInfos = append(workerInfos, ui.WorkerInfo{
			Num:            i + 1,
			Name:           wc.Name,
			Pattern:        wc.Pattern,
			Remaining:      remaining,
			Status:         "idle",
			LastElapsed:    lastElapsed,
			ChildCapacity:  cfg.WorkerParallelism,
			ActiveChildren: 0,
			Generation:        generation,
			BufferedCompleted: bufferedCompleted,
			BufferedPartial:   bufferedPartial,
			ResetCount:        resetCount,
			SerialFallbacks:   serialFallbacks,
			ClaimConflicts:    claimConflicts,
		})
	}

	ui.PrintBanner()
	ui.PrintStatus(cfg.Checklist, workerInfos)

	// Per-worker details.
	fmt.Println()
	fmt.Println("Worker Details")
	fmt.Println("──────────────")

	var totalCompleted int
	var totalIterations int
	var totalElapsedS float64

	for _, wc := range cfg.Workers {
		ws := st.Workers[wc.Name]
		if ws == nil {
			fmt.Printf("  %-12s  no iterations recorded\n", wc.Name)
			continue
		}

		var workerElapsedS float64
		for _, h := range ws.History {
			workerElapsedS += h.ElapsedS
		}

		iters := len(ws.History)
		var avgTime time.Duration
		if iters > 0 {
			avgTime = time.Duration(workerElapsedS / float64(iters) * float64(time.Second))
		}

		fmt.Printf("  %-12s  iterations: %-4d  completed: %-4d  avg/iter: %s\n",
			wc.Name, iters, ws.Completed, formatDuration(avgTime))

		totalCompleted += ws.Completed
		totalIterations += iters
		totalElapsedS += workerElapsedS
	}

	// Overall stats.
	counts, err := worker.CountByStatus(cfg.Checklist)
	if err != nil {
		return fmt.Errorf("counting checklist items: %w", err)
	}

	remaining := counts["~"]
	total := counts["x"] + counts["s"] + counts["~"]

	fmt.Println()
	fmt.Println("Overall")
	fmt.Println("───────")
	fmt.Printf("  Total items:      %d\n", total)
	fmt.Printf("  Completed:        %d\n", counts["x"])
	fmt.Printf("  Skipped:          %d\n", counts["s"])
	fmt.Printf("  Remaining:        %d\n", remaining)

	if totalIterations > 0 {
		avgIterTime := time.Duration(totalElapsedS / float64(totalIterations) * float64(time.Second))
		fmt.Printf("  Avg time/iter:    %s\n", formatDuration(avgIterTime))

		if totalCompleted > 0 {
			avgPerItem := time.Duration(totalElapsedS / float64(totalCompleted) * float64(time.Second))
			estRemaining := time.Duration(float64(remaining) * float64(avgPerItem))
			fmt.Printf("  Est. remaining:   %s\n", formatDuration(estRemaining))
		}
	}

	return nil
}

// formatDuration formats a duration as "XhYmZs", omitting zero leading components.
func formatDuration(d time.Duration) string {
	if d == 0 {
		return "0s"
	}

	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60

	if h > 0 {
		return fmt.Sprintf("%dh%dm%ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
