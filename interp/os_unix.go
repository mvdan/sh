// Copyright (c) 2017, Andrey Nering <andrey.nering@gmail.com>
// See LICENSE for licensing information

//go:build unix

package interp

import (
	"os"
	"os/user"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

func mkfifo(path string, mode uint32) error {
	return unix.Mkfifo(path, mode)
}

// hasPermissionToDir returns true if the OS current user has execute
// permission to the given directory
func hasPermissionToDir(info os.FileInfo) bool {
	user, err := user.Current()
	if err != nil {
		return false // unknown user; assume no permissions
	}
	uid, err := strconv.Atoi(user.Uid)
	if err != nil {
		return false // on POSIX systems, Uid should always be a decimal number
	}
	if uid == 0 {
		return true // super-user
	}

	st, _ := info.Sys().(*syscall.Stat_t)
	if st == nil {
		panic("unexpected info.Sys type")
	}
	perm := info.Mode().Perm()

	// user (u)
	if st.Uid == uint32(uid) {
		return perm&0o100 != 0
	}

	// group (g) -- check the users's actual group, and then all the other
	// groups they're in.
	gid, err := strconv.Atoi(user.Gid)
	if err != nil {
		return false // on POSIX systems, Gid should always be a decimal number
	}
	if st.Gid == uint32(gid) {
		return perm&0o010 != 0
	}
	gids, err := user.GroupIds()
	if err != nil {
		// If we can't get the list of group IDs, we can't know if the group
		// permissions, so default to false/no access.
		return false
	}
	for _, gid := range gids {
		gid, err := strconv.Atoi(gid)
		if err != nil {
			return false
		}
		if st.Gid == uint32(gid) {
			return perm&0o010 != 0
		}
	}

	// remaining users (o)
	return perm&0o001 != 0
}
