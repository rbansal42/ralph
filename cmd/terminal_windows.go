//go:build windows

package cmd

import "fmt"

type terminalState struct{}

// makeRaw is not supported on Windows — hotkeys are silently disabled.
func makeRaw(fd int) (*terminalState, error) {
	return nil, fmt.Errorf("raw terminal mode not supported on Windows")
}

// restoreTerminal is a no-op on Windows.
func restoreTerminal(fd int, state *terminalState) {}
