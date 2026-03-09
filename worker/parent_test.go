package worker

import "testing"

func newTestParentWorker(parallelism int) *ParentWorker {
	return &ParentWorker{
		parallelism: parallelism,
		queue:       make([]Task, 0),
		inFlight:    make(map[string]Task),
		claims:      NewClaimTable(),
	}
}

func TestParentDispatchesUpToParallelism(t *testing.T) {
	parent := newTestParentWorker(2)
	parent.Enqueue(
		Task{ID: "1", Files: []string{"a.go"}},
		Task{ID: "2", Files: []string{"b.go"}},
		Task{ID: "3", Files: []string{"c.go"}},
	)

	parent.FillChildSlots()

	if got := parent.InFlightCount(); got != 2 {
		t.Fatalf("InFlightCount = %d, want 2", got)
	}
}

func TestParentSkipsBlockedTaskAndDispatchesCompatibleTask(t *testing.T) {
	parent := newTestParentWorker(2)
	if !parent.claims.TryClaim("existing", []string{"a.go"}) {
		t.Fatal("initial claim should succeed")
	}

	parent.Enqueue(Task{ID: "blocked", Files: []string{"a.go"}})
	parent.Enqueue(Task{ID: "free", Files: []string{"b.go"}})

	parent.FillChildSlots()

	if !parent.IsInFlight("free") {
		t.Fatal("expected compatible task to be dispatched")
	}

	if parent.IsInFlight("blocked") {
		t.Fatal("blocked task should remain queued")
	}
}
