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
			want: `1:1: unexpected token EOF, wanted '\''`,
		},
		{
			in:   "\"",
			want: `1:1: unexpected token EOF, wanted '"'`,
		},
		{
			in:   "'\\''",
			want: `1:5: unexpected token EOF, wanted '\''`,
		},
		{
			in:   ";",
			want: `1:1: unexpected token ';', wanted command`,
		},
		{
			in:   "foà=3bar",
			want: `1:1: invalid var name "foà"`,
		},
		{
			in:   "à(){}",
			want: `1:1: invalid func name "à"`,
		},
		{
			in:   "{",
			want: `1:2: unexpected token EOF, wanted '}'`,
		},
		{
			in:   "{\n",
			want: `2:1: unexpected token EOF, wanted '}'`,
		},
		{
			in:   "{{}",
			want: `1:4: unexpected token EOF after block`,
		},
		{
			in:   "}",
			want: `1:1: unexpected token '}', wanted command`,
		},
		{
			in:   "{}}",
			want: `1:3: unexpected token '}' after block`,
		},
		{
			in:   "{#}",
			want: `1:4: unexpected token EOF, wanted '}'`,
		},
		{
			in:   "{}(){}",
			want: `1:3: unexpected token '(' after block`,
		},
		{
			in:   "(",
			want: `1:2: unexpected token EOF, wanted command`,
		},
		{
			in:   ")",
			want: `1:1: unexpected token ')', wanted command`,
		},
		{
			in:   "()",
			want: `1:2: unexpected token ')', wanted command`,
		},
		{
			in:   "( foo;",
			want: `1:7: unexpected token EOF, wanted ')'`,
		},
		{
			in:   "=",
			want: `1:1: unexpected token '=', wanted command`,
		},
		{
			in:   "&",
			want: `1:1: unexpected token '&', wanted command`,
		},
		{
			in:   "|",
			want: `1:1: unexpected token '|', wanted command`,
		},
		{
			in:   "foo;;",
			want: `1:5: unexpected token ';', wanted command`,
		},
		{
			in:   "foo(",
			want: `1:5: unexpected token EOF, wanted ')'`,
		},
		{
			in:   "foo'",
			want: `1:4: unexpected token EOF, wanted '\''`,
		},
		{
			in:   "foo\"",
			want: `1:4: unexpected token EOF, wanted '"'`,
		},
		{
			in:   "foo()",
			want: `1:6: unexpected token EOF, wanted command`,
		},
		{
			in:   "foo() {",
			want: `1:8: unexpected token EOF, wanted '}'`,
		},
		{
			in:   "foo &&",
			want: `1:7: unexpected token EOF, wanted command`,
		},
		{
			in:   "foo |",
			want: `1:6: unexpected token EOF, wanted command`,
		},
		{
			in:   "foo ||",
			want: `1:7: unexpected token EOF, wanted command`,
		},
		{
			in:   "foo >",
			want: `1:6: unexpected token EOF, wanted word`,
		},
		{
			in:   "foo >>",
			want: `1:7: unexpected token EOF, wanted word`,
		},
		{
			in:   "foo >&",
			want: `1:7: unexpected token EOF, wanted word`,
		},
		{
			in:   "foo >&bar",
			want: `1:8: invalid fd "bar"`,
		},
		{
			in:   "foo <",
			want: `1:6: unexpected token EOF, wanted word`,
		},
		{
			in:   "if",
			want: `1:3: unexpected token EOF, wanted command`,
		},
		{
			in:   "if foo;",
			want: `1:8: unexpected token EOF, wanted "then"`,
		},
		{
			in:   "if foo; bar",
			want: `1:11: unexpected token word, wanted "then"`,
		},
		{
			in:   "if foo; then bar;",
			want: `1:18: unexpected token EOF, wanted "fi"`,
		},
		{
			in:   "'foo' '",
			want: `1:8: unexpected token EOF, wanted '\''`,
		},
		{
			in:   "'foo\n' '",
			want: `2:3: unexpected token EOF, wanted '\''`,
		},
		{
			in:   "while",
			want: `1:6: unexpected token EOF, wanted command`,
		},
		{
			in:   "while foo;",
			want: `1:11: unexpected token EOF, wanted "do"`,
		},
		{
			in:   "while foo; bar",
			want: `1:14: unexpected token word, wanted "do"`,
		},
		{
			in:   "while foo; do bar;",
			want: `1:19: unexpected token EOF, wanted "done"`,
		},
	}
	for _, c := range errs {
		r := strings.NewReader(c.in)
		err := parse(r, "")
		if err == nil {
			t.Fatalf("Expected error in: %q", c.in)
		}
		got := err.Error()[1:]
		if got != c.want {
			t.Fatalf("Error mismatch in %q\nwant: %s\ngot:  %s", c.in, c.want, got)
		}
	}
}
