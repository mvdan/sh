package interp

import (
	"io/fs"
	"syscall"
	"time"
)

func getAtime(info fs.FileInfo) time.Time {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return info.ModTime()
	}
	// Note that these are int32 on 32-bit platforms.
	return time.Unix(int64(stat.Atim.Sec), int64(stat.Atim.Nsec))
}
