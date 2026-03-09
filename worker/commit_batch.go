package worker

// CommitBatcher collects successful child results until the parent is ready to
// integrate them into a shared commit.
type CommitBatcher struct {
	threshold int
	results   []ChildResult
}

// NewCommitBatcher creates a batcher with a positive flush threshold.
func NewCommitBatcher(threshold int) *CommitBatcher {
	if threshold <= 0 {
		threshold = 1
	}

	return &CommitBatcher{threshold: threshold}
}

// Add appends a successful result to the batch. It returns true when the batch
// has reached its flush threshold.
func (b *CommitBatcher) Add(result ChildResult) bool {
	if result.FailureReason != "" {
		return false
	}

	b.results = append(b.results, result)
	return len(b.results) >= b.threshold
}

// PendingCount returns the number of successful results waiting to be flushed.
func (b *CommitBatcher) PendingCount() int {
	return len(b.results)
}

// Reset clears the current batch after integration.
func (b *CommitBatcher) Reset() {
	b.results = nil
}
