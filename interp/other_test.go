// Copyright (c) 2026, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

//go:build !unix && !windows

package interp_test

// shortPathName only means anything on Windows; the call site is guarded by a
// runtime GOOS check. It is defined here as well so that the test binary
// builds on platforms which are neither unix nor windows, such as js/wasm.
func shortPathName(path string) (string, error) {
	panic("only works on windows")
}
