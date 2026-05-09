// Copyright (c) 2017, Andrey Nering <andrey.nering@gmail.com>
// See LICENSE for licensing information

//go:build !unix

package interp

import (
	"context"
	"fmt"
	"os"

	"mvdan.cc/sh/v3/syntax"
)

func mkfifo(path string, mode uint32) error {
	return fmt.Errorf("unsupported")
}

// dupPipeFd is a no-op on non-unix platforms; the original pipe fd is
// returned. Pipelines will still run, but EOF/SIGPIPE propagation is
// best-effort because the parent retains the original fd reference.
func dupPipeFd(f *os.File) (*os.File, error) {
	return f, nil
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
	r.errf("unsupported unary test op: %v\n", op)
	return false
}

// waitStatus is a no-op on plan9 and windows.
type waitStatus struct{}

func (waitStatus) Signaled() bool { return false }
func (waitStatus) Signal() int    { return 0 }
