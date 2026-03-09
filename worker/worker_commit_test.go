package worker

import (
	"errors"
	"testing"
)

func TestShouldCommitIteration(t *testing.T) {
	if !shouldCommitIteration(0, nil) {
		t.Fatal("successful batch should commit")
	}

	if shouldCommitIteration(1, nil) {
		t.Fatal("non-zero exit batch should not commit")
	}

	if shouldCommitIteration(0, errors.New("backend error")) {
		t.Fatal("errored batch should not commit")
	}
}

func TestBatchFailureHandling(t *testing.T) {
	if !shouldRetryIteration(1, errors.New("backend error"), 0) {
		t.Fatal("empty failed batch should be retryable")
	}

	if shouldRetryIteration(1, errors.New("backend error"), 1) {
		t.Fatal("mixed-success batch should not be retryable")
	}

	if !shouldAbortIteration(1, errors.New("backend error"), 1) {
		t.Fatal("mixed-success batch should abort worker")
	}

	if shouldAbortIteration(1, errors.New("backend error"), 0) {
		t.Fatal("empty failed batch should not abort worker")
	}
}
