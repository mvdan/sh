// Copyright (c) 2017, Andrey Nering <andrey.nering@gmail.com>
// See LICENSE for licensing information

//go:build !unix

package interp

import (
	"context"
	"fmt"

	"mvdan.cc/sh/v3/syntax"
)

func mkfifo(path string, mode uint32) error {
	return fmt.Errorf("unsupported")
}

// defaultAccess attempts to emulate access(2) on Windows.
// Windows seems to have a different system of permissions than Unix,
// so for now just rely on what [io/fs.FileInfo] gives us
// via the stat handler.
func defaultAccess(ctx context.Context, path string, mode AccessMode) error {
	info, err := HandlerCtx(ctx).runner.statHandler(ctx, path, false)
	if err != nil {
		return err
	}
	m := info.Mode()
	if mode&AccessRead != 0 && m&0o400 == 0 {
		return fmt.Errorf("file is not readable")
	}
	if mode&AccessWrite != 0 && m&0o200 == 0 {
		return fmt.Errorf("file is not writable")
	}
	if mode&AccessExec != 0 && m&0o100 == 0 {
		return fmt.Errorf("file is not executable")
	}
	return nil
}

// unTestOwnOrGrp panics. Under Unix, it implements the -O and -G unary tests,
// but under Windows, it's unclear how to implement those tests, since Windows
// doesn't have the concept of a file owner, just ACLs, and it's unclear how
// to map the one to the other.
func (r *Runner) unTestOwnOrGrp(ctx context.Context, op syntax.UnTestOperator, x string) bool {
	r.errf("unsupported unary test op: %v\n", op)
	return false
}

// waitStatus is a no-op where there are no signals: plan9, windows and js.
type waitStatus struct{}

// isENOEXEC is a no-op where there are no signals: plan9, windows and js.
func isENOEXEC(err error) bool { return false }

// isETXTBSY is a no-op where there are no signals: plan9, windows and js.
func isETXTBSY(err error) bool { return false }

func (waitStatus) Signaled() bool { return false }
func (waitStatus) Signal() int    { return 0 }

// killProcess is a no-op on plan9, windows and js/wasm, which have no
// signals to send.
func killProcess(pid, signum int) error {
	return fmt.Errorf("cannot signal processes on this platform")
}

// signalNames is the fallback signal table for platforms with no signals to
// ask about. It is Linux's numbering, which is as good a choice as any: these
// names never reach a kernel here, they only let a script say `kill -TERM %1`
// and have the job end.
//
// On unix this table does not exist; see os_unix.go for why asking the
// platform is the only way to be right on more than one of them.
var signalNames = map[int]string{
	1: "HUP", 2: "INT", 3: "QUIT", 4: "ILL", 5: "TRAP", 6: "ABRT", 7: "BUS",
	8: "FPE", 9: "KILL", 10: "USR1", 11: "SEGV", 12: "USR2", 13: "PIPE",
	14: "ALRM", 15: "TERM", 16: "STKFLT", 17: "CHLD", 18: "CONT", 19: "STOP",
	20: "TSTP", 21: "TTIN", 22: "TTOU", 23: "URG", 24: "XCPU", 25: "XFSZ",
	26: "VTALRM", 27: "PROF", 28: "WINCH", 29: "IO", 30: "PWR", 31: "SYS",
}

func signalName(num int) string { return signalNames[num] }

func signalNum(name string) int {
	for num, known := range signalNames {
		if known == name {
			return num
		}
	}
	return 0
}

// maxSignal bounds the search `kill -l` walks; see os_unix.go.
const maxSignal = 31
