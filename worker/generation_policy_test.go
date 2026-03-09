package worker

import "testing"

func TestShouldResetGenerationAfterConfiguredRuns(t *testing.T) {
	if !shouldResetGeneration(3, 3) {
		t.Fatal("expected rollover at threshold")
	}
}

func TestShouldNotResetGenerationBelowThreshold(t *testing.T) {
	if shouldResetGeneration(2, 3) {
		t.Fatal("unexpected rollover below threshold")
	}
}
