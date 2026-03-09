package worker

import (
	"testing"

	"github.com/rahulvramesh/ralph/state"
)

func TestResultBufferRoundTripsThroughWorkerState(t *testing.T) {
	ws := &state.WorkerState{
		BufferedCompleted: []state.BufferedResult{
			{Path: "a", Complete: true, Generation: 1},
		},
		BufferedPartial: []state.BufferedResult{
			{Path: "b", Complete: false, Generation: 1},
		},
	}

	buf := ResultBufferFromWorkerState(ws)
	copyState := &state.WorkerState{}
	buf.WriteToWorkerState(copyState)

	if len(copyState.BufferedCompleted) != 1 || copyState.BufferedCompleted[0].Path != "a" {
		t.Fatalf("BufferedCompleted = %#v, want one completed item", copyState.BufferedCompleted)
	}
	if len(copyState.BufferedPartial) != 1 || copyState.BufferedPartial[0].Path != "b" {
		t.Fatalf("BufferedPartial = %#v, want one partial item", copyState.BufferedPartial)
	}
}

func TestResultBufferFlushCompletedRemovesOnlyCompletedRecords(t *testing.T) {
	buf := NewResultBuffer()
	buf.Accept(state.BufferedResult{Path: "a", Complete: true})
	buf.Accept(state.BufferedResult{Path: "b", Complete: true})
	buf.Accept(state.BufferedResult{Path: "c", Complete: false})

	flushed := buf.FlushCompleted(2)

	if len(flushed) != 2 {
		t.Fatalf("len(flushed) = %d, want 2", len(flushed))
	}
	if buf.CompletedCount() != 0 {
		t.Fatalf("CompletedCount() = %d, want 0", buf.CompletedCount())
	}
	if buf.PartialCount() != 1 {
		t.Fatalf("PartialCount() = %d, want 1", buf.PartialCount())
	}
}
