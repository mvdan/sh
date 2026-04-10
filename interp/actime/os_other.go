package actime

import (
	"io/fs"
	"time"
)

func GetAtime(info fs.FileInfo) time.Time {
	return info.ModTime()
}
