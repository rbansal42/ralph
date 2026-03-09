package worker

import "testing"

func successfulResult(id string) ChildResult {
	return ChildResult{
		CompletedItems: []string{id},
		FilesChanged:   []string{id + ".go"},
	}
}

func failedResult(id string) ChildResult {
	return ChildResult{
		AttemptedItems: []string{id},
		FailureReason:  "backend error",
	}
}

func TestCommitBatchFlushesAfterThreshold(t *testing.T) {
	batch := NewCommitBatcher(2)

	if batch.Add(successfulResult("1")) {
		t.Fatal("first result should not flush")
	}

	if !batch.Add(successfulResult("2")) {
		t.Fatal("second result should trigger flush")
	}
}

func TestCommitBatchRejectsFailedResult(t *testing.T) {
	batch := NewCommitBatcher(2)

	if batch.Add(failedResult("1")) {
		t.Fatal("failed result must not trigger flush")
	}

	if got := batch.PendingCount(); got != 0 {
		t.Fatalf("PendingCount = %d, want 0", got)
	}
}
