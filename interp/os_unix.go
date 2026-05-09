// Copyright (c) 2017, Andrey Nering <andrey.nering@gmail.com>
// See LICENSE for licensing information

//go:build unix

package interp

import (
	"context"
	"os"
	"os/user"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
	"mvdan.cc/sh/v3/syntax"
)

func mkfifo(path string, mode uint32) error {
	return unix.Mkfifo(path, mode)
}

// dupPipeFd duplicates a pipe file descriptor, returning a new *os.File
// that refers to the same underlying pipe endpoint. The caller can close
// the original fd while the duplicate remains valid. This is used to
// ensure the parent process does not hold extra pipe fd references during
// pipeline execution, which would prevent EOF/SIGPIPE propagation.
func dupPipeFd(f *os.File) (*os.File, error) {
	newFd, err := syscall.Dup(int(f.Fd()))
	if err != nil {
		return nil, err
	}
	syscall.CloseOnExec(newFd)
	return os.NewFile(uintptr(newFd), f.Name()+"-dup"), nil
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
