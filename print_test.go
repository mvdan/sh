// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFprintCompact(t *testing.T) {
	for i, c := range astTests {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			in := c.strs[0]
			prog, err := Parse(strings.NewReader(in), "", 0)
			if err != nil {
				t.Fatal(err)
			}
			want := in
			got := strFprint(prog)
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

func TestFprintWeirdFormat(t *testing.T) {
	var weirdFormats = [...]struct {
		in, want string
	}{
		{"foo; bar", "foo\nbar"},
		{"foo\n\n\nbar", "foo\n\nbar"},
		{"foo\n\n", "foo"},
		{"\n\nfoo", "foo"},
		{"a=b # inline\nbar", "a=b # inline\nbar"},
		{"a=`b` # inline", "a=`b` # inline"},
		{"`a` `b`", "`a` `b`"},
		{"if a\nthen\n\tb\nfi", "if a; then\n\tb\nfi"},
		{"foo >&2 <f bar", "foo >&2 <f bar"},
		{"foo >&2 bar <f", "foo >&2 bar <f"},
		{"foo >&2 bar <f bar2", "foo >&2 bar bar2 <f"},
		{"foo <<EOF bar\nl1\nEOF", "foo bar <<EOF\nl1\nEOF"},
		{
			"foo <<EOF && bar\nl1\nEOF",
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
			"a &&\nb &&\nc",
			"a &&\n\tb &&\n\tc",
		},
		{
			"\"\\\nfoo\\\n  bar\"",
			"\"\\\nfoo\\\n  bar\"",
		},
		{
			"foo \\\n>bar",
			"foo \\\n\t>bar",
		},
		{
			"foo \\\nfoo2 \\\n>bar",
			"foo \\\n\tfoo2 \\\n\t>bar",
		},
		{
			"case $i in\n1)\nfoo\n;;\nesac",
			"case $i in\n1)\n\tfoo\n\t;;\nesac",
		},
		{
			"case $i in\n1)\nfoo\nesac",
			"case $i in\n1)\n\tfoo\n\t;;\nesac",
		},
		{
			"case $i in\n1) foo\nesac",
			"case $i in\n1) foo;;\nesac",
		},
		{
			"case $i in\n1) foo; bar\nesac",
			"case $i in\n1)\n\tfoo\n\tbar\n\t;;\nesac",
		},
		{
			"case $i in\n1) foo; bar;;\nesac",
			"case $i in\n1)\n\tfoo\n\tbar\n\t;;\nesac",
		},
	}

	for i, tc := range weirdFormats {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			prog, err := Parse(strings.NewReader(tc.in), "", ParseComments)
			if err != nil {
				t.Fatal(err)
			}
			want := tc.want + "\n"
			got := strFprint(prog)
			if got != want {
				t.Fatalf("Fprint mismatch:\nin:\n%s\nwant:\n%sgot:\n%s",
					tc.in, want, got)
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
	got := strFprint(prog)

	outb, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := string(outb)
	if got != want {
		t.Fatalf("Fprint mismatch:\nwant:\n%sgot:\n%s",
			want, got)
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
				Node:   Subshell{},
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
