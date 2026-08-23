// Copyright (c) 2026, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

//go:build !windows

package interp_test

// shortPathName only means anything on Windows; the call site is guarded by a
// runtime GOOS check.
func shortPathName(path string) (string, error) {
	panic("only works on windows")
}
