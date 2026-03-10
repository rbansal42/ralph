package ui

import "testing"

func TestWorkerSafetyLabel(t *testing.T) {
	info := WorkerInfo{
		SerialFallbacks: 3,
		ClaimConflicts:  2,
	}

	if got := workerSafetyLabel(info); got != "serial 3 | conflicts 2" {
		t.Fatalf("workerSafetyLabel() = %q, want %q", got, "serial 3 | conflicts 2")
	}
}
