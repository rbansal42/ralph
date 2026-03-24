package backend

import (
	"context"
	"fmt"
	"io"
	"os"
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
// If output is non-nil, subprocess stdout is streamed there instead of os.Stdout.
func New(name string, output io.Writer) (Backend, error) {
	if output == nil {
		output = os.Stdout
	}
	switch name {
	case "opencode":
		return &OpenCode{Output: output}, nil
	case "claude":
		return &Claude{Output: output}, nil
	default:
		return nil, fmt.Errorf("unknown backend %q (supported: opencode, claude)", name)
	}
}
