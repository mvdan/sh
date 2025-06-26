//go:build plan9 || js

package interp

import (
	"os/exec"
)

// prepareCommand is a no-op.
func prepareCommand(cmd *exec.Cmd) {}

// interruptCommand interrupts the process killing it.
func interruptCommand(cmd *exec.Cmd) error {
	return cmd.Process.Kill()
}

// killCommand kills the process by killing it.
func killCommand(cmd *exec.Cmd) error {
	return cmd.Process.Kill()
}
