// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	paths, err := filepath.Glob("testdata/*.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		testParse(t, path)
	}
}

func testParse(t *testing.T, path string) {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := parse(f, path); err != nil {
		t.Fatalf("Parse error: %v", err)
	}
}

func TestParseErr(t *testing.T) {
	errs := []struct {
		in, want string
	}{
		{
			in:   "'",
			want: "1:1: unexpected token EOF, wanted '\\''",
		},
		{
			in:   "\"",
			want: "1:1: unexpected token EOF, wanted '\"'",
		},
		{
			in:   "'\\''",
			want: "1:2: unexpected token string, wanted '\\''",
		},
		{
			in:   ";",
			want: "1:1: unexpected token ';'",
		},
		{
			in:   "à=3",
			want: "1:3: invalid var name: à",
		},
		{
			in:   "à(){}",
			want: "1:3: invalid func name: à",
		},
		{
			in:   "{",
			want: "1:1: unexpected token EOF",
		},
		{
			in:   "{{}",
			want: "1:3: unexpected token EOF",
		},
		{
			in:   "}",
			want: "1:1: unexpected token '}'",
		},
		{
			in:   "{}}",
			want: "1:3: unexpected token '}'",
		},
		{
			in:   "{#}",
			want: "1:2: unexpected token EOF",
		},
		{
			in:   "{}(){}",
			want: "1:3: unexpected token '('",
		},
		{
			in:   "=",
			want: "1:1: unexpected token '='",
		},
		{
			in:   "&",
			want: "1:1: unexpected token '&'",
		},
		{
			in:   "|",
			want: "1:1: unexpected token '|'",
		},
		{
			in:   "foo;;",
			want: "1:5: unexpected token ';'",
		},
		{
			in:   "foo(",
			want: "1:4: unexpected token EOF",
		},
		{
			in:   "foo'",
			want: "1:4: unexpected token string, wanted '\\''",
		},
		{
			in:   "foo\"",
			want: "1:4: unexpected token string, wanted '\"'",
		},
		{
			in:   "foo()",
			want: "1:5: unexpected token EOF",
		},
		{
			in:   "foo() {",
			want: "1:7: unexpected token EOF",
		},
		{
			in:   "foo &&",
			want: "1:6: unexpected token EOF",
		},
		{
			in:   "foo |",
			want: "1:5: unexpected token EOF",
		},
		{
			in:   "foo ||",
			want: "1:6: unexpected token EOF",
		},
		{
			in:   "foo >",
			want: "1:5: unexpected token EOF, wanted string",
		},
		{
			in:   "foo >>",
			want: "1:6: unexpected token EOF, wanted string",
		},
		{
			in:   "foo >&",
			want: "1:6: unexpected token EOF, wanted string",
		},
		{
			in:   "foo <",
			want: "1:5: unexpected token EOF, wanted string",
		},
	}
	for _, c := range errs {
		r := strings.NewReader(c.in)
		err := parse(r, "")
		if err == nil {
			t.Fatalf("Expected error in: %q", c.in)
		}
		got := err.Error()
		if got != c.want {
			t.Fatalf("Error mismatch in %q\nwant: %q\ngot:  %q", c.in, c.want, got)
		}
	}
}
