package actime

import (
	"io/fs"
	"syscall"
	"time"
)

func GetAtime(info fs.FileInfo) time.Time {
	stat := info.Sys().(*syscall.Stat_t)
	return time.Unix(stat.Atimespec.Sec, stat.Atimespec.Nsec)
}
