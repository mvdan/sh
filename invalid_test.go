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
			`1:2: reached EOF without closing quote '`,
		},
		{
			`"`,
			`1:2: reached EOF without closing quote "`,
		},
		{
			`'\''`,
			`1:5: reached EOF without closing quote '`,
		},
		{
			";",
			`1:1: ; is not a valid start for a statement`,
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
			`'foo'(){}`,
			`1:1: invalid func name: 'foo'`,
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
			`1:1: } is not a valid start for a statement`,
		},
		{
			"{#}",
			`1:4: reached EOF without matching token { with }`,
		},
		{
			"(",
			`1:2: a subshell must contain one or more statements`,
		},
		{
			")",
			`1:1: ) is not a valid start for a statement`,
		},
		{
			"()",
			`1:2: a subshell must contain one or more statements`,
		},
		{
			"( #foo\n)",
			`2:1: a subshell must contain one or more statements`,
		},
		{
			"( foo;",
			`1:7: reached EOF without matching token ( with )`,
		},
		{
			"&",
			`1:1: & is not a valid start for a statement`,
		},
		{
			"|",
			`1:1: | is not a valid start for a statement`,
		},
		{
			"foo;;",
			`1:4: a command can only contain words and redirects`,
		},
		{
			"foo(",
			`1:5: functions must start like "foo()"`,
		},
		{
			"foo(bar",
			`1:5: functions must start like "foo()"`,
		},
		{
			"à(",
			`1:3: functions must start like "foo()"`,
		},
		{
			"foo'",
			`1:5: reached EOF without closing quote '`,
		},
		{
			`foo"`,
			`1:5: reached EOF without closing quote "`,
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
			`1:9: a command can only contain words and redirects`,
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
			`1:8: reached EOF without closing quote '`,
		},
		{
			"'foo\n' '",
			`2:4: reached EOF without closing quote '`,
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
			`2:1: ; is not a valid start for a statement`,
		},
		{
			"echo $(foo",
			`1:11: reached EOF without matching token ( with )`,
		},
		{
			"echo $((foo",
			`1:12: reached EOF without matching token (( with ))`,
		},
		{
			"echo ${foo",
			`1:11: reached EOF without matching token { with }`,
		},
		{
			"#foo\n{",
			`2:2: a block must contain one or more statements`,
		},
		{
			`echo "foo${bar"`,
			`1:16: reached EOF without matching token { with }`,
		},
		{
			"foo\n;",
			`2:1: ; is not a valid start for a statement`,
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
			`1:7: "case x" must be followed by "in"`,
		},
		{
			"case i in",
			`1:10: "case x in" must be followed by one or more patterns`,
		},
		{
			"case i in 3) foo;",
			`1:18: case statement must end with "esac"`,
		},
		{
			"case i in 3) foo; 4) bar; esac",
			`1:20: a command can only contain words and redirects`,
		},
		{
			"case i in 3&) foo;",
			`1:12: case patterns must be separated with |`,
		},
		{
			"case i in &) foo;",
			`1:11: case patterns must consist of words`,
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
