package backend

import (
	"context"
	"fmt"
)

// Backend is the interface that all CLI coding agent backends must implement.
type Backend interface {
	// Name returns the backend identifier (e.g. "opencode", "claude").
	Name() string

	// CheckAuth verifies that the backend CLI is authenticated and can reach
	// the given model. Implementations should use a short timeout internally.
	CheckAuth(ctx context.Context, model string) error

	// RunPrompt executes the prompt file against the backend CLI in the given
	// workdir. It streams stdout to os.Stdout while also capturing the full
	// combined output. Returns the captured output, the process exit code, and
	// any execution error.
	RunPrompt(ctx context.Context, promptFile string, workdir string, model string) (output string, exitCode int, err error)

	// AuthGuide returns a human-readable multi-line string explaining how to
	// authenticate the backend.
	AuthGuide() string
}

// New returns a Backend for the given name. Supported names: "opencode", "claude".
func New(name string) (Backend, error) {
	switch name {
	case "opencode":
		return &OpenCode{}, nil
	case "claude":
		return &Claude{}, nil
	default:
		return nil, fmt.Errorf("unknown backend %q (supported: opencode, claude)", name)
	}
}
