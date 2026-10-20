// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package expand

import (
	"io"
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/go-quicktest/qt"
	"mvdan.cc/sh/v3/syntax"
)

func parseWord(t *testing.T, src string, opts ...syntax.ParserOption) *syntax.Word {
	t.Helper()
	p := syntax.NewParser(opts...)
	word, err := p.Document(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	return word
}

func TestConfigNils(t *testing.T) {
	os.Setenv("EXPAND_GLOBAL", "value")
	tests := []struct {
		name string
		cfg  *Config
		src  string
		want string
	}{
		{
			"NilConfig",
			nil,
			"$EXPAND_GLOBAL",
			"",
		},
		{
			"ZeroConfig",
			&Config{},
			"$EXPAND_GLOBAL",
			"",
		},
		{
			"EnvConfig",
			&Config{Env: ListEnviron(os.Environ()...)},
			"$EXPAND_GLOBAL",
			"value",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			word := parseWord(t, tc.src)
			got, err := Literal(tc.cfg, word)
			if err != nil {
				t.Fatalf("did not want error, got %v", err)
			}
			if got != tc.want {
				t.Fatalf("wanted %q, got %q", tc.want, got)
			}
		})
	}
}

func TestMirBSDKornHash(t *testing.T) {
	t.Parallel()
	word := parseWord(t, "${foo@#}", syntax.Variant(syntax.LangMirBSDKorn))
	_, err := Literal(nil, word)
	qt.Assert(t, qt.ErrorMatches(err, "unsupported"))
}

func TestCmdSubstTooLarge(t *testing.T) {
	t.Parallel()
	cfg := &Config{CmdSubst: func(w io.Writer, cs *syntax.CmdSubst) error {
		// The expansion fails even if the handler ignores the write error.
		io.WriteString(w, strings.Repeat("x", 16<<20+1))
		return nil
	}}
	_, err := Literal(cfg, parseWord(t, "$(foo)"))
	qt.Assert(t, qt.ErrorMatches(err, "expansion would exceed 16777216 bytes"))
}

func TestFieldsIdempotency(t *testing.T) {
	tests := []struct {
		src  string
		want []string
	}{
		{
			"{1..4}",
			[]string{"1", "2", "3", "4"},
		},
		{
			"a{1..4}",
			[]string{"a1", "a2", "a3", "a4"},
		},
	}
	for _, tc := range tests {
		word := parseWord(t, tc.src)
		for range 2 {
			got, err := Fields(nil, word)
			if err != nil {
				t.Fatalf("did not want error, got %v", err)
			}
			qt.Assert(t, qt.DeepEquals(got, tc.want))
		}
	}
}

func TestFieldsEscapedGlob(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		Env: ListEnviron("PWD=/"),
		ReadDir2: func(string) ([]fs.DirEntry, error) {
			return []fs.DirEntry{
				&mockFileInfo{name: "a.go"},
				&mockFileInfo{name: "b.go"},
			}, nil
		},
	}
	tests := []struct {
		src  string
		want []string
	}{
		{`*.go`, []string{"a.go", "b.go"}},
		{`"*".go`, []string{"*.go"}},
		{`'*'.go`, []string{"*.go"}},
		{`\*.go`, []string{"*.go"}},
		{`\[a].go`, []string{"[a].go"}},
		{`a\*`, []string{"a*"}},
	}
	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			p := syntax.NewParser()
			var words []*syntax.Word
			for w, err := range p.WordsSeq(strings.NewReader(tc.src)) {
				qt.Assert(t, qt.IsNil(err))
				words = append(words, w)
			}
			got, err := Fields(cfg, words...)
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.DeepEquals(got, tc.want), qt.Commentf("input: %q", tc.src))
		})
	}
}

func Test_glob(t *testing.T) {
	cfg := &Config{
		ReadDir2: func(string) ([]fs.DirEntry, error) {
			return []fs.DirEntry{
				// The filenames here are sorted, just like [io/fs.ReadDirFS].
				&mockFileInfo{name: "A"},
				&mockFileInfo{name: "AB"},
				&mockFileInfo{name: "a"},
				&mockFileInfo{name: "ab"},
			}, nil
		},
	}

	tests := []struct {
		noCaseGlob bool
		pat        string
		want       []string
	}{
		{false, "a*", []string{"a", "ab"}},
		{false, "A*", []string{"A", "AB"}},
		{false, "*b", []string{"ab"}},
		{false, "b*", nil},
		{true, "a*", []string{"A", "AB", "a", "ab"}},
		{true, "A*", []string{"A", "AB", "a", "ab"}},
		{true, "*b", []string{"AB", "ab"}},
		{true, "b*", nil},
	}
	for _, tc := range tests {
		t.Run(tc.pat, func(t *testing.T) {
			cfg.NoCaseGlob = tc.noCaseGlob
			got, err := newExpander(cfg).glob("/", tc.pat)
			if err != nil {
				t.Fatalf("did not want error, got %v", err)
			}
			qt.Assert(t, qt.DeepEquals(got, tc.want))
		})
	}
}

// Test_globUnixPaths globs a virtual filesystem with Unix paths, which must
// not be resolved as OS paths on Windows: "/etc/*" is absolute, and the
// directories read and the matches use slashes.
func Test_globUnixPaths(t *testing.T) {
	fsys := fstest.MapFS{
		"tmp/file":  {},
		"etc/hosts": {},
	}
	cfg := &Config{
		ReadDir2: func(path string) ([]fs.DirEntry, error) {
			if path == "/" {
				return fs.ReadDir(fsys, ".")
			}
			if !strings.HasPrefix(path, "/") {
				return nil, &fs.PathError{Op: "readdir", Path: path, Err: fs.ErrNotExist}
			}
			return fs.ReadDir(fsys, path[1:])
		},
	}

	tests := []struct {
		base, pat string
		want      []string
	}{
		{"/", "/etc/*", []string{"/etc/hosts"}},
		{"/", "etc/*", []string{"etc/hosts"}},
		{"/", "*/hosts", []string{"etc/hosts"}},
		{"/", "/*/", []string{"/etc/", "/tmp/"}},
		{"/tmp", "*", []string{"file"}},
		{"/tmp", "../etc/*", []string{"../etc/hosts"}},
	}
	for _, tc := range tests {
		t.Run(tc.base+" "+tc.pat, func(t *testing.T) {
			got, err := newExpander(cfg).glob(tc.base, tc.pat)
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.DeepEquals(got, tc.want))
		})
	}
}

type mockFileInfo struct {
	name        string
	typ         fs.FileMode
	fs.DirEntry // Stub out everything but Name() & Type()
}

var _ fs.DirEntry = (*mockFileInfo)(nil)

func (fi *mockFileInfo) Name() string      { return fi.name }
func (fi *mockFileInfo) Type() fs.FileMode { return fi.typ }
