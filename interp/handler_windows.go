//go:build windows

package interp

import (
	"os"
	"os/exec"
)

// prepareCommand is a no-op on Windows.
func prepareCommand(cmd *exec.Cmd) {}

// interruptCommand interrupts the whole process group.
func interruptCommand(cmd *exec.Cmd) error {
	return cmd.Process.Signal(os.Kill)
}

// killCommand kills the whole process group.
func killCommand(cmd *exec.Cmd) error {
	return cmd.Process.Signal(os.Kill)
}
