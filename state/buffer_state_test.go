package state

import (
	"path/filepath"
	"testing"
)

func TestStateRoundTripBufferedWorkerState(t *testing.T) {
	s := &State{
		StartedAt:   "2026-03-10T10:00:00Z",
		LastUpdated: "2026-03-10T10:00:00Z",
		Workers: map[string]*WorkerState{
			"tasks": {
				ParentGeneration:   2,
				GenerationRuns:     4,
				PendingCommitCount: 3,
				ResetCount:         1,
				LastResetReason:    "generation threshold",
				BufferedCompleted: []BufferedResult{
					{Path: "app/Tasks/A.php", Complete: true, Generation: 2},
				},
				BufferedPartial: []BufferedResult{
					{Path: "app/Tasks/B.php", Complete: false, Generation: 2},
				},
			},
		},
	}

	path := filepath.Join(t.TempDir(), "state.json")
	if err := s.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	got := loaded.Workers["tasks"]
	if got.GenerationRuns != 4 {
		t.Fatalf("GenerationRuns = %d, want 4", got.GenerationRuns)
	}
	if got.PendingCommitCount != 3 {
		t.Fatalf("PendingCommitCount = %d, want 3", got.PendingCommitCount)
	}
	if got.ResetCount != 1 {
		t.Fatalf("ResetCount = %d, want 1", got.ResetCount)
	}
	if got.LastResetReason != "generation threshold" {
		t.Fatalf("LastResetReason = %q, want %q", got.LastResetReason, "generation threshold")
	}
	if len(got.BufferedCompleted) != 1 || got.BufferedCompleted[0].Path != "app/Tasks/A.php" {
		t.Fatalf("BufferedCompleted = %#v, want one completed item", got.BufferedCompleted)
	}
	if len(got.BufferedPartial) != 1 || got.BufferedPartial[0].Path != "app/Tasks/B.php" {
		t.Fatalf("BufferedPartial = %#v, want one partial item", got.BufferedPartial)
	}
}
