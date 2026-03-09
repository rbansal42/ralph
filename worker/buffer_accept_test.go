package worker

import (
	"testing"

	"github.com/rahulvramesh/ralph/state"
)

func TestAcceptBatchIntoBufferAssignsGeneration(t *testing.T) {
	buf := NewResultBuffer()
	batch := acceptedBatch{
		completed: []state.BufferedResult{{Path: "a", Complete: true}},
		partial:   []state.BufferedResult{{Path: "b", Complete: false}},
	}

	added := acceptBatchIntoBuffer(buf, batch, 3)

	if added != 2 {
		t.Fatalf("acceptBatchIntoBuffer() added = %d, want 2", added)
	}

	completed := buf.FlushCandidates(1)
	if len(completed) != 1 || completed[0].Generation != 3 {
		t.Fatalf("completed generation = %#v, want generation 3", completed)
	}
	if buf.partial[0].Generation != 3 {
		t.Fatalf("partial generation = %d, want 3", buf.partial[0].Generation)
	}
}
