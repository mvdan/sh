// Copyright (c) 2017, Andrey Nering <andrey.nering@gmail.com>
// See LICENSE for licensing information

//go:build unix

package interp

import (
	"context"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
	"mvdan.cc/sh/v3/syntax"
)

func mkfifo(path string, mode uint32) error {
	return unix.Mkfifo(path, mode)
}

// access is similar to checking the permission bits from [io/fs.FileInfo],
// but it also takes into account the current user's role.
func (r *Runner) access(ctx context.Context, path string, mode uint32) error {
	// TODO(v4): "access" may need to become part of a handler, like "open" or "stat".
	return unix.Access(path, mode)
}

// unTestOwnOrGrp implements the -O and -G unary tests. If the file does not
// exist, or the current user cannot be retrieved, returns false.
func (r *Runner) unTestOwnOrGrp(ctx context.Context, op syntax.UnTestOperator, x string) bool {
	info, err := r.stat(ctx, x)
	if err != nil {
		return false
	}
	u, err := user.Current()
	if err != nil {
		return false
	}
	if op == syntax.TsUsrOwn {
		uid, _ := strconv.Atoi(u.Uid)
		return uint32(uid) == info.Sys().(*syscall.Stat_t).Uid
	}
	gid, _ := strconv.Atoi(u.Gid)
	return uint32(gid) == info.Sys().(*syscall.Stat_t).Gid
}

type waitStatus = syscall.WaitStatus

// prepareCommand sets the SysProcAttr for the command.
// If processGroup is true, a new process group is created.
func prepareCommand(cmd *exec.Cmd, processGroup bool) {
	if processGroup {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
}

// postStartCommand is a no-op on Unix.
func postStartCommand(cmd *exec.Cmd, processGroup bool) {}

// interruptCommand sends SIGINT to the process or its process group.
func interruptCommand(cmd *exec.Cmd, processGroup bool) error {
	if processGroup {
		return unix.Kill(-cmd.Process.Pid, unix.SIGINT)
	}
	return cmd.Process.Signal(syscall.SIGINT)
}

// killCommand sends SIGKILL to the process or its process group.
func killCommand(cmd *exec.Cmd, processGroup bool) error {
	if processGroup {
		return unix.Kill(-cmd.Process.Pid, unix.SIGKILL)
	}
	return cmd.Process.Kill()
}
