// Copyright (c) 2017, Andrey Nering <andrey.nering@gmail.com>
// See LICENSE for licensing information

//go:build unix

package interp

import (
	"context"
	"os/user"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
	"mvdan.cc/sh/v3/syntax"
)

func mkfifo(path string, mode uint32) error {
	return unix.Mkfifo(path, mode)
}

// DefaultAccessHandler returns the [AccessHandlerFunc] used by default on Unix systems.
// It uses permission bits from [io/fs.FileInfo] via the stat handler.
func DefaultAccessHandler() AccessHandlerFunc {
	return func(ctx context.Context, path string, mode uint32) error {
		return unix.Access(path, mode)
	}
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
