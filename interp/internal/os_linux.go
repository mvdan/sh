package actime

import (
	"io/fs"
	"syscall"
	"time"
)

func GetAtime(info fs.FileInfo) time.Time {
	stat, ok := info.Sys().(*syscall.Stat_t)

	if !ok {
		return info.ModTime()
	}
	return time.Unix(stat.Atim.Sec, stat.Atim.Nsec)
}
