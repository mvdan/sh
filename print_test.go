// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFprintCompact(t *testing.T) {
	for i, c := range astTests {
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			in := c.strs[0]
			prog, err := Parse(strings.NewReader(in), "", 0)
			if err != nil {
				t.Fatal(err)
			}
			want := in
			got := strFprint(prog, 0)
			if len(got) > 0 {
				got = got[:len(got)-1]
			}
			if got != want {
				t.Fatalf("Fprint mismatch\nwant: %q\ngot:  %q",
					want, got)
			}
		})
	}
}

func strFprint(node Node, spaces int) string {
	var buf bytes.Buffer
	c := PrintConfig{Spaces: spaces}
	c.Fprint(&buf, node)
	return buf.String()
}

func TestFprintWeirdFormat(t *testing.T) {
	var weirdFormats = [...]struct {
		in, want string
	}{
		{"foo; bar", "foo\nbar"},
		{"foo\n\n\nbar", "foo\n\nbar"},
		{"foo\n\n", "foo"},
		{"\n\nfoo", "foo"},
		{"# foo\n # bar", "# foo\n# bar"},
		{"a=b # inline\nbar", "a=b # inline\nbar"},
		{"a=`b` # inline", "a=`b` # inline"},
		{"`a` `b`", "`a` `b`"},
		{"if a\nthen\n\tb\nfi", "if a; then\n\tb\nfi"},
		{"if a; then\nb\nelse\nfi", "if a; then\n\tb\nfi"},
		{"foo >&2 <f bar", "foo >&2 <f bar"},
		{"foo >&2 bar <f", "foo >&2 bar <f"},
		{"foo >&2 bar <f bar2", "foo >&2 bar bar2 <f"},
		{"foo <<EOF bar\nl1\nEOF", "foo bar <<EOF\nl1\nEOF"},
		{
			"foo <<EOF && bar\nl1\nEOF",
			"foo <<EOF && bar\nl1\nEOF",
		},
		{
			"foo <<EOF &&\nl1\nEOF\nbar",
			"foo <<EOF && bar\nl1\nEOF",
		},
		{
			"foo <<EOF\nl1\nEOF\n\nfoo2",
			"foo <<EOF\nl1\nEOF\n\nfoo2",
		},
		{
			"{ foo; bar; }",
			"{\n\tfoo\n\tbar\n}",
		},
		{
			"(foo; bar)",
			"(\n\tfoo\n\tbar\n)",
		},
		{
			"{\nfoo\nbar; }",
			"{\n\tfoo\n\tbar\n}",
		},
		{
			"{\nbar\n# extra\n}",
			"{\n\tbar\n\t# extra\n}",
		},
		{
			"foo\nbar  # extra",
			"foo\nbar # extra",
		},
		{
			"foo # 1\nfooo # 2\nfo # 3",
			"foo  # 1\nfooo # 2\nfo   # 3",
		},
		{
			"foo   # 1\nfooo  # 2\nfo    # 3",
			"foo  # 1\nfooo # 2\nfo   # 3",
		},
		{
			"fooooo\nfoo # 1\nfooo # 2\nfo # 3\nfooooo",
			"fooooo\nfoo  # 1\nfooo # 2\nfo   # 3\nfooooo",
		},
		{
			"foo\nbar\nfoo # 1\nfooo # 2",
			"foo\nbar\nfoo  # 1\nfooo # 2",
		},
		{
			"foobar # 1\nfoo\nfoo # 2",
			"foobar # 1\nfoo\nfoo # 2",
		},
		{
			"foobar # 1\n#foo\nfoo # 2",
			"foobar # 1\n#foo\nfoo # 2",
		},
		{
			"foobar # 1\n\nfoo # 2",
			"foobar # 1\n\nfoo # 2",
		},
		{
			"foo # 2\nfoo2 bar # 1",
			"foo      # 2\nfoo2 bar # 1",
		},
		{
			"foo bar # 1\n! foo # 2",
			"foo bar # 1\n! foo   # 2",
		},
		{
			"foo bar # 1\n! foo # 2",
			"foo bar # 1\n! foo   # 2",
		},
		{
			"foo; foooo # 1",
			"foo\nfoooo # 1",
		},
		{
			"(\nbar\n# extra\n)",
			"(\n\tbar\n\t# extra\n)",
		},
		{
			"for a in 1 2\ndo\n\t# bar\ndone",
			"for a in 1 2; do\n\t# bar\ndone",
		},
		{
			"for a in 1 2; do\n\n\tbar\ndone",
			"for a in 1 2; do\n\n\tbar\ndone",
		},
		{
			"a \\\n\t&& b",
			"a \\\n\t&& b",
		},
		{
			"a \\\n\t&& b\nc",
			"a \\\n\t&& b\nc",
		},
		{
			"{\n(a \\\n&& b)\nc\n}",
			"{\n\t(a \\\n\t\t&& b)\n\tc\n}",
		},
		{
			"a && b \\\n&& c",
			"a && b \\\n\t&& c",
		},
		{
			"a \\\n&& $(b) && c \\\n&& d",
			"a \\\n\t&& $(b) && c \\\n\t&& d",
		},
		{
			"a \\\n&& b\nc \\\n&& d",
			"a \\\n\t&& b\nc \\\n\t&& d",
		},
		{
			"a | {\nb \\\n| c\n}",
			"a | {\n\tb \\\n\t\t| c\n}",
		},
		{
			"a \\\n\t&& if foo; then\nbar\nfi",
			"a \\\n\t&& if foo; then\n\t\tbar\n\tfi",
		},
		{
			"if\nfoo\nthen\nbar\nfi",
			"if\n\tfoo\nthen\n\tbar\nfi",
		},
		{
			"if foo \\\nbar\nthen\nbar\nfi",
			"if foo \\\n\tbar; then\n\tbar\nfi",
		},
		{
			"if foo \\\n&& bar\nthen\nbar\nfi",
			"if foo \\\n\t&& bar; then\n\tbar\nfi",
		},
		{
			"a |\nb |\nc",
			"a \\\n\t| b \\\n\t| c",
		},
		{
			"foo |\n# misplaced\nbar",
			"foo \\\n\t| bar # misplaced",
		},
		{
			"foo | while read l; do\nbar\ndone",
			"foo | while read l; do\n\tbar\ndone",
		},
		{
			"\"\\\nfoo\\\n  bar\"",
			"\"\\\nfoo\\\n  bar\"",
		},
		{
			"foo \\\n>bar\netc",
			"foo \\\n\t>bar\netc",
		},
		{
			"foo \\\nfoo2 \\\n>bar",
			"foo \\\n\tfoo2 \\\n\t>bar",
		},
		{
			"case $i in\n1)\nfoo\n;;\nesac",
			"case $i in\n\t1)\n\t\tfoo\n\t\t;;\nesac",
		},
		{
			"case $i in\n1)\nfoo\nesac",
			"case $i in\n\t1)\n\t\tfoo\n\t\t;;\nesac",
		},
		{
			"case $i in\n1) foo\nesac",
			"case $i in\n\t1) foo ;;\nesac",
		},
		{
			"case $i in\n1) foo; bar\nesac",
			"case $i in\n\t1)\n\t\tfoo\n\t\tbar\n\t\t;;\nesac",
		},
		{
			"case $i in\n1) foo; bar;;\nesac",
			"case $i in\n\t1)\n\t\tfoo\n\t\tbar\n\t\t;;\nesac",
		},
		{
			"a=(\nb\nc\n) foo",
			"a=(\n\tb\n\tc\n) foo",
		},
		{
			"foo <<EOF | `bar`\n3\nEOF",
			"foo <<EOF | `bar`\n3\nEOF",
		},
	}

	for i, tc := range weirdFormats {
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			for _, s := range [...]string{"", "\n"} {
				in := s + tc.in + s
				prog, err := Parse(strings.NewReader(in), "",
					ParseComments)
				if err != nil {
					t.Fatal(err)
				}
				want := tc.want + "\n"
				got := strFprint(prog, 0)
				if got != want {
					t.Fatalf("Fprint mismatch:\n"+
						"in:\n%s\nwant:\n%sgot:\n%s",
						in, want, got)
				}
			}
		})
	}
}

func parsePath(tb testing.TB, path string) File {
	f, err := os.Open(path)
	if err != nil {
		tb.Fatal(err)
	}
	defer f.Close()
	prog, err := Parse(f, "", ParseComments)
	if err != nil {
		tb.Fatal(err)
	}
	return prog
}

func TestFprintMultiline(t *testing.T) {
	path := filepath.Join("testdata", "canonical.sh")
	prog := parsePath(t, path)
	got := strFprint(prog, 0)

	want, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("Fprint mismatch in canonical.sh")
	}
}

func TestFprintSpaces(t *testing.T) {
	var spaceFormats = [...]struct {
		spaces   int
		in, want string
	}{
		{
			0,
			"{\nfoo \\\nbar\n}",
			"{\n\tfoo \\\n\t\tbar\n}",
		},
		{
			-1,
			"{\nfoo \\\nbar\n}",
			"{\nfoo \\\nbar\n}",
		},
		{
			2,
			"{\nfoo \\\nbar\n}",
			"{\n  foo \\\n    bar\n}",
		},
		{
			4,
			"{\nfoo \\\nbar\n}",
			"{\n    foo \\\n        bar\n}",
		},
	}

	for i, tc := range spaceFormats {
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			prog, err := Parse(strings.NewReader(tc.in), "", ParseComments)
			if err != nil {
				t.Fatal(err)
			}
			want := tc.want + "\n"
			got := strFprint(prog, tc.spaces)
			if got != want {
				t.Fatalf("Fprint mismatch:\nin:\n%s\nwant:\n%sgot:\n%s",
					tc.in, want, got)
			}
		})
	}
}

var errBadWriter = fmt.Errorf("write: expected error")

type badWriter struct{}

func (b badWriter) Write(p []byte) (int, error) { return 0, errBadWriter }

func TestWriteErr(t *testing.T) {
	var out badWriter
	f := File{
		Stmts: []Stmt{
			{
				Redirs: []Redirect{{}},
				Cmd:    Subshell{},
			},
		},
	}
	err := Fprint(out, f)
	if err == nil {
		t.Fatalf("Expected error with bad writer")
	}
	if err != errBadWriter {
		t.Fatalf("Error mismatch with bad writer:\nwant: %v\ngot:  %v",
			errBadWriter, err)
	}
}
