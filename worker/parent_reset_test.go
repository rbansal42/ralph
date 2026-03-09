package worker

import "testing"

func TestParentResetPreservesQueueAndGeneration(t *testing.T) {
	parent := newTestParentWorker(2)
	parent.Generation = 3
	parent.Completed = map[string]bool{"done": true}
	parent.Enqueue(Task{ID: "1", Files: []string{"a.go"}})

	snapshot := parent.Snapshot()
	reloaded := ParentWorkerFromSnapshot(snapshot, 2)

	if reloaded.Generation != 3 {
		t.Fatalf("Generation = %d, want 3", reloaded.Generation)
	}

	if len(reloaded.Queue()) != 1 {
		t.Fatalf("queue length = %d, want 1", len(reloaded.Queue()))
	}

	if !reloaded.Completed["done"] {
		t.Fatal("completed state should survive snapshot")
	}
}

func TestParentResetWaitsForInflightChildren(t *testing.T) {
	parent := newTestParentWorker(1)
	parent.MarkInFlight(Task{ID: "1", Files: []string{"a.go"}})

	if parent.CanResetNow() {
		t.Fatal("expected reset to be blocked while child is in flight")
	}
}
