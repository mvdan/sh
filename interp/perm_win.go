// Copyright (c) 2017, Andrey Nering <andrey.nering@gmail.com>
// See LICENSE for licensing information

// +build windows

package interp

import (
	"os"
)

// hasPermissionToDir is no-op on Windows
func hasPermissionToDir(info os.FileInfo) bool {
	return true
}
