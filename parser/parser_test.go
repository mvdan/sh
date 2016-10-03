// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package parser

import (
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/mvdan/sh/ast"
	"github.com/mvdan/sh/internal"
	"github.com/mvdan/sh/internal/tests"

	"github.com/kr/pretty"
)

func TestParse(t *testing.T) {
	internal.DefaultPos = 0
	for i, c := range tests.FileTests {
		want := c.Ast.(*ast.File)
		tests.SetPosRecurse(t, "", want.Stmts, internal.DefaultPos, false)
		for j, in := range c.Strs {
			t.Run(fmt.Sprintf("%03d-%d", i, j), singleParse(in, want))
		}
	}
}

func confirmParse(shell, in string, fail bool) func(*testing.T) {
	return func(t *testing.T) {
		cmd := exec.Command(shell, "-n")
		cmd.Stdin = strings.NewReader(in)
		err := cmd.Run()
		if fail && err == nil {
			t.Fatalf("Expected error in `%s -n` of %q, found none", shell, in)
		} else if !fail && err != nil {
			t.Fatalf("Unexpected error in `%s -n` of %q: %v", shell, in, err)
		}
	}
}

func TestParseConfirm(t *testing.T) {
	if testing.Short() {
		t.Skip("calling bash/dash is slow.")
	}
	for i, c := range tests.FileTests {
		for j, in := range c.Strs {
			t.Run(fmt.Sprintf("noerr-%03d-%d", i, j),
				confirmParse("bash", in, false))
		}
	}
}

func singleParse(in string, want *ast.File) func(t *testing.T) {
	return func(t *testing.T) {
		got, err := Parse([]byte(in), "", 0)
		if err != nil {
			t.Fatalf("Unexpected error in %q: %v", in, err)
		}
		tests.CheckNewlines(t, in, got.Lines)
		got.Lines = nil
		tests.SetPosRecurse(t, in, got.Stmts, internal.DefaultPos, true)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("AST mismatch in %q\ndiff:\n%s", in,
				strings.Join(pretty.Diff(want, got), "\n"),
			)
		}
	}
}

func BenchmarkParse(b *testing.B) {
	type benchmark struct {
		name, in string
	}
	benchmarks := []benchmark{
		{
			"Whitespace",
			strings.Repeat("\n\n\t\t        \n", 10),
		},
		{
			"Comment",
			"# " + strings.Repeat("foo bar ", 10),
		},
		{
			"LongLit",
			strings.Repeat("really_long_lit__", 10),
		},
		{
			"Cmds",
			strings.Repeat("a b c d; ", 10),
		},
		{
			"Quoted",
			"'" + strings.Repeat("foo bar ", 10) + "\n'" +
				`"` + strings.Repeat("foo bar ", 10) + "\n\"",
		},
		{
			"NestedStmts",
			"a() { (b); { c; }; (d); }; $(a `b` $(c) `d`)",
		},
		{
			"Assign",
			"foo=bar a=b c=d abcdé=fg",
		},
		{
			"Clauses",
			"if a; then while b; do for c in d e; do f; done; done; fi",
		},
		{
			"Binary",
			"a | b && c || d | e && g || f | h",
		},
		{
			"Redirect",
			"foo >a <b <<<c 2>&1 <<EOF\n" +
				strings.Repeat("somewhat long heredoc line\n", 10) +
				"EOF",
		},
		{
			"Arithm",
			"$((a + (-b) * c)); let a++ b=(1+2)",
		},
	}
	for _, c := range benchmarks {
		b.Run(c.name, func(b *testing.B) {
			in := []byte(c.in)
			for i := 0; i < b.N; i++ {
				if _, err := Parse(in, "", ParseComments); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

var shellTests = []struct {
	in, want string
}{
	{
		"'",
		`1:1: reached EOF without closing quote '`,
	},
	{
		`"`,
		`1:1: reached EOF without closing quote "`,
	},
	{
		`'\''`,
		`1:4: reached EOF without closing quote '`,
	},
	{
		";",
		`1:1: ; can only immediately follow a statement`,
	},
	{
		"{ ; }",
		`1:3: ; can only immediately follow a statement`,
	},
	{
		`"foo"(){}`,
		`1:1: invalid func name: "\"foo\""`,
	},
	{
		`foo$bar(){}`,
		`1:1: invalid func name: "foo$bar"`,
	},
	{
		`function "foo"(){}`,
		`1:10: invalid func name: "\"foo\""`,
	},
	{
		"{",
		`1:1: reached EOF without matching word { with }`,
	},
	{
		"}",
		`1:1: } can only be used to close a block`,
	},
	{
		"{ #}",
		`1:1: reached EOF without matching word { with }`,
	},
	{
		"(",
		`1:1: reached EOF without matching token ( with )`,
	},
	{
		")",
		`1:1: ) can only be used to close a subshell`,
	},
	{
		"( )",
		`1:1: a subshell must contain at least one statement`,
	},
	{
		"`",
		"1:1: reached EOF without closing quote `",
	},
	{
		";;",
		`1:1: ;; can only be used in a case clause`,
	},
	{
		"( foo;",
		`1:1: reached EOF without matching token ( with )`,
	},
	{
		"&",
		`1:1: & can only immediately follow a statement`,
	},
	{
		"|",
		`1:1: | can only immediately follow a statement`,
	},
	{
		"&&",
		`1:1: && can only immediately follow a statement`,
	},
	{
		"||",
		`1:1: || can only immediately follow a statement`,
	},
	{
		"foo; || bar",
		`1:6: || can only immediately follow a statement`,
	},
	{
		"foo & || bar",
		`1:7: || can only immediately follow a statement`,
	},
	{
		"foo & ; bar",
		`1:7: ; can only immediately follow a statement`,
	},
	{
		"foo;;",
		`1:4: ;; can only be used in a case clause`,
	},
	{
		"foo(",
		`1:1: "foo(" must be followed by )`,
	},
	{
		"foo(bar",
		`1:1: "foo(" must be followed by )`,
	},
	{
		"à(",
		`1:1: "foo(" must be followed by )`,
	},
	{
		"function",
		`1:1: "function" must be followed by a word`,
	},
	{
		"function foo(",
		`1:10: "foo(" must be followed by )`,
	},
	{
		"function `function",
		`1:11: "function" must be followed by a word`,
	},
	{
		"foo'",
		`1:4: reached EOF without closing quote '`,
	},
	{
		`foo"`,
		`1:4: reached EOF without closing quote "`,
	},
	{
		`"foo`,
		`1:1: reached EOF without closing quote "`,
	},
	{
		`"foo\`,
		`1:1: reached EOF without closing quote "`,
	},
	{
		`"foo\a`,
		`1:1: reached EOF without closing quote "`,
	},
	{
		"foo()",
		`1:1: "foo()" must be followed by a statement`,
	},
	{
		"function foo()",
		`1:1: "foo()" must be followed by a statement`,
	},
	{
		"foo() {",
		`1:7: reached EOF without matching word { with }`,
	},
	{
		"echo foo(",
		`1:9: a command can only contain words and redirects`,
	},
	{
		"foo &&",
		`1:5: && must be followed by a statement`,
	},
	{
		"foo |",
		`1:5: | must be followed by a statement`,
	},
	{
		"foo |&",
		`1:5: |& must be followed by a statement`,
	},
	{
		"foo ||",
		`1:5: || must be followed by a statement`,
	},
	{
		"foo >",
		`1:5: > must be followed by a word`,
	},
	{
		"foo >>",
		`1:5: >> must be followed by a word`,
	},
	{
		"foo <",
		`1:5: < must be followed by a word`,
	},
	{
		"foo <\nbar",
		`2:1: redirect word must be on the same line`,
	},
	{
		"foo <>",
		`1:5: <> must be followed by a word`,
	},
	{
		"foo <<<",
		`1:5: <<< must be followed by a word`,
	},
	{
		"foo <<",
		`1:5: << must be followed by a word`,
	},
	{
		"foo <<\nEOF\nbar\nEOF",
		`2:1: heredoc stop word must be on the same line`,
	},
	{
		"if",
		`1:1: "if" must be followed by at least one statement`,
	},
	{
		"if; then bar; fi",
		`1:1: "if" must be followed by at least one statement`,
	},
	{
		"if foo;",
		`1:1: "if <cond>" must be followed by "then"`,
	},
	{
		"if foo then",
		`1:1: "if <cond>" must be followed by "then"`,
	},
	{
		"if foo; then bar;",
		`1:1: if statement must end with "fi"`,
	},
	{
		"if foo; then; fi",
		`1:9: "then" must be followed by at least one statement`,
	},
	{
		"if foo; then bar; fi#etc",
		`1:1: if statement must end with "fi"`,
	},
	{
		"if a; then b; elif c;",
		`1:15: "elif <cond>" must be followed by "then"`,
	},
	{
		"if a; then b; elif; then c; fi",
		`1:15: "elif" must be followed by at least one statement`,
	},
	{
		"if a; then b; elif c; then; fi",
		`1:23: "then" must be followed by at least one statement`,
	},
	{
		"if a; then b; else; fi",
		`1:15: "else" must be followed by at least one statement`,
	},
	{
		"'foo' '",
		`1:7: reached EOF without closing quote '`,
	},
	{
		"'foo\n' '",
		`2:3: reached EOF without closing quote '`,
	},
	{
		"while",
		`1:1: "while" must be followed by at least one statement`,
	},
	{
		"while; do bar; done",
		`1:1: "while" must be followed by at least one statement`,
	},
	{
		"while foo;",
		`1:1: "while <cond>" must be followed by "do"`,
	},
	{
		"while foo; do bar",
		`1:1: while statement must end with "done"`,
	},
	{
		"while foo; do bar;",
		`1:1: while statement must end with "done"`,
	},
	{
		"while foo; do; fi",
		`1:12: "do" must be followed by at least one statement`,
	},
	{
		"until",
		`1:1: "until" must be followed by at least one statement`,
	},
	{
		"until; do bar; done",
		`1:1: "until" must be followed by at least one statement`,
	},
	{
		"until foo;",
		`1:1: "until <cond>" must be followed by "do"`,
	},
	{
		"until foo; do bar",
		`1:1: until statement must end with "done"`,
	},
	{
		"until foo; do bar;",
		`1:1: until statement must end with "done"`,
	},
	{
		"until foo; do; fi",
		`1:12: "do" must be followed by at least one statement`,
	},
	{
		"for",
		`1:1: "for" must be followed by a literal`,
	},
	{
		"for i",
		`1:1: "for foo" must be followed by "in", ; or a newline`,
	},
	{
		"for i in;",
		`1:1: "for foo [in words]" must be followed by "do"`,
	},
	{
		"for i in 1 2 3;",
		`1:1: "for foo [in words]" must be followed by "do"`,
	},
	{
		"for i in 1 2 &",
		`1:14: word list can only contain words`,
	},
	{
		"for i in 1 2 3; do echo $i;",
		`1:1: for statement must end with "done"`,
	},
	{
		"for i in 1 2 3; echo $i;",
		`1:1: "for foo [in words]" must be followed by "do"`,
	},
	{
		"for 'i' in 1 2 3; do echo $i; done",
		`1:1: "for" must be followed by a literal`,
	},
	{
		"for in 1 2 3; do echo $i; done",
		`1:1: "for foo" must be followed by "in", ; or a newline`,
	},
	{
		"foo &\n;",
		`2:1: ; can only immediately follow a statement`,
	},
	{
		"echo $(foo",
		`1:6: reached EOF without matching token ( with )`,
	},
	{
		"echo $((foo",
		`1:6: reached EOF without matching token $(( with ))`,
	},
	{
		"echo $[foo",
		`1:6: reached EOF without matching token $[ with ]`,
	},
	{
		`echo $((\`,
		`1:6: reached EOF without matching token $(( with ))`,
	},
	{
		`echo $((foo\`,
		`1:6: reached EOF without matching token $(( with ))`,
	},
	{
		`echo $((foo\a`,
		`1:6: reached EOF without matching token $(( with ))`,
	},
	{
		"echo $((()))",
		`1:9: parentheses must enclose an expression`,
	},
	{
		"echo $(((3))",
		`1:6: reached ) without matching token $(( with ))`,
	},
	{
		"echo $((+))",
		`1:9: + must be followed by an expression`,
	},
	{
		"echo $((a b c))",
		`1:11: not a valid arithmetic operator: b`,
	},
	{
		"echo $((a *))",
		`1:11: * must be followed by an expression`,
	},
	{
		"echo $((++))",
		`1:9: ++ must be followed by a word`,
	},
	{
		"<<EOF\n$(()a",
		`2:1: $(( must be followed by a word`,
	},
	{
		"echo ${foo",
		`1:6: reached EOF without matching token ${ with }`,
	},
	{
		"echo $'",
		`1:6: reached EOF without closing quote '`,
	},
	{
		`echo $"`,
		`1:6: reached EOF without closing quote "`,
	},
	{
		"echo $foo ${}",
		`1:11: parameter expansion requires a literal`,
	},
	{
		"echo ${foo-bar",
		`1:6: reached EOF without matching token ${ with }`,
	},
	{
		"echo ${#foo-bar}",
		`1:12: can only get length of a simple parameter`,
	},
	{
		"#foo\n{",
		`2:1: reached EOF without matching word { with }`,
	},
	{
		`echo "foo${bar"`,
		`1:10: reached EOF without matching token ${ with }`,
	},
	{
		"foo\n;",
		`2:1: ; can only immediately follow a statement`,
	},
	{
		"(foo) bar",
		`1:7: statements must be separated by &, ; or a newline`,
	},
	{
		"{ foo; } bar",
		`1:10: statements must be separated by &, ; or a newline`,
	},
	{
		"if foo; then bar; fi bar",
		`1:22: statements must be separated by &, ; or a newline`,
	},
	{
		"case",
		`1:1: "case" must be followed by a word`,
	},
	{
		"case i",
		`1:1: "case x" must be followed by "in"`,
	},
	{
		"case i in 3) foo;",
		`1:1: case statement must end with "esac"`,
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
		"case $i in &) foo;",
		`1:12: case patterns must consist of words`,
	},
	{
		"\"`\"",
		`1:3: reached EOF without closing quote "`,
	},
	{
		"`\"`",
		"1:3: reached EOF without closing quote `",
	},
	{
		"`{\n`",
		"1:2: reached ` without matching word { with }",
	},
	{
		"echo \"`)`\"",
		`1:8: ) can only be used to close a subshell`,
	},
	{
		"[[",
		`1:1: test clause requires at least one expression`,
	},
	{
		"[[ a",
		`1:1: reached EOF without matching word [[ with ]]`,
	},
	{
		"[[ -f a",
		`1:1: reached EOF without matching word [[ with ]]`,
	},
	{
		"[[ a == b",
		`1:1: reached EOF without matching word [[ with ]]`,
	},
	{
		"[[ a =~ b",
		`1:1: reached EOF without matching word [[ with ]]`,
	},
	{
		"[[ a b c ]]",
		`1:6: not a valid test operator: b`,
	},
	{
		"[[ a & b ]]",
		`1:6: not a valid test operator: &`,
	},
	{
		"[[ true && () ]]",
		`1:12: parentheses must enclose an expression`,
	},
	{
		"declare (",
		`1:9: "declare" must be followed by words`,
	},
	{
		"let",
		`1:1: let clause requires at least one expression`,
	},
	{
		"let a+ b",
		`1:6: + must be followed by an expression`,
	},
	{
		"let + a",
		`1:5: + must be followed by an expression`,
	},
	{
		"let a ++",
		`1:7: ++ must be followed by a word`,
	},
	{
		"let ))",
		`1:5: "let" must be followed by arithmetic expressions`,
	},
	{
		"a=(<)",
		`1:4: array elements must be words`,
	},
	{
		"foo <<$(bar)",
		`1:9: nested statements not allowed in this word`,
	},
	{
		`""()`,
		`1:1: invalid func name: "\"\""`,
	},
}

func checkError(in, want string, mode Mode) func(*testing.T) {
	return func(t *testing.T) {
		_, err := Parse([]byte(in), "", mode)
		if err == nil {
			t.Fatalf("Expected error in %q: %v", in, want)
		}
		if got := err.Error(); got != want {
			t.Fatalf("Error mismatch in %q\nwant: %s\ngot:  %s",
				in, want, got)
		}
	}
}

func TestParseErrPosix(t *testing.T) {
	for i, c := range shellTests {
		t.Run(fmt.Sprintf("%03d", i), checkError(c.in, c.want, PosixConformant))
	}
	for i, c := range posixTests {
		t.Run(fmt.Sprintf("%03d", len(shellTests)+i),
			checkError(c.in, c.want, PosixConformant))
	}
}

func TestParseErrBash(t *testing.T) {
	for i, c := range shellTests {
		t.Run(fmt.Sprintf("%03d", i), checkError(c.in, c.want, 0))
	}
	for i, c := range bashTests {
		t.Run(fmt.Sprintf("%03d", len(shellTests)+i),
			checkError(c.in, c.want, 0))
	}
}

var bashTests = []struct {
	in, want string
}{
	{
		"((foo",
		`1:1: reached EOF without matching token (( with ))`,
	},
	{
		"echo ((foo",
		`1:6: a command can only contain words and redirects`,
	},
}

var posixTests = []struct {
	in, want string
}{
	{
		"((foo",
		`1:2: reached EOF without matching token ( with )`,
	},
	{
		"echo ((foo",
		`1:1: "foo(" must be followed by )`,
	},
}

func TestInputName(t *testing.T) {
	in := shellTests[0].in
	want := "some-file.sh:" + shellTests[0].want
	_, err := Parse([]byte(in), "some-file.sh", 0)
	if err == nil {
		t.Fatalf("Expected error in %q: %v", in, want)
	}
	got := err.Error()
	if got != want {
		t.Fatalf("Error mismatch in %q\nwant: %s\ngot:  %s",
			in, want, got)
	}
}
