// Copyright (c) 2017, Andrey Nering <andrey.nering@gmail.com>
// See LICENSE for licensing information

//go:build unix

package interp

import (
	"context"
	"errors"
	"os/user"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
	"mvdan.cc/sh/v3/syntax"
)

func mkfifo(path string, mode uint32) error {
	return unix.Mkfifo(path, mode)
}

// defaultAccess is similar to checking the permission bits from [io/fs.FileInfo],
// but it also takes into account the current user's role.
func defaultAccess(ctx context.Context, path string, mode AccessMode) error {
	return unix.Access(path, uint32(mode))
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

// isENOEXEC reports whether the kernel refused to execute a file
// with ENOEXEC, e.g. a script without a shebang line.
func isENOEXEC(err error) bool { return errors.Is(err, syscall.ENOEXEC) }

// isETXTBSY reports whether the kernel refused to execute a file
// with ETXTBSY, i.e. a process holds it open for writing.
func isETXTBSY(err error) bool { return errors.Is(err, syscall.ETXTBSY) }

// killProcess sends a signal to a process that is not one of this runner's
// jobs, for the kill builtin given a PID it does not know.
func killProcess(pid, signum int) error {
	return unix.Kill(pid, unix.Signal(signum))
}

// signalName and signalNum resolve signal names through the running platform
// rather than a table compiled into the shell.
//
// The numbers are not portable and the differences are not obscure: SIGUSR1 is
// 10 on Linux and 30 on darwin, where 10 is SIGBUS. A hardcoded Linux table
// meant `kill -USR1 <pid>` on macOS reached a real process with SIGBUS, which
// is a fault, not a user signal. Asking the platform is the only way to be
// right on more than one of them, and x/sys/unix already has the tables.
//
// SIG is not part of the name here: the builtin accepts and prints USR1, and
// x/sys/unix wants SIGUSR1, so the prefix is added and stripped at this
// boundary and nowhere else.
func signalName(num int) string {
	return strings.TrimPrefix(unix.SignalName(syscall.Signal(num)), "SIG")
}

func signalNum(name string) int {
	return int(unix.SignalNum("SIG" + name))
}

// maxSignal bounds the search `kill -l` walks to list every signal the
// platform knows. Signal numbers are small and dense everywhere this builds;
// 64 covers Linux's realtime range, which is the widest of them.
const maxSignal = 64
