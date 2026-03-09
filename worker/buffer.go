package worker

import "github.com/rahulvramesh/ralph/state"

// ResultBuffer tracks accepted completed and partial results separately.
type ResultBuffer struct {
	completed []state.BufferedResult
	partial   []state.BufferedResult
}

// NewResultBuffer creates an empty result buffer.
func NewResultBuffer() *ResultBuffer {
	return &ResultBuffer{}
}

// Accept records a parent-accepted buffered result.
func (b *ResultBuffer) Accept(result state.BufferedResult) {
	if result.Complete {
		b.completed = append(b.completed, result)
		return
	}
	b.partial = append(b.partial, result)
}

// CompletedCount returns the number of completed buffered results.
func (b *ResultBuffer) CompletedCount() int {
	return len(b.completed)
}

// PartialCount returns the number of partial buffered results.
func (b *ResultBuffer) PartialCount() int {
	return len(b.partial)
}

// FlushCandidates returns completed buffered results only when the threshold is met.
func (b *ResultBuffer) FlushCandidates(threshold int) []state.BufferedResult {
	if threshold <= 0 || len(b.completed) < threshold {
		return nil
	}
	results := make([]state.BufferedResult, len(b.completed))
	copy(results, b.completed)
	return results
}
