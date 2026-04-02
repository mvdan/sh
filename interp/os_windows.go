// Copyright (c) 2017, Andrey Nering <andrey.nering@gmail.com>
// See LICENSE for licensing information

//go:build windows

package interp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"mvdan.cc/sh/v3/syntax"
)

func mkfifo(path string, mode uint32) error {
	return fmt.Errorf("unsupported")
}

// access attempts to emulate [unix.Access] on Windows.
// Windows seems to have a different system of permissions than Unix,
// so for now just rely on what [io/fs.FileInfo] gives us.
func (r *Runner) access(ctx context.Context, path string, mode uint32) error {
	info, err := r.lstat(ctx, path)
	if err != nil {
		return err
	}
	m := info.Mode()
	switch mode {
	case access_R_OK:
		if m&0o400 == 0 {
			return fmt.Errorf("file is not readable")
		}
	case access_W_OK:
		if m&0o200 == 0 {
			return fmt.Errorf("file is not writable")
		}
	case access_X_OK:
		if m&0o100 == 0 {
			return fmt.Errorf("file is not executable")
		}
	}
	return nil
}

// unTestOwnOrGrp panics. Under Unix, it implements the -O and -G unary tests,
// but under Windows, it's unclear how to implement those tests, since Windows
// doesn't have the concept of a file owner, just ACLs, and it's unclear how
// to map the one to the other.
func (r *Runner) unTestOwnOrGrp(ctx context.Context, op syntax.UnTestOperator, x string) bool {
	panic(fmt.Sprintf("unhandled unary test op: %v", op))
}

// waitStatus is a no-op on windows.
type waitStatus struct{}

func (waitStatus) Signaled() bool { return false }
func (waitStatus) Signal() int    { return 0 }

// jobHandles maps process IDs to their job object handles.
var jobHandles sync.Map

// prepareCommand sets the SysProcAttr for the command.
// If processGroup is true, the process is created suspended and will be
// assigned to a job object in postStartCommand.
func prepareCommand(cmd *exec.Cmd, processGroup bool) {
	if processGroup {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			CreationFlags: windows.CREATE_SUSPENDED | windows.CREATE_NEW_PROCESS_GROUP,
		}
	}
}

// postStartCommand assigns the process to a job object if processGroup is true,
// then resumes the process. This must be called immediately after cmd.Start().
func postStartCommand(cmd *exec.Cmd, processGroup bool) {
	if !processGroup || cmd.Process == nil {
		return
	}

	// Create a job object.
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		cmd.Process.Kill()
		return
	}

	// Set the job to kill all processes when the job handle is closed.
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE

	_, err = windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil {
		windows.CloseHandle(job)
		cmd.Process.Kill()
		return
	}

	// Assign the process to the job.
	procHandle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		windows.CloseHandle(job)
		cmd.Process.Kill()
		return
	}
	defer windows.CloseHandle(procHandle)
	err = windows.AssignProcessToJobObject(job, procHandle)
	if err != nil {
		windows.CloseHandle(job)
		cmd.Process.Kill()
		return
	}

	// Store the job handle for later termination.
	jobHandles.Store(cmd.Process.Pid, job)

	// Resume the process.
	_, err = windows.ResumeThread(procHandle)
	if err != nil {
		// If resume fails, kill the job.
		windows.TerminateJobObject(job, 1)
		windows.CloseHandle(job)
		jobHandles.Delete(cmd.Process.Pid)
	} else {
		// Clean up the job handle when the process exits.
		pid := cmd.Process.Pid
		go func() {
			cmd.Wait()
			if handle, ok := jobHandles.LoadAndDelete(pid); ok {
				windows.CloseHandle(handle.(windows.Handle))
			}
		}()
	}
}

// interruptCommand sends CTRL_C_EVENT to the process group if processGroup is true,
// otherwise signals the individual process. If sending the console event fails,
// it falls back to terminating the job object.
func interruptCommand(cmd *exec.Cmd, processGroup bool) error {
	if processGroup {
		// Try to send CTRL_C_EVENT to the process group.
		// This requires that the process was created with CREATE_NEW_PROCESS_GROUP.
		err := windows.GenerateConsoleCtrlEvent(windows.CTRL_C_EVENT, uint32(cmd.Process.Pid))
		if err == nil {
			return nil
		}
		// If sending console event fails, fall back to terminating the job object.
		if job, ok := jobHandles.Load(cmd.Process.Pid); ok {
			return windows.TerminateJobObject(job.(windows.Handle), 1)
		}
	}
	return cmd.Process.Signal(os.Interrupt)
}

// killCommand terminates the job object if processGroup is true,
// otherwise kills the individual process.
func killCommand(cmd *exec.Cmd, processGroup bool) error {
	if processGroup {
		if job, ok := jobHandles.LoadAndDelete(cmd.Process.Pid); ok {
			handle := job.(windows.Handle)
			err := windows.TerminateJobObject(handle, 1)
			windows.CloseHandle(handle)
			return err
		}
	}
	return cmd.Process.Kill()
}
