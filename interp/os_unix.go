// Copyright (c) 2017, Andrey Nering <andrey.nering@gmail.com>
// See LICENSE for licensing information

//go:build unix

package interp

import (
	"context"
	"errors"
	"io/fs"
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
	err := unix.Access(path, mode)
	if err == nil {
		return nil
	}
	if !errors.Is(err, unix.ENOENT) && !errors.Is(err, unix.ENOTDIR) {
		return err
	}
	info, statErr := r.stat(ctx, path)
	if statErr != nil {
		return err
	}
	return accessFileMode(info.Mode(), mode)
}

func accessFileMode(mode fs.FileMode, access uint32) error {
	if access&access_R_OK != 0 && mode&0o400 == 0 {
		return errors.New("file is not readable")
	}
	if access&access_W_OK != 0 && mode&0o200 == 0 {
		return errors.New("file is not writable")
	}
	if access&access_X_OK != 0 && mode&0o100 == 0 {
		return errors.New("file is not executable")
	}
	return nil
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
