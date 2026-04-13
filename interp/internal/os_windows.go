package actime

import (
	"io/fs"
	"syscall"
	"time"
)

func GetAtime(info fs.FileInfo) time.Time {
	stat, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return info.ModTime()
	}
	return time.Unix(0, stat.LastAccessTime.Nanoseconds())
}
