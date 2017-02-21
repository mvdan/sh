// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package fileutil

import (
	"os"
	"regexp"
	"strings"
)

var (
	validShebang = regexp.MustCompile(`^#!\s?/(usr/)?bin/(env\s+)?(sh|bash)`)
	shellFile    = regexp.MustCompile(`\.(sh|bash)$`)
)

func HasShellShebang(bs []byte) bool {
	return validShebang.Match(bs)
}

type ShellFileConfidence int

const (
	ConfNotShellFile ShellFileConfidence = iota
	ConfIfHasShebang
	ConfIsShellFile
)

func CouldBeShellFile(info os.FileInfo) ShellFileConfidence {
	name := info.Name()
	switch {
	case info.IsDir(), name[0] == '.', !info.Mode().IsRegular():
		return ConfNotShellFile
	case shellFile.MatchString(name):
		return ConfIsShellFile
	case strings.Contains(name, "."):
		return ConfNotShellFile // different extension
	case info.Size() < 8:
		return ConfNotShellFile // cannot possibly hold valid shebang
	default:
		return ConfIfHasShebang
	}
}
