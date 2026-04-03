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

// jobEntry holds the job object handle and a flag indicating whether it has
// already been terminated, so that the cleanup goroutine and killCommand /
// interruptCommand do not race to close the same handle.
type jobEntry struct {
	mu          sync.Mutex
	handle      windows.Handle
	closed      bool
}

// terminate terminates the job object and closes the handle exactly once.
// It is safe to call from multiple goroutines concurrently.
// When called from the cleanup goroutine after natural process exit, the
// TerminateJobObject call is a no-op because the job is already empty.
func (e *jobEntry) terminate(exitCode uint32) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	err := windows.TerminateJobObject(e.handle, exitCode)
	windows.CloseHandle(e.handle)
	return err
}

// jobHandles maps process IDs to their *jobEntry.
var jobHandles sync.Map

// ntResumeProcess is the NtResumeProcess syscall from ntdll.dll.
// Unlike ResumeThread (which requires a thread handle), NtResumeProcess
// accepts a process handle and resumes all suspended threads in the process.
// The required access right is PROCESS_SUSPEND_RESUME.
var ntResumeProcess = windows.NewLazyDLL("ntdll.dll").NewProc("NtResumeProcess")

// prepareCommand sets the SysProcAttr for the command.
// If processGroup is true, the process is created suspended and will be
// assigned to a job object in postStartCommand before being resumed.
func prepareCommand(cmd *exec.Cmd, processGroup bool) {
	if processGroup {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			CreationFlags: windows.CREATE_SUSPENDED | windows.CREATE_NEW_PROCESS_GROUP,
		}
	}
}

// postStartCommand assigns the process to a job object if processGroup is true,
// then resumes the process. This must be called immediately after cmd.Start().
//
// The process is started suspended (via CREATE_SUSPENDED in prepareCommand) to
// eliminate the race between process start and job object assignment: the process
// cannot spawn children until it is resumed, so all descendants will inherit the job.
func postStartCommand(cmd *exec.Cmd, processGroup bool) error {
	if !processGroup || cmd.Process == nil {
		return nil
	}

	pid := cmd.Process.Pid

	// Open the process with the rights needed for job assignment, termination,
	// and resuming (PROCESS_SUSPEND_RESUME).
	procHandle, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_SUSPEND_RESUME|windows.SYNCHRONIZE,
		false, uint32(pid))
	if err != nil {
		cmd.Process.Kill()
		return fmt.Errorf("OpenProcess: %w", err)
	}
	// procHandle is kept open for the lifetime of postStartCommand; the cleanup
	// goroutine below takes ownership and closes it when the process exits.

	// Create a job object.
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		windows.CloseHandle(procHandle)
		cmd.Process.Kill()
		return fmt.Errorf("CreateJobObject: %w", err)
	}

	// Set the job to kill all processes when the last job handle is closed.
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
		windows.CloseHandle(procHandle)
		cmd.Process.Kill()
		return fmt.Errorf("SetInformationJobObject: %w", err)
	}

	// Assign the process to the job before resuming it, so all child processes
	// it spawns will also belong to the job.
	if err = windows.AssignProcessToJobObject(job, procHandle); err != nil {
		windows.CloseHandle(job)
		windows.CloseHandle(procHandle)
		cmd.Process.Kill()
		return fmt.Errorf("AssignProcessToJobObject: %w", err)
	}

	entry := &jobEntry{handle: job}

	// Store the job entry before resuming so interruptCommand/killCommand can find it.
	jobHandles.Store(pid, entry)

	// Resume the suspended process. NtResumeProcess accepts a process handle
	// directly, unlike ResumeThread which requires a thread handle.
	// NTSTATUS 0 == STATUS_SUCCESS.
	//
	// We must pin procHandle as a uintptr passed to a syscall only during the
	// call itself; storing it before and using it here is correct because
	// procHandle is a windows.Handle (uintptr) that we own.
	if status, _, _ := ntResumeProcess.Call(uintptr(procHandle)); status != 0 {
		jobHandles.Delete(pid)
		entry.terminate(1)
		windows.CloseHandle(procHandle)
		return fmt.Errorf("NtResumeProcess: NTSTATUS 0x%x", status)
	}

	// Release the job handle when the process exits naturally.
	// procHandle was opened with SYNCHRONIZE so we can wait on it directly,
	// avoiding a second OpenProcess call.
	go func() {
		defer windows.CloseHandle(procHandle)
		windows.WaitForSingleObject(procHandle, windows.INFINITE)
		if e, ok := jobHandles.LoadAndDelete(pid); ok {
			e.(*jobEntry).terminate(0)
		}
	}()
	return nil
}

// interruptCommand sends CTRL_BREAK_EVENT to the process group if processGroup
// is true, otherwise signals the individual process.
//
// Note: processes created with CREATE_NEW_PROCESS_GROUP ignore CTRL_C_EVENT,
// so CTRL_BREAK_EVENT must be used instead. If the console event fails,
// the job object is terminated as a fallback.
func interruptCommand(cmd *exec.Cmd, processGroup bool) error {
	if processGroup {
		err := windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(cmd.Process.Pid))
		if err == nil {
			return nil
		}
		// Fall back to terminating the job object if the console event fails.
		if e, ok := jobHandles.LoadAndDelete(cmd.Process.Pid); ok {
			return e.(*jobEntry).terminate(1)
		}
	}
	return cmd.Process.Signal(os.Interrupt)
}

// killCommand terminates the job object if processGroup is true,
// otherwise kills the individual process.
func killCommand(cmd *exec.Cmd, processGroup bool) error {
	if processGroup {
		if e, ok := jobHandles.LoadAndDelete(cmd.Process.Pid); ok {
			return e.(*jobEntry).terminate(1)
		}
	}
	return cmd.Process.Kill()
}
