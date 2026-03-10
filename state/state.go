package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// IterationRecord captures the result of a single worker iteration.
type IterationRecord struct {
	Iteration int     `json:"iteration"`
	Completed int     `json:"completed"`
	ElapsedS  float64 `json:"elapsed_s"`
	ExitCode  int     `json:"exit_code"`
	Timestamp string  `json:"timestamp"`
}

// BufferedResult is a parent-accepted result record that may persist across
// iterations and generations before it becomes commit-eligible or flushed.
type BufferedResult struct {
	Path         string   `json:"path"`
	FilesChanged []string `json:"files_changed,omitempty"`
	Complete     bool     `json:"complete"`
	Generation   int      `json:"generation"`
}

// WorkerState tracks the cumulative progress for a named worker.
type WorkerState struct {
	Iteration        int               `json:"iteration"`
	Completed        int               `json:"completed"`
	Skipped          int               `json:"skipped"`
	History          []IterationRecord `json:"history"`
	ParentGeneration int               `json:"parent_generation,omitempty"`
	GenerationRuns   int               `json:"generation_runs,omitempty"`
	PendingCommitCount int             `json:"pending_commit_count,omitempty"`
	ResetCount       int               `json:"reset_count,omitempty"`
	LastResetReason  string            `json:"last_reset_reason,omitempty"`
	SerialFallbackCount int            `json:"serial_fallback_count,omitempty"`
	ClaimConflictCount  int            `json:"claim_conflict_count,omitempty"`
	BufferedCompleted []BufferedResult `json:"buffered_completed,omitempty"`
	BufferedPartial   []BufferedResult `json:"buffered_partial,omitempty"`
	ClaimedFiles     map[string]string `json:"claimed_files,omitempty"`
}

// State is the top-level container persisted to the JSON state file.
type State struct {
	StartedAt      string                  `json:"started_at"`
	LastUpdated    string                  `json:"last_updated"`
	RunStartCommit string                  `json:"run_start_commit,omitempty"`
	Workers        map[string]*WorkerState `json:"workers"`
}

// SetRunStartCommit records the git HEAD at run start for partial completion detection.
func (s *State) SetRunStartCommit(commit string) {
	if s.RunStartCommit == "" {
		s.RunStartCommit = commit
	}
}

// Load reads state from a JSON file at path. If the file does not exist, an
// empty State is returned with StartedAt set to the current time.
func Load(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &State{
				StartedAt:   time.Now().UTC().Format(time.RFC3339),
				LastUpdated: time.Now().UTC().Format(time.RFC3339),
				Workers:     make(map[string]*WorkerState),
			}, nil
		}
		return nil, fmt.Errorf("reading state file: %w", err)
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing state file: %w", err)
	}
	if s.Workers == nil {
		s.Workers = make(map[string]*WorkerState)
	}
	return &s, nil
}

// Save writes the state to path as pretty-printed JSON.
func (s *State) Save(path string) error {
	s.LastUpdated = time.Now().UTC().Format(time.RFC3339)

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling state: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing state file: %w", err)
	}
	return nil
}

// RecordIteration appends an iteration record for the named worker and updates
// its cumulative counters.
func (s *State) RecordIteration(workerName string, iteration int, completed int, elapsed time.Duration, exitCode int) {
	ws, ok := s.Workers[workerName]
	if !ok {
		ws = &WorkerState{}
		s.Workers[workerName] = ws
	}

	ws.Iteration = iteration
	ws.Completed += completed
	ws.History = append(ws.History, IterationRecord{
		Iteration: iteration,
		Completed: completed,
		ElapsedS:  elapsed.Seconds(),
		ExitCode:  exitCode,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// GetWorkerIteration returns the last iteration number for the named worker,
// or 0 if the worker has no recorded state.
func (s *State) GetWorkerIteration(workerName string) int {
	ws, ok := s.Workers[workerName]
	if !ok {
		return 0
	}
	return ws.Iteration
}
