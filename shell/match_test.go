// Copyright (c) 2026, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package shell

import (
	"testing"

	"github.com/go-quicktest/qt"
)

var matchTests = []struct {
	pat, name string
	want      bool
}{
	{"foo", "foo", true},
	{"foo", "foobar", false},
	{"foo*", "foobar", true},
	{"*bar", "foobar", true},
	{"f?o", "foo", true},
	{"f?o", "fo", false},
	{"[fg]oo", "goo", true},
	{"[!f]oo", "foo", false},
	{"[[:digit:]]*", "1abc", true},
	{`\*`, "*", true},
	{`\*`, "a", false},
	// Unmatched brackets and unclosed groups are literals, like in Bash.
	{"[a", "[a", true},
	{"@(a|b", "@(a|b", true},
	{"@(a|b", "a", false},
	{"", "", true},
	{"", "a", false},

	// Slashes are ordinary characters.
	{"*", "a/b", true},
	{"a?b", "a/b", true},
	{"*.go", "dir/file.go", true},
	{"*", ".hidden", true},

	// Extended operators are always recognized.
	{"@(foo|bar)", "bar", true},
	{"@(foo|bar)", "baz", false},
	{"*(ab)", "ababab", true},
	{"+(ab)", "", false},
	{"?(ab)", "", true},
	{"!(*.go)", "main.md", true},
	{"!(*.go)", "main.go", false},
	{"src/!(test).go", "src/main.go", true},
}

func TestMatch(t *testing.T) {
	t.Parallel()
	for _, tc := range matchTests {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			got, err := Match(tc.pat, tc.name)
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Equals(got, tc.want), qt.Commentf("pattern %q, name %q", tc.pat, tc.name))
		})
	}
}

func TestMatchError(t *testing.T) {
	t.Parallel()
	for _, pat := range []string{
		`foo\`,
		// Negation is only supported with a fixed prefix and suffix.
		"src/!(test)/*",
		"!(a)!(b)",
	} {
		_, err := Match(pat, "x")
		qt.Assert(t, qt.IsNotNil(err), qt.Commentf("pattern %q", pat))
	}
}
