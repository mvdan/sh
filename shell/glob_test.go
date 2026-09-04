// Copyright (c) 2026, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package shell

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/go-quicktest/qt"
)

func TestGlob(t *testing.T) {
	t.Parallel()
	files := []string{
		"a.go", "b.go", ".hidden.go", "with space.txt",
		"sub/c.go", "sub/deep/d.go", "sub/.dot/e.go",
	}
	dir := t.TempDir()
	mapFS := fstest.MapFS{}
	for _, name := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		qt.Assert(t, qt.IsNil(os.MkdirAll(filepath.Dir(path), 0o777)))
		qt.Assert(t, qt.IsNil(os.WriteFile(path, nil, 0o666)))
		mapFS[name] = &fstest.MapFile{}
	}

	tests := []struct {
		in   string
		want []string
	}{
		{"*.go", []string{"a.go", "b.go"}},
		{"?.go", []string{"a.go", "b.go"}},
		{"[a].go", []string{"a.go"}},
		{".*.go", []string{".hidden.go"}},
		{"sub/*", []string{"sub/c.go", "sub/deep"}},
		{"sub/*/*.go", []string{"sub/deep/d.go"}},
		{"**/*.go", []string{"a.go", "b.go", "sub/c.go", "sub/deep/d.go"}},
		{"sub/**", []string{"sub/", "sub/c.go", "sub/deep", "sub/deep/d.go"}},
		{"*.zzz", nil},
		{"", nil},

		// Several patterns, quoting, braces, and default values.
		{"*.go *.txt", []string{"a.go", "b.go", "with space.txt"}},
		{"'with space.txt'", []string{"with space.txt"}},
		{`with\ *.txt`, []string{"with space.txt"}},
		{"with *.txt", []string{"with", "with space.txt"}},
		{"{a,z}.go", []string{"a.go", "z.go"}},
		{"${x:-sub}/*.go", []string{"sub/c.go"}},

		// Words without metacharacters are returned as-is.
		{"a.go", []string{"a.go"}},
		{"missing.go", []string{"missing.go"}},
		{`\*.go`, []string{"*.go"}},

		// Paths outside of the filesystem root match nothing.
		{"../*", nil},
	}
	for name, fsys := range map[string]fs.FS{"MapFS": mapFS, "DirFS": os.DirFS(dir)} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, tc := range tests {
				got, err := Glob(fsys, tc.in)
				qt.Assert(t, qt.IsNil(err), qt.Commentf("pattern %q", tc.in))
				qt.Assert(t, qt.DeepEquals(got, tc.want), qt.Commentf("pattern %q", tc.in))
			}

			_, err := Glob(fsys, "'unclosed")
			qt.Assert(t, qt.IsNotNil(err))
			_, err = Glob(fsys, "$(echo foo)")
			qt.Assert(t, qt.ErrorMatches(err, `unexpected command substitution.*`))
			_, err = Glob(fsys, "$x/*.go")
			qt.Assert(t, qt.ErrorMatches(err, `x: unbound variable`))
		})
	}
}
