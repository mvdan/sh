// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"strings"
	"testing"
)

func TestParseErr(t *testing.T) {
	errs := []struct {
		in, want string
	}{
		{
			"'",
			`1:2: unexpected token EOF - wanted '`,
		},
		{
			`"`,
			`1:2: unexpected token EOF - wanted "`,
		},
		{
			`'\''`,
			`1:5: unexpected token EOF - wanted '`,
		},
		{
			";",
			`1:1: unexpected token ; - wanted command`,
		},
		{
			"à(){}",
			`1:1: invalid func name: à`,
		},
		{
			`"foo"(){}`,
			`1:1: invalid func name: "foo"`,
		},
		{
			"{",
			`1:2: a block must contain one or more statements`,
		},
		{
			"{}",
			`1:2: a block must contain one or more statements`,
		},
		{
			"}",
			`1:1: unexpected token } - wanted command`,
		},
		{
			"{#}",
			`1:4: unexpected token EOF - wanted }`,
		},
		{
			"(",
			`1:2: a subshell must contain one or more statements`,
		},
		{
			")",
			`1:1: unexpected token ) - wanted command`,
		},
		{
			"()",
			`1:2: a subshell must contain one or more statements`,
		},
		{
			"( foo;",
			`1:7: unexpected token EOF - wanted )`,
		},
		{
			"( #foo\n)",
			`2:1: unexpected token ) - wanted command`,
		},
		{
			"&",
			`1:1: unexpected token & - wanted command`,
		},
		{
			"|",
			`1:1: unexpected token | - wanted command`,
		},
		{
			"foo;;",
			`1:4: unexpected token ;; after command`,
		},
		{
			"foo(",
			`1:5: unexpected token EOF - wanted )`,
		},
		{
			"à(",
			`1:3: unexpected token EOF - wanted )`,
		},
		{
			"foo'",
			`1:5: unexpected token EOF - wanted '`,
		},
		{
			`foo"`,
			`1:5: unexpected token EOF - wanted "`,
		},
		{
			"foo()",
			`1:6: "foo()" must be followed by a statement`,
		},
		{
			"foo() {",
			`1:8: a block must contain one or more statements`,
		},
		{
			"echo foo(",
			`1:9: unexpected token ( after command`,
		},
		{
			"foo &&",
			`1:7: && must be followed by a statement`,
		},
		{
			"foo |",
			`1:6: | must be followed by a statement`,
		},
		{
			"foo ||",
			`1:7: || must be followed by a statement`,
		},
		{
			"foo >",
			`1:6: > must be followed by a word`,
		},
		{
			"foo >>",
			`1:7: >> must be followed by a word`,
		},
		{
			"foo <",
			`1:6: < must be followed by a word`,
		},
		{
			"if",
			`1:3: "if" must be followed by a statement`,
		},
		{
			"if foo;",
			`1:8: "if x" must be followed by "then"`,
		},
		{
			"if foo; bar",
			`1:9: "if x" must be followed by "then"`,
		},
		{
			"if foo; then bar;",
			`1:18: if statement must end with "fi"`,
		},
		{
			"if a; then b; elif; then c; fi",
			`1:19: "elif" must be followed by a statement`,
		},
		{
			"if a; then b; elif c;",
			`1:22: "elif x" must be followed by "then"`,
		},
		{
			"'foo' '",
			`1:8: unexpected token EOF - wanted '`,
		},
		{
			"'foo\n' '",
			`2:4: unexpected token EOF - wanted '`,
		},
		{
			"while",
			`1:6: "while" must be followed by a statement`,
		},
		{
			"while foo;",
			`1:11: "while x" must be followed by "do"`,
		},
		{
			"while foo; bar",
			`1:12: "while x" must be followed by "do"`,
		},
		{
			"while foo; do bar",
			`1:18: while statement must end with "done"`,
		},
		{
			"while foo; do bar;",
			`1:19: while statement must end with "done"`,
		},
		{
			"for",
			`1:4: "for" must be followed by a literal`,
		},
		{
			"for i",
			`1:6: "for foo" must be followed by "in"`,
		},
		{
			"for i in;",
			`1:10: "for foo in list" must be followed by "do"`,
		},
		{
			"for i in 1 2 3;",
			`1:16: "for foo in list" must be followed by "do"`,
		},
		{
			"for i in 1 2 3; do echo $i;",
			`1:28: for statement must end with "done"`,
		},
		{
			"for i in 1 2 3; echo $i;",
			`1:17: "for foo in list" must be followed by "do"`,
		},
		{
			"for in 1 2 3; do echo $i; done",
			`1:8: "for foo" must be followed by "in"`,
		},
		{
			"foo &\n;",
			`2:1: unexpected token ; - wanted command`,
		},
		{
			"echo $(foo",
			`1:11: unexpected token EOF - wanted )`,
		},
		{
			"echo $((foo",
			`1:12: unexpected token EOF - wanted ))`,
		},
		{
			"echo ${foo",
			`1:11: unexpected token EOF - wanted }`,
		},
		{
			"#foo\n{",
			`2:2: a block must contain one or more statements`,
		},
		{
			`echo "foo${bar"`,
			`1:16: unexpected token EOF - wanted }`,
		},
		{
			"foo\n;",
			`2:1: unexpected token ; - wanted command`,
		},
		{
			"(foo) bar",
			`1:7: statements must be separated by ; or a newline`,
		},
		{
			"{foo;} bar",
			`1:8: statements must be separated by ; or a newline`,
		},
		{
			"if foo; then bar; fi bar",
			`1:22: statements must be separated by ; or a newline`,
		},
		{
			"case",
			`1:5: "case" must be followed by a word`,
		},
		{
			"case i",
			`1:7: unexpected token EOF - wanted in`,
		},
		{
			"case i in",
			`1:10: "case x in" must be followed by one or more patterns`,
		},
		{
			"case i in 3) foo;",
			`1:18: unexpected token EOF - wanted esac`,
		},
		{
			"case i in 3) foo; 4) bar; esac",
			`1:20: unexpected token ) after command`,
		},
	}
	for _, c := range errs {
		r := strings.NewReader(c.in)
		_, err := Parse(r, "")
		if err == nil {
			t.Fatalf("Expected error in %q", c.in)
		}
		got := err.Error()[1:]
		if got != c.want {
			t.Fatalf("Error mismatch in %q\nwant: %s\ngot:  %s",
				c.in, c.want, got)
		}
	}
}
