// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

// Package fileutil contains code to work with shell files, also known
// as shell scripts.
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

// HasShebang reports whether bs begins with a valid sh or bash shebang.
// It supports variations with /usr and env.
func HasShebang(bs []byte) bool {
	return shebangRe.Match(bs)
}

// ScriptConfidence defines how likely a file is to be a shell script,
// from complete certainty that it is not one to complete certainty that
// it is one.
type ScriptConfidence int

const (
	ConfNotScript ScriptConfidence = iota
	ConfIfShebang
	ConfIsScript
)

// CouldBeScript reports how likely a file is to be a shell script. It
// discards directories, symlinks, hidden files and files with non-shell
// extensions.
func CouldBeScript(info os.FileInfo) ScriptConfidence {
	name := info.Name()
	switch {
	case info.IsDir(), name[0] == '.':
		return ConfNotScript
	case info.Mode()&os.ModeSymlink != 0:
		return ConfNotScript
	case extRe.MatchString(name):
		return ConfIsScript
	case strings.IndexByte(name, '.') > 0:
		return ConfNotScript // different extension
	case info.Size() < int64(len("#/bin/sh\n")):
		return ConfNotScript // cannot possibly hold valid shebang
	default:
		return ConfIfShebang
	}
}
