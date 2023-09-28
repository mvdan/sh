// Copyright (c) 2017, Andrey Nering <andrey.nering@gmail.com>
// See LICENSE for licensing information

//go:build !unix

package interp

import (
	"fmt"
)

func mkfifo(path string, mode uint32) error {
	return fmt.Errorf("unsupported")
}

// hasPermissionToDir is a no-op on Windows.
func hasPermissionToDir(string) bool {
	return true
}
