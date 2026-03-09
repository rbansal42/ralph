package state

import (
	"path/filepath"
	"testing"
)

func TestStateRoundTripParentFields(t *testing.T) {
	s := &State{
		StartedAt:   "2026-03-09T12:00:00Z",
		LastUpdated: "2026-03-09T12:00:00Z",
		Workers: map[string]*WorkerState{
			"tasks": {
				ParentGeneration: 2,
				ClaimedFiles: map[string]string{
					"app/Tasks/Foo.php": "child-1",
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
	if got.ParentGeneration != 2 {
		t.Fatalf("ParentGeneration = %d, want 2", got.ParentGeneration)
	}

	if got.ClaimedFiles["app/Tasks/Foo.php"] != "child-1" {
		t.Fatalf("ClaimedFiles[app/Tasks/Foo.php] = %q, want %q", got.ClaimedFiles["app/Tasks/Foo.php"], "child-1")
	}
}
