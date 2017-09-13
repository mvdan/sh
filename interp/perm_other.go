// Copyright (c) 2017, Andrey Nering <andrey.nering@gmail.com>
// See LICENSE for licensing information

// +build !windows

package interp

import (
	"os"
	"os/user"
	"strconv"
	"syscall"
)

// hasPermissionToDir returns if the OS current user has execute permission
// to the given directory
func hasPermissionToDir(info os.FileInfo) bool {
	st, _ := info.Sys().(*syscall.Stat_t)
	if st == nil {
		return true
	}

	user, err := user.Current()
	if err != nil {
		return false
	}
	perm := info.Mode().Perm()
	uid, _ := strconv.Atoi(user.Uid)
	gid, _ := strconv.Atoi(user.Gid)

	// user (u)
	if perm&0100 != 0 && st.Uid == uint32(uid) {
		return true
	}
	// other users in group (g)
	if perm&0010 != 0 && st.Uid != uint32(uid) && st.Gid == uint32(gid) {
		return true
	}
	// remaining users (o)
	if perm&0001 != 0 && st.Uid != uint32(uid) && st.Gid != uint32(gid) {
		return true
	}

	return false
}
