// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/kr/pretty"
)

func TestParse(t *testing.T) {
	defaultPos = 0
	for i, c := range astTests {
		want := c.ast.(*File)
		setPosRecurse(t, "", want.Stmts, defaultPos, false)
		for j, in := range c.strs {
			t.Run(fmt.Sprintf("%03d-%d", i, j), singleParse(in, want))
		}
	}
}

func checkNewlines(tb testing.TB, src string, got []int) {
	want := []int{0}
	for i, b := range src {
		if b == '\n' {
			want = append(want, i+1)
		}
	}
	if !reflect.DeepEqual(got, want) {
		tb.Fatalf("Unexpected newline offsets at %q:\ngot:  %v\nwant: %v",
			src, got, want)
	}
}

func singleParse(in string, want *File) func(t *testing.T) {
	return func(t *testing.T) {
		got, err := Parse(in, "", 0)
		if err != nil {
			t.Fatalf("Unexpected error in %q: %v", in, err)
		}
		checkNewlines(t, in, got.lines)
		got.lines = nil
		setPosRecurse(t, in, got.Stmts, defaultPos, true)
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

var errBadReader = fmt.Errorf("read: expected error")

type badReader struct{}

func (b badReader) Read(p []byte) (int, error) { return 0, errBadReader }

func TestReadErr(t *testing.T) {
	var in badReader
	_, err := Parse(in, "", 0)
	if err == nil {
		t.Fatalf("Expected error with bad reader")
	}
	if err != errBadReader {
		t.Fatalf("Error mismatch with bad reader:\nwant: %v\ngot:  %v",
			errBadReader, err)
	}
}

var errTests = []struct {
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
		`1:1: invalid func name: "foo"`,
	},
	{
		`foo$bar(){}`,
		`1:1: invalid func name: foo$bar`,
	},
	{
		`function "foo"(){}`,
		`1:10: invalid func name: "foo"`,
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
		`1:1: ;; is not a valid start for a statement`,
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
		"foo;;",
		`1:4: a command can only contain words and redirects`,
	},
	{
		"foo(",
		`1:1: "foo(" must be followed by ")"`,
	},
	{
		"foo(bar",
		`1:1: "foo(" must be followed by ")"`,
	},
	{
		"à(",
		`1:1: "foo(" must be followed by ")"`,
	},
	{
		"function",
		`1:1: "function" must be followed by a word`,
	},
	{
		"function foo(",
		`1:10: "foo(" must be followed by ")"`,
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
		"foo <>",
		`1:5: <> must be followed by a word`,
	},
	{
		"foo <<<",
		`1:5: <<< must be followed by a word`,
	},
	{
		"if",
		`1:1: "if" must be followed by a statement list`,
	},
	{
		"if foo;",
		`1:1: "if [stmts]" must be followed by "then"`,
	},
	{
		"if foo then",
		`1:1: "if [stmts]" must be followed by "then"`,
	},
	{
		"if foo; then bar;",
		`1:1: if statement must end with "fi"`,
	},
	{
		"if foo; then bar; fi#etc",
		`1:1: if statement must end with "fi"`,
	},
	{
		"if a; then b; elif c;",
		`1:15: "elif [stmts]" must be followed by "then"`,
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
		`1:1: "while" must be followed by a statement list`,
	},
	{
		"while foo;",
		`1:1: "while [stmts]" must be followed by "do"`,
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
		"until",
		`1:1: "until" must be followed by a statement list`,
	},
	{
		"until foo;",
		`1:1: "until [stmts]" must be followed by "do"`,
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
		`1:6: reached EOF without matching token (( with ))`,
	},
	{
		`echo $((\`,
		`1:6: reached EOF without matching token (( with ))`,
	},
	{
		`echo $((foo\`,
		`1:6: reached EOF without matching token (( with ))`,
	},
	{
		`echo $((foo\a`,
		`1:6: reached EOF without matching token (( with ))`,
	},
	{
		"echo $((()))",
		`1:9: parentheses must enclose an expression`,
	},
	{
		"echo $(((3))",
		`1:6: reached ) without matching token (( with ))`,
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
}

func TestParseErr(t *testing.T) {
	for i, c := range errTests {
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			_, err := Parse(c.in, "", 0)
			if err == nil {
				t.Fatalf("Expected error in %q: %v", c.in, c.want)
			}
			got := err.Error()
			if got != c.want {
				t.Fatalf("Error mismatch in %q\nwant: %s\ngot:  %s",
					c.in, c.want, got)
			}
		})
	}
}

func TestInputName(t *testing.T) {
	in := errTests[0].in
	want := "some-file.sh:" + errTests[0].want
	_, err := Parse(in, "some-file.sh", 0)
	if err == nil {
		t.Fatalf("Expected error in %q: %v", in, want)
	}
	got := err.Error()
	if got != want {
		t.Fatalf("Error mismatch in %q\nwant: %s\ngot:  %s",
			in, want, got)
	}
}
