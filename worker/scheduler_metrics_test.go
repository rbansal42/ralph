package worker

import (
	"testing"

	"github.com/rahulvramesh/ralph/state"
)

func TestWorkerStateTracksSchedulerSafetyCounters(t *testing.T) {
	ws := &state.WorkerState{
		SerialFallbackCount: 3,
		ClaimConflictCount:  2,
	}

	if ws.SerialFallbackCount != 3 {
		t.Fatalf("SerialFallbackCount = %d, want 3", ws.SerialFallbackCount)
	}
	if ws.ClaimConflictCount != 2 {
		t.Fatalf("ClaimConflictCount = %d, want 2", ws.ClaimConflictCount)
	}
}
