package worker

import (
	"testing"

	"github.com/rahulvramesh/ralph/state"
)

func TestBufferAcceptsCompletedAndPartialResultsSeparately(t *testing.T) {
	buf := NewResultBuffer()
	buf.Accept(state.BufferedResult{Path: "a", Complete: true})
	buf.Accept(state.BufferedResult{Path: "b", Complete: false})

	if got := buf.CompletedCount(); got != 1 {
		t.Fatalf("CompletedCount() = %d, want 1", got)
	}
	if got := buf.PartialCount(); got != 1 {
		t.Fatalf("PartialCount() = %d, want 1", got)
	}
}

func TestBufferFlushCandidatesRequireThreshold(t *testing.T) {
	buf := NewResultBuffer()
	buf.Accept(state.BufferedResult{Path: "a", Complete: true})

	if got := len(buf.FlushCandidates(2)); got != 0 {
		t.Fatalf("len(FlushCandidates()) = %d, want 0", got)
	}
}
