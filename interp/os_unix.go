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

// hasPermissionToDir returns if the OS current user has execute permission
// to the given directory
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
	if perm&0o100 != 0 && st.Uid == uint32(uid) {
		return true
	}

	// Group perms only apply if you're not the owner of the directory.
	gid, _ := strconv.Atoi(user.Gid)
	inGroup := st.Gid == uint32(gid)
	if st.Uid != uint32(uid) {
		gids, _ := user.GroupIds()
		for _, gid := range gids {
			gid, _ := strconv.Atoi(gid)
			if st.Gid == uint32(gid) {
				// other users in group (g)
				if perm&0o010 != 0 {
					return true
				}
				inGroup = true
			}
		}
	}

	// remaining users (o) -- only apply if you're not the owner and none of
	// your groups match its group.
	if perm&0o001 != 0 && st.Uid != uint32(uid) && !inGroup {
		return true
	}

	return false
}
