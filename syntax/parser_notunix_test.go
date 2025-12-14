// Copyright (c) 2025, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

//go:build !unix

package syntax

import "os/exec"

func killCommandOnTestExit(cmd *exec.Cmd) {
	// We don't develop outside of Linux at the moment.
}
