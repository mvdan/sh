// Copyright (c) 2026, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package internal

import (
	"path"
	"path/filepath"
	"runtime"
)

// Paths resolves paths as OS paths via [filepath], or as Unix paths via
// [path] for a virtual filesystem on Windows; see [PathsFor].
type Paths struct {
	virtual bool
}

// PathsFor returns how to resolve paths against the working directory dir.
//
// On Windows, [os.Getwd] and [filepath.Abs] always return a volume name, so a
// slash-rooted directory without one can only come from a caller setting it
// directly, for a virtual filesystem with Unix paths such as a remote Linux
// machine. Its paths are resolved via [path] rather than [filepath].
// Other platforms always use [filepath], which already resolves Unix paths,
// and on Plan 9 also recognizes absolute paths starting with "#".
func PathsFor(dir string) Paths {
	return Paths{virtual: runtime.GOOS == "windows" && path.IsAbs(dir) && filepath.VolumeName(dir) == ""}
}

// Separator returns the path separator as a string.
func (p Paths) Separator() string {
	if p.virtual {
		return "/"
	}
	return string(filepath.Separator)
}

// IsAbs is [filepath.IsAbs] or [path.IsAbs].
func (p Paths) IsAbs(name string) bool {
	if p.virtual {
		return path.IsAbs(name)
	}
	return filepath.IsAbs(name)
}

// Join is [filepath.Join] or [path.Join] for two elements.
func (p Paths) Join(elem1, elem2 string) string {
	if p.virtual {
		return path.Join(elem1, elem2)
	}
	return filepath.Join(elem1, elem2)
}

// Clean is [filepath.Clean] or [path.Clean].
func (p Paths) Clean(name string) string {
	if p.virtual {
		return path.Clean(name)
	}
	return filepath.Clean(name)
}
