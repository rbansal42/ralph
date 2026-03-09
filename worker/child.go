package worker

import (
	"encoding/json"
	"errors"
	"strings"
)

const childResultMarker = "RALPH_CHILD_RESULT "

var ErrMissingChildResult = errors.New("missing child result marker")

// ChildResult is the machine-readable summary returned by a child agent.
type ChildResult struct {
	AttemptedItems []string `json:"attempted,omitempty"`
	CompletedItems []string `json:"completed,omitempty"`
	FilesChanged   []string `json:"files_changed,omitempty"`
	ChecklistLines []int    `json:"checklist_lines,omitempty"`
	FailureReason  string   `json:"failure_reason,omitempty"`
}

// ParseChildResult extracts the final child result payload from backend output.
func ParseChildResult(output string) (ChildResult, error) {
	idx := strings.LastIndex(output, childResultMarker)
	if idx == -1 {
		return ChildResult{}, ErrMissingChildResult
	}

	raw := strings.TrimSpace(output[idx+len(childResultMarker):])

	var result ChildResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return ChildResult{}, err
	}

	return result, nil
}
