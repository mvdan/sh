package actime

import (
	"io/fs"
	"syscall"
	"time"
)

func GetAtime(info fs.FileInfo) time.Time {
	stat := info.Sys().(*syscall.Win32FileAttributeData)
	return time.Unix(0, stat.LastAccessTime.Nanoseconds())
}
