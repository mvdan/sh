// Copyright (c) 2026, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package shell

import (
	"io/fs"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

// Glob returns the names of all files in fsys matching the shell pattern,
// following Bash's pathname expansion rather than the rules of [fs.Glob].
// Names are slash-separated and relative to the root of fsys;
// use [os.DirFS] to glob the filesystem directly like [filepath.Glob].
//
// The pattern is parsed like the arguments to a command, as in [Fields],
// so it may contain several patterns separated by spaces, and names containing
// spaces must be quoted or escaped. Braces are expanded, "**" matches any
// number of directories like Bash's globstar option, and "*" does not match
// names beginning with a dot. No environment variables are set, so parameter
// expansions without a default value result in an error.
//
// Patterns which match no files result in no names, and words without any
// pattern metacharacters are returned as-is, whether or not the file exists.
//
// An error will be reported if the input string had invalid syntax.
func Glob(fsys fs.FS, pattern string) ([]string, error) {
	p := syntax.NewParser()
	var words []*syntax.Word
	for w, err := range p.WordsSeq(strings.NewReader(pattern)) {
		if err != nil {
			return nil, err
		}
		words = append(words, w)
	}
	cfg := &expand.Config{
		// Relative patterns are resolved from $PWD; use the root of fsys.
		Env: expand.ListEnviron("PWD=."),
		ReadDir2: func(path string) ([]fs.DirEntry, error) {
			// The expand package uses OS-specific paths, while io/fs uses
			// slash-separated paths relative to the root. Anything else,
			// such as an absolute path, is treated as an empty directory.
			// TODO(v4): once expand globs with io/fs semantics, pass fsys
			// directly and drop this adapter.
			path = filepath.ToSlash(path)
			if path == "" {
				path = "."
			}
			if !fs.ValidPath(path) {
				return nil, nil
			}
			return fs.ReadDir(fsys, path)
		},
		GlobStar: true,
		NullGlob: true,
		NoUnset:  true,
	}
	names, err := expand.Fields(cfg, words...)
	if err != nil {
		return nil, err
	}
	// TODO(v4): remove once expand joins glob results with slashes.
	if filepath.Separator != '/' {
		for i, name := range names {
			names[i] = filepath.ToSlash(name)
		}
	}
	return names, nil
}
