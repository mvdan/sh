//go:build !unix && !windows

package interp

import (
	"io/fs"
	"time"
)

func getAtime(info fs.FileInfo) time.Time {
	return info.ModTime()
}
