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

func strFprint(n Node) string {
	var buf bytes.Buffer
	Fprint(&buf, n)
	return buf.String()
}

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
			got = got[:len(got)-1]
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
		{"foo\n\n\nbar", "foo\n\nbar"},
		{"foo\n\n", "foo"},
		{"\n\nfoo", "foo"},
		{"a=b # inline\nbar", "a=b # inline\nbar"},
		{"a=`b` # inline", "a=`b` # inline"},
		{"`a` `b`", "`a` `b`"},
		{"if a\nthen\n\tb\nfi", "if a; then\n\tb\nfi"},
		{
			"{\nbar\n# extra\n}",
			"{\n\tbar\n\t# extra\n}",
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
				t.Fatalf("Fprint mismatch:\nwant:\n%sgot:\n%s",
					want, got)
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
