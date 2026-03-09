package worker

import "testing"

func TestClaimTableRejectsOverlappingFiles(t *testing.T) {
	table := NewClaimTable()

	if !table.TryClaim("child-1", []string{"app/Tasks/A.php"}) {
		t.Fatal("first claim should succeed")
	}

	if table.TryClaim("child-2", []string{"app/Tasks/A.php"}) {
		t.Fatal("overlapping claim should fail")
	}
}

func TestSchedulerRoutesUnknownFootprintToSerialLane(t *testing.T) {
	task := Task{ID: "1"}

	parallel, serial := PartitionDispatchable([]Task{task})

	if len(parallel) != 0 || len(serial) != 1 {
		t.Fatalf("parallel=%d serial=%d, want 0 and 1", len(parallel), len(serial))
	}
}
