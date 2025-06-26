//go:build unix

package interp

import (
	"os/exec"
	"syscall"
)

// prepareCommand sets the SysProcAttr for the command to create a new process group.
func prepareCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// interruptCommand interrupts the whole process group.
func interruptCommand(cmd *exec.Cmd) error {
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
}

// killCommand kills the whole process group.
func killCommand(cmd *exec.Cmd) error {
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
