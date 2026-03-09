package worker

import "github.com/rahulvramesh/ralph/state"

// ResultBuffer tracks accepted completed and partial results separately.
type ResultBuffer struct {
	completed []state.BufferedResult
	partial   []state.BufferedResult
}

// ResultBufferFromWorkerState rebuilds a runtime buffer from persisted worker state.
func ResultBufferFromWorkerState(ws *state.WorkerState) *ResultBuffer {
	buf := NewResultBuffer()
	buf.completed = append(buf.completed, ws.BufferedCompleted...)
	buf.partial = append(buf.partial, ws.BufferedPartial...)
	return buf
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

// FlushCompleted removes and returns completed buffered results once the threshold is met.
func (b *ResultBuffer) FlushCompleted(threshold int) []state.BufferedResult {
	results := b.FlushCandidates(threshold)
	if len(results) == 0 {
		return nil
	}
	b.completed = nil
	return results
}

// WriteToWorkerState persists the current buffer slices into worker state.
func (b *ResultBuffer) WriteToWorkerState(ws *state.WorkerState) {
	ws.BufferedCompleted = append(ws.BufferedCompleted[:0], b.completed...)
	ws.BufferedPartial = append(ws.BufferedPartial[:0], b.partial...)
}

func acceptBatchIntoBuffer(buf *ResultBuffer, batch acceptedBatch, generation int) int {
	added := 0
	for _, result := range batch.completed {
		result.Generation = generation
		buf.Accept(result)
		added++
	}
	for _, result := range batch.partial {
		result.Generation = generation
		buf.Accept(result)
		added++
	}
	return added
}
