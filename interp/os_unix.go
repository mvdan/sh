// Copyright (c) 2017, Andrey Nering <andrey.nering@gmail.com>
// See LICENSE for licensing information

//go:build unix

package interp

import (
	"golang.org/x/sys/unix"
)

func mkfifo(path string, mode uint32) error {
	return unix.Mkfifo(path, mode)
}

// hasPermissionToDir returns true if the OS current user has execute
// permission to the given directory
func hasPermissionToDir(path string) bool {
	return unix.Access(path, unix.X_OK) == nil
}
