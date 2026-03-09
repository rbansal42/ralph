package ui

import "testing"

func TestWorkerGenerationLabel(t *testing.T) {
	info := WorkerInfo{
		Generation:        3,
		BufferedCompleted: 2,
		BufferedPartial:   1,
		ResetCount:        4,
	}

	if got := workerGenerationLabel(info); got != "gen 3 | buf 2c/1p | resets 4" {
		t.Fatalf("workerGenerationLabel() = %q, want %q", got, "gen 3 | buf 2c/1p | resets 4")
	}
}
