//go:build !windows

package cmd

import (
	"golang.org/x/sys/unix"
)

// makeRaw puts the terminal into raw mode and returns the previous state.
func makeRaw(fd int) (*unix.Termios, error) {
	termios, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return nil, err
	}

	oldState := *termios

	// Disable canonical mode and echo
	termios.Lflag &^= unix.ICANON | unix.ECHO

	// Set minimum characters for read to 1, timeout to 1 (0.1s)
	termios.Cc[unix.VMIN] = 1
	termios.Cc[unix.VTIME] = 1

	if err := unix.IoctlSetTermios(fd, unix.TIOCSETA, termios); err != nil {
		return nil, err
	}

	return &oldState, nil
}

// restoreTerminal restores the terminal to its previous state.
func restoreTerminal(fd int, state *unix.Termios) {
	if state != nil {
		unix.IoctlSetTermios(fd, unix.TIOCSETA, state)
	}
}
