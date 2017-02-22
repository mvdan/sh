// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package fileutil

import (
	"os"
	"regexp"
	"strings"
)

var (
	shebangRe = regexp.MustCompile(`^#!\s?/(usr/)?bin/(env\s+)?(sh|bash)\s`)
	extRe     = regexp.MustCompile(`\.(sh|bash)$`)
)

func HasShebang(bs []byte) bool {
	return shebangRe.Match(bs)
}

type ScriptConfidence int

const (
	ConfNotScript ScriptConfidence = iota
	ConfIfShebang
	ConfIsScript
)

func CouldBeScript(info os.FileInfo) ScriptConfidence {
	name := info.Name()
	switch {
	case info.IsDir(), name[0] == '.', !info.Mode().IsRegular():
		return ConfNotScript
	case extRe.MatchString(name):
		return ConfIsScript
	case strings.Contains(name, "."):
		return ConfNotScript // different extension
	case info.Size() < int64(len("#/bin/sh\n")):
		return ConfNotScript // cannot possibly hold valid shebang
	default:
		return ConfIfShebang
	}
}
