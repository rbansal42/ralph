package ui

import "testing"

func TestWorkerChildPoolLabel(t *testing.T) {
	info := WorkerInfo{
		ActiveChildren: 2,
		ChildCapacity:  4,
	}

	if got := workerChildPoolLabel(info); got != "children 2/4" {
		t.Fatalf("workerChildPoolLabel() = %q, want %q", got, "children 2/4")
	}
}

func TestWorkerChildPoolLabelOmitsEmptyCapacity(t *testing.T) {
	info := WorkerInfo{}

	if got := workerChildPoolLabel(info); got != "" {
		t.Fatalf("workerChildPoolLabel() = %q, want empty", got)
	}
}
