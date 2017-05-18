// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/kr/pretty"
)

func TestKeepComments(t *testing.T) {
	in := "# foo\ncmd\n# bar"
	want := &File{
		Comments: []*Comment{
			{Text: " foo"},
			{Text: " bar"},
		},
		Stmts: litStmts("cmd"),
	}
	singleParse(NewParser(KeepComments), in, want)(t)
}

func TestParseBash(t *testing.T) {
	t.Parallel()
	p := NewParser()
	for i, c := range append(fileTests, fileTestsNoPrint...) {
		want := c.Bash
		if want == nil {
			continue
		}
		for j, in := range c.Strs {
			t.Run(fmt.Sprintf("%03d-%d", i, j), singleParse(p, in, want))
		}
	}
}

func TestParsePosix(t *testing.T) {
	t.Parallel()
	p := NewParser(Variant(LangPOSIX))
	for i, c := range append(fileTests, fileTestsNoPrint...) {
		want := c.Posix
		if want == nil {
			continue
		}
		for j, in := range c.Strs {
			t.Run(fmt.Sprintf("%03d-%d", i, j),
				singleParse(p, in, want))
		}
	}
}

var hasBash44 bool

func TestMain(m *testing.M) {
	os.Setenv("LANGUAGE", "en_US.UTF8")
	os.Setenv("LC_ALL", "en_US.UTF8")
	hasBash44 = checkBash()
	os.Exit(m.Run())
}

func checkBash() bool {
	out, err := exec.Command("bash", "-c", "echo -n $BASH_VERSION").Output()
	if err != nil {
		return false
	}
	return strings.HasPrefix(string(out), "4.4")
}

var extGlobRe = regexp.MustCompile(`[@?*+!]\(`)

func confirmParse(in string, posix, fail bool) func(*testing.T) {
	return func(t *testing.T) {
		t.Parallel()
		var opts []string
		if posix {
			opts = append(opts, "--posix")
		}
		if i := strings.Index(in, " #INVBASH"); i >= 0 {
			fail = !fail
			in = in[:i]
		}
		if extGlobRe.MatchString(in) {
			// otherwise bash refuses to parse these
			// properly. Also avoid -n since that too makes
			// bash bail.
			in = "shopt -s extglob\n" + in
		} else if !fail {
			// -n makes bash accept invalid inputs like
			// "let" or "`{`", so only use it in
			// non-erroring tests. Should be safe to not use
			// -n anyway since these are supposed to just
			// fail.
			// also, -n will break if we are using extglob
			// as extglob is not actually applied.
			opts = append(opts, "-n")
		}
		cmd := exec.Command("bash", opts...)
		cmd.Stdin = strings.NewReader(in)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		err := cmd.Run()
		if stderr.Len() > 0 {
			// bash sometimes likes to error on an input via stderr
			// while forgetting to set the exit code to non-zero.
			// Fun.
			if s := stderr.String(); !strings.Contains(s, ": warning: ") {
				err = errors.New(s)
			}
		}
		if err != nil && strings.Contains(err.Error(), "command not found") {
			err = nil
		}
		if fail && err == nil {
			t.Fatalf("Expected error in `%s` of %q, found none", strings.Join(cmd.Args, " "), in)
		} else if !fail && err != nil {
			t.Fatalf("Unexpected error in `%s` of %q: %v", strings.Join(cmd.Args, " "), in, err)
		}
	}
}

func TestParseBashConfirm(t *testing.T) {
	if testing.Short() {
		t.Skip("calling bash is slow.")
	}
	if !hasBash44 {
		t.Skip("bash 4.4 required to run")
	}
	for i, c := range append(fileTests, fileTestsNoPrint...) {
		for j, in := range c.Strs {
			t.Run(fmt.Sprintf("%03d-%d", i, j),
				confirmParse(in, false, false))
		}
	}
}

func TestParseErrBashConfirm(t *testing.T) {
	if testing.Short() {
		t.Skip("calling bash is slow.")
	}
	if !hasBash44 {
		t.Skip("bash 4.4 required to run")
	}
	i := 0
	for _, c := range shellTests {
		if c.bash == nil && c.common == nil {
			continue
		}
		t.Run(fmt.Sprintf("%03d", i), confirmParse(c.in, false, true))
		i++
	}
}

func TestParseErrPosixConfirm(t *testing.T) {
	if testing.Short() {
		t.Skip("calling bash is slow.")
	}
	if !hasBash44 {
		t.Skip("bash 4.4 required to run")
	}
	i := 0
	for _, c := range shellTests {
		if c.posix == nil && c.common == nil {
			continue
		}
		t.Run(fmt.Sprintf("%03d", i), confirmParse(c.in, true, true))
		i++
	}
}

func singleParse(p *Parser, in string, want *File) func(t *testing.T) {
	return func(t *testing.T) {
		if i := strings.Index(in, " #INVBASH"); i >= 0 {
			in = in[:i]
		}
		got, err := p.Parse(newStrictReader(in), "")
		if err != nil {
			t.Fatalf("Unexpected error in %q: %v", in, err)
		}
		checkNewlines(t, in, got.lines)
		got.lines = nil
		clearPosRecurse(t, in, got)
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
			"LongStrs",
			strings.Repeat("\n\n\t\t        \n", 10) +
				"# " + strings.Repeat("foo bar ", 10) + "\n" +
				strings.Repeat("longlit_", 10) + "\n" +
				"'" + strings.Repeat("foo bar ", 20) + "'\n" +
				`"` + strings.Repeat("foo bar ", 20) + `"`,
		},
		{
			"Cmds+Nested",
			strings.Repeat("a b c d; ", 8) +
				"a() { (b); { c; }; }; $(d; `e`)",
		},
		{
			"Vars+Clauses",
			"foo=bar; a=b; c=d$foo${bar}e $simple ${complex:-default}; " +
				"if a; then while b; do for c in d e; do f; done; done; fi",
		},
		{
			"Binary+Redirs",
			"a | b && c || d | e && g || f | h; " +
				"foo >a <b <<<c 2>&1 <<EOF\n" +
				strings.Repeat("somewhat long heredoc line\n", 10) +
				"EOF",
		},
	}
	for _, c := range benchmarks {
		p := NewParser(KeepComments)
		in := strings.NewReader(c.in)
		b.Run(c.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := p.Parse(in, ""); err != nil {
					b.Fatal(err)
				}
				in.Reset(c.in)
			}
		})
	}
}

type errorCase struct {
	in                  string
	common, bash, posix interface{}
}

var shellTests = []errorCase{
	{
		in:     "echo \x80 #INVBASH bash uses bytes",
		common: `1:6: invalid UTF-8 encoding`,
	},
	{
		in:     "\necho \x80 #INVBASH bash uses bytes",
		common: `2:6: invalid UTF-8 encoding`,
	},
	{
		in:     "echo foo\x80bar #INVBASH bash uses bytes",
		common: `1:9: invalid UTF-8 encoding`,
	},
	{
		in:     "echo foo\xc3 #INVBASH bash uses bytes",
		common: `1:9: invalid UTF-8 encoding`,
	},
	{
		in:     "#foo\xc3 #INVBASH bash uses bytes",
		common: `1:5: invalid UTF-8 encoding`,
	},
	{
		in:     `$ #INVBASH`,
		common: `1:1: $ must be escaped or followed by a literal`,
	},
	{
		in:     `$ # #INVBASH`,
		common: `1:1: $ must be escaped or followed by a literal`,
	},
	{
		in:     `foo$ #INVBASH`,
		common: `1:4: $ must be escaped or followed by a literal`,
	},
	{
		in:     `"$" #INVBASH`,
		common: `1:2: $ must be escaped or followed by a literal`,
	},
	{
		in:     `($) #INVBASH`,
		common: `1:2: $ must be escaped or followed by a literal`,
	},
	{
		in:     `$(foo$) #INVBASH`,
		common: `1:6: $ must be escaped or followed by a literal`,
	},
	{
		in:     "echo $((foo\x80bar",
		common: `1:12: invalid UTF-8 encoding`,
	},
	{
		in:     "((foo\x80bar",
		common: `1:6: invalid UTF-8 encoding`,
	},
	{
		in:     ";\x80",
		common: `1:2: invalid UTF-8 encoding`,
	},
	{
		in:     "echo a\x80 #INVBASH bash uses bytes",
		common: `1:7: invalid UTF-8 encoding`,
	},
	{
		in:     "${a\x80",
		common: `1:4: invalid UTF-8 encoding`,
	},
	{
		in:     "${a#\x80",
		common: `1:5: invalid UTF-8 encoding`,
	},
	{
		in:     "echo $((a |\x80",
		common: `1:12: invalid UTF-8 encoding`,
	},
	{
		in:     "<<$\xc8 #INVBASH bash uses bytes",
		common: `1:4: invalid UTF-8 encoding`,
	},
	{
		in:     "}",
		common: `1:1: } can only be used to close a block`,
	},
	{
		in:     "then",
		common: `1:1: "then" can only be used in an if`,
	},
	{
		in:     "elif",
		common: `1:1: "elif" can only be used in an if`,
	},
	{
		in:     "fi",
		common: `1:1: "fi" can only be used to end an if`,
	},
	{
		in:     "do",
		common: `1:1: "do" can only be used in a loop`,
	},
	{
		in:     "done",
		common: `1:1: "done" can only be used to end a loop`,
	},
	{
		in:     "esac",
		common: `1:1: "esac" can only be used to end a case`,
	},
	{
		in:     "'",
		common: `1:1: reached EOF without closing quote '`,
	},
	{
		in:     `"`,
		common: `1:1: reached EOF without closing quote "`,
	},
	{
		in:     `'\''`,
		common: `1:4: reached EOF without closing quote '`,
	},
	{
		in:     ";",
		common: `1:1: ; can only immediately follow a statement`,
	},
	{
		in:     "{ ; }",
		common: `1:3: ; can only immediately follow a statement`,
	},
	{
		in:     `"foo"(){}`,
		common: `1:1: invalid func name`,
	},
	{
		in:     `foo$bar(){}`,
		common: `1:1: invalid func name`,
	},
	{
		in:     "{",
		common: `1:1: reached EOF without matching { with }`,
	},
	{
		in:     "{ #}",
		common: `1:1: reached EOF without matching { with }`,
	},
	{
		in:     "(",
		common: `1:1: reached EOF without matching ( with )`,
	},
	{
		in:     ")",
		common: `1:1: ) can only be used to close a subshell`,
	},
	{
		in:     "`",
		common: "1:1: reached EOF without closing quote `",
	},
	{
		in:     ";;",
		common: `1:1: ;; can only be used in a case clause`,
	},
	{
		in:     "( foo;",
		common: `1:1: reached EOF without matching ( with )`,
	},
	{
		in:     "&",
		common: `1:1: & can only immediately follow a statement`,
	},
	{
		in:     "|",
		common: `1:1: | can only immediately follow a statement`,
	},
	{
		in:     "&&",
		common: `1:1: && can only immediately follow a statement`,
	},
	{
		in:     "||",
		common: `1:1: || can only immediately follow a statement`,
	},
	{
		in:     "foo; || bar",
		common: `1:6: || can only immediately follow a statement`,
	},
	{
		in:     "echo & || bar",
		common: `1:8: || can only immediately follow a statement`,
	},
	{
		in:     "echo & ; bar",
		common: `1:8: ; can only immediately follow a statement`,
	},
	{
		in:     "foo;;",
		common: `1:4: ;; can only be used in a case clause`,
	},
	{
		in:     "foo(",
		common: `1:1: "foo(" must be followed by )`,
	},
	{
		in:     "foo(bar",
		common: `1:1: "foo(" must be followed by )`,
	},
	{
		in:     "à(",
		common: `1:1: "foo(" must be followed by )`,
	},
	{
		in:     "foo'",
		common: `1:4: reached EOF without closing quote '`,
	},
	{
		in:     `foo"`,
		common: `1:4: reached EOF without closing quote "`,
	},
	{
		in:     `"foo`,
		common: `1:1: reached EOF without closing quote "`,
	},
	{
		in:     `"foobar\`,
		common: `1:1: reached EOF without closing quote "`,
	},
	{
		in:     `"foo\a`,
		common: `1:1: reached EOF without closing quote "`,
	},
	{
		in:     "foo()",
		common: `1:1: "foo()" must be followed by a statement`,
	},
	{
		in:     "foo() {",
		common: `1:7: reached EOF without matching { with }`,
	},
	{
		in:     "echo foo(",
		common: `1:9: a command can only contain words and redirects`,
	},
	{
		in:     "echo &&",
		common: `1:6: && must be followed by a statement`,
	},
	{
		in:     "echo |",
		common: `1:6: | must be followed by a statement`,
	},
	{
		in:     "echo ||",
		common: `1:6: || must be followed by a statement`,
	},
	{
		in:     "echo >",
		common: `1:6: > must be followed by a word`,
	},
	{
		in:     "echo >>",
		common: `1:6: >> must be followed by a word`,
	},
	{
		in:     "echo <",
		common: `1:6: < must be followed by a word`,
	},
	{
		in:     "echo 2>",
		common: `1:7: > must be followed by a word`,
	},
	{
		in:     "echo <\nbar",
		common: `2:1: redirect word must be on the same line`,
	},
	{
		in:     "echo <<",
		common: `1:6: << must be followed by a word`,
	},
	{
		in:     "echo <<\nEOF\nbar\nEOF",
		common: `2:1: redirect word must be on the same line`,
	},
	{
		in:     "if",
		common: `1:1: "if" must be followed by a statement list`,
	},
	{
		in:     "if true;",
		common: `1:1: "if <cond>" must be followed by "then"`,
	},
	{
		in:     "if true then",
		common: `1:1: "if <cond>" must be followed by "then"`,
	},
	{
		in:     "if true; then bar;",
		common: `1:1: if statement must end with "fi"`,
	},
	{
		in:     "if true; then bar; fi#etc",
		common: `1:1: if statement must end with "fi"`,
	},
	{
		in:     "if a; then b; elif c;",
		common: `1:15: "elif <cond>" must be followed by "then"`,
	},
	{
		in:     "'foo' '",
		common: `1:7: reached EOF without closing quote '`,
	},
	{
		in:     "'foo\n' '",
		common: `2:3: reached EOF without closing quote '`,
	},
	{
		in:     "while",
		common: `1:1: "while" must be followed by a statement list`,
	},
	{
		in:     "while true;",
		common: `1:1: "while <cond>" must be followed by "do"`,
	},
	{
		in:     "while true; do bar",
		common: `1:1: while statement must end with "done"`,
	},
	{
		in:     "while true; do bar;",
		common: `1:1: while statement must end with "done"`,
	},
	{
		in:     "until",
		common: `1:1: "until" must be followed by a statement list`,
	},
	{
		in:     "until true;",
		common: `1:1: "until <cond>" must be followed by "do"`,
	},
	{
		in:     "until true; do bar",
		common: `1:1: until statement must end with "done"`,
	},
	{
		in:     "until true; do bar;",
		common: `1:1: until statement must end with "done"`,
	},
	{
		in:     "for",
		common: `1:1: "for" must be followed by a literal`,
	},
	{
		in:     "for i",
		common: `1:1: "for foo" must be followed by "in", ; or a newline`,
	},
	{
		in:     "for i in;",
		common: `1:1: "for foo [in words]" must be followed by "do"`,
	},
	{
		in:     "for i in 1 2 3;",
		common: `1:1: "for foo [in words]" must be followed by "do"`,
	},
	{
		in:     "for i in 1 2 &",
		common: `1:14: word list can only contain words`,
	},
	{
		in:     "for i in 1 2 3; do echo $i;",
		common: `1:1: for statement must end with "done"`,
	},
	{
		in:     "for i in 1 2 3; echo $i;",
		common: `1:1: "for foo [in words]" must be followed by "do"`,
	},
	{
		in:     "for 'i' in 1 2 3; do echo $i; done",
		common: `1:1: "for" must be followed by a literal`,
	},
	{
		in:     "for in 1 2 3; do echo $i; done",
		common: `1:1: "for foo" must be followed by "in", ; or a newline`,
	},
	{
		in:     "echo foo &\n;",
		common: `2:1: ; can only immediately follow a statement`,
	},
	{
		in:     "echo $(foo",
		common: `1:6: reached EOF without matching ( with )`,
	},
	{
		in:     "echo $((foo",
		common: `1:6: reached EOF without matching $(( with ))`,
	},
	{
		in:     `echo $((\`,
		common: `1:6: reached EOF without matching $(( with ))`,
	},
	{
		in:     `echo $((o\`,
		common: `1:6: reached EOF without matching $(( with ))`,
	},
	{
		in:     `echo $((foo\a`,
		common: `1:6: reached EOF without matching $(( with ))`,
	},
	{
		in:     `echo $(("`,
		common: `1:9: arithmetic expressions must consist of names and numbers`,
	},
	{
		in:     `echo $(($(a"`,
		common: `1:9: arithmetic expressions must consist of names and numbers`,
	},
	{
		in:     `echo $(($((a"`,
		common: `1:9: arithmetic expressions must consist of names and numbers`,
	},
	{
		in:     "echo $((`echo 0`",
		common: `1:9: arithmetic expressions must consist of names and numbers`,
	},
	{
		in:     `echo $((& $(`,
		common: `1:9: & must follow an expression`,
	},
	{
		in:     `echo $((a'`,
		common: `1:10: not a valid arithmetic operator: '`,
	},
	{
		in:     `echo $((a b"`,
		common: `1:11: not a valid arithmetic operator: b`,
	},
	{
		in:     "echo $((\"`)",
		common: `1:9: arithmetic expressions must consist of names and numbers`,
	},
	{
		in:     "echo $((()))",
		common: `1:9: parentheses must enclose an expression`,
	},
	{
		in:     "echo $(((3))",
		common: `1:6: reached ) without matching $(( with ))`,
	},
	{
		in:     "echo $((+))",
		common: `1:9: + must be followed by an expression`,
	},
	{
		in:     "echo $((a b c))",
		common: `1:11: not a valid arithmetic operator: b`,
	},
	{
		in:     "echo $((a ; c))",
		common: `1:11: not a valid arithmetic operator: ;`,
	},
	{
		in:     "echo $((a *))",
		common: `1:11: * must be followed by an expression`,
	},
	{
		in:     "echo $((++))",
		common: `1:9: ++ must be followed by a literal`,
	},
	{
		in:     "echo $((a ? b))",
		common: `1:9: ternary operator missing : after ?`,
	},
	{
		in:     "echo $((a : b))",
		common: `1:9: ternary operator missing ? before :`,
	},
	{
		in:     "echo $((/",
		common: `1:9: / must follow an expression`,
	},
	{
		in:     "echo $((:",
		common: `1:9: : must follow an expression`,
	},
	{
		in:     "echo $(((a)+=b))",
		common: `1:12: += must follow a name`,
	},
	{
		in:     "echo $((1=2))",
		common: `1:10: = must follow a name`,
	},
	{
		in:     "echo $(($0=2)) #INVBASH",
		common: `1:11: = must follow a name`,
	},
	{
		in:     "echo $(('1=2'))",
		common: `1:9: arithmetic expressions must consist of names and numbers`,
	},
	{
		in:     "echo $((1'2'))",
		common: `1:10: not a valid arithmetic operator: '`,
	},
	{
		in:     "echo $(($1'2'))",
		common: `1:11: not a valid arithmetic operator: '`,
	},
	{
		in:     "<<EOF\n$(()a",
		common: `2:1: reached ) without matching $(( with ))`,
	},
	{
		in:     "<<EOF\n`))",
		common: `2:2: ) can only be used to close a subshell`,
	},
	{
		in:     "echo ${foo",
		common: `1:6: reached EOF without matching ${ with }`,
	},
	{
		in:     "echo $foo ${}",
		common: `1:11: parameter expansion requires a literal`,
	},
	{
		in:     "echo ${foo-bar",
		common: `1:6: reached EOF without matching ${ with }`,
	},
	{
		in:     "#foo\n{",
		common: `2:1: reached EOF without matching { with }`,
	},
	{
		in:     `echo "foo${bar"`,
		common: `1:10: reached EOF without matching ${ with }`,
	},
	{
		in:     "echo ${##",
		common: `1:6: reached EOF without matching ${ with }`,
	},
	{
		in:     "echo ${$foo}",
		common: `1:9: $ cannot be followed by a word`,
	},
	{
		in:     "echo ${?foo}",
		common: `1:9: ? cannot be followed by a word`,
	},
	{
		in:     "echo ${-foo}",
		common: `1:9: - cannot be followed by a word`,
	},
	{
		in:     "echo foo\n;",
		common: `2:1: ; can only immediately follow a statement`,
	},
	{
		in:     "(foo) bar",
		common: `1:7: statements must be separated by &, ; or a newline`,
	},
	{
		in:     "{ foo; } bar",
		common: `1:10: statements must be separated by &, ; or a newline`,
	},
	{
		in:     "if foo; then bar; fi bar",
		common: `1:22: statements must be separated by &, ; or a newline`,
	},
	{
		in:     "case",
		common: `1:1: "case" must be followed by a word`,
	},
	{
		in:     "case i",
		common: `1:1: "case x" must be followed by "in"`,
	},
	{
		in:     "case i in 3) foo;",
		common: `1:1: case statement must end with "esac"`,
	},
	{
		in:     "case i in 3) foo; 4) bar; esac",
		common: `1:20: a command can only contain words and redirects`,
	},
	{
		in:     "case i in 3&) foo;",
		common: `1:12: case patterns must be separated with |`,
	},
	{
		in:     "case $i in &) foo;",
		common: `1:12: case patterns must consist of words`,
	},
	{
		in:     "\"`\"",
		common: `1:3: reached EOF without closing quote "`,
	},
	{
		in:     "`\"`",
		common: "1:3: reached EOF without closing quote `",
	},
	{
		in:     "`{\n`",
		common: "1:2: reached ` without matching { with }",
	},
	{
		in:     "echo \"`)`\"",
		common: `1:8: ) can only be used to close a subshell`,
	},
	{
		in:     "<<$bar #INVBASH bash allows this",
		common: `1:3: expansions not allowed in heredoc words`,
	},
	{
		in:     "<<${bar} #INVBASH bash allows this",
		common: `1:3: expansions not allowed in heredoc words`,
	},
	{
		in:     "<<$(bar) #INVBASH bash allows this",
		common: `1:3: expansions not allowed in heredoc words`,
	},
	{
		in:     "<<$+ #INVBASH bash allows this",
		common: `1:3: expansions not allowed in heredoc words`,
	},
	{
		in:     "<<`bar` #INVBASH bash allows this",
		common: `1:3: expansions not allowed in heredoc words`,
	},
	{
		in:     `<<"$bar" #INVBASH bash allows this`,
		common: `1:4: expansions not allowed in heredoc words`,
	},
	{
		in:     "<<$ #INVBASH",
		common: `1:3: expansions not allowed in heredoc words`,
	},
	{
		in:     "<<a <<0\n$(<<$<< #INVBASH",
		common: `2:5: expansions not allowed in heredoc words`,
	},
	{
		in:     `""()`,
		common: `1:1: invalid func name`,
	},
	{
		// bash errors on the empty condition here, this is to
		// add coverage for empty statement lists
		in:     `if; then bar; fi; ;`,
		common: `1:19: ; can only immediately follow a statement`,
	},
	{
		in:   "]] )",
		bash: `1:1: ]] can only be used to close a test`,
	},
	{
		in:   "((foo",
		bash: `1:1: reached EOF without matching (( with ))`,
	},
	{
		in:   "echo ((foo",
		bash: `1:6: (( can only be used to open an arithmetic cmd`,
	},
	{
		in:   "echo |&",
		bash: `1:6: |& must be followed by a statement`,
	},
	{
		in:   "|& a",
		bash: `1:1: |& is not a valid start for a statement`,
	},
	{
		in:   "let",
		bash: `1:1: let clause requires at least one expression`,
	},
	{
		in:   "let a+ b",
		bash: `1:6: + must be followed by an expression`,
	},
	{
		in:   "let + a",
		bash: `1:5: + must be followed by an expression`,
	},
	{
		in:   "let a ++",
		bash: `1:7: ++ must be followed by a literal`,
	},
	{
		in:   "let (a)++",
		bash: `1:8: ++ must follow a name`,
	},
	{
		in:   "let 1++",
		bash: `1:6: ++ must follow a name`,
	},
	{
		in:   "let $0++",
		bash: `1:7: ++ must follow a name`,
	},
	{
		in:   "let --(a)",
		bash: `1:5: -- must be followed by a literal`,
	},
	{
		in:   "let --$a",
		bash: `1:5: -- must be followed by a literal`,
	},
	{
		in:   "let a+\n",
		bash: `1:6: + must be followed by an expression`,
	},
	{
		in:   "let ))",
		bash: `1:1: let clause requires at least one expression`,
	},
	{
		in:   "`let !`",
		bash: `1:6: ! must be followed by an expression`,
	},
	{
		in:   "let a:b",
		bash: `1:5: ternary operator missing ? before :`,
	},
	{
		in:   "let a+b=c",
		bash: `1:8: = must follow a name`,
	},
	{
		in:   "let 'foo'",
		bash: `1:5: arithmetic expressions must consist of names and numbers`,
	},
	{
		in:   `let a"=b+c"`,
		bash: `1:5: arithmetic expressions must consist of names and numbers`,
	},
	{
		in:   "[[",
		bash: `1:1: test clause requires at least one expression`,
	},
	{
		in:   "[[ ]]",
		bash: `1:1: test clause requires at least one expression`,
	},
	{
		in:   "[[ a",
		bash: `1:1: reached EOF without matching [[ with ]]`,
	},
	{
		in:   "[[ a ||",
		bash: `1:6: || must be followed by an expression`,
	},
	{
		in:   "[[ a ==",
		bash: `1:6: == must be followed by a word`,
	},
	{
		in:   "[[ a =~",
		bash: `1:6: =~ must be followed by a word`,
	},
	{
		in:   "[[ -f a",
		bash: `1:1: reached EOF without matching [[ with ]]`,
	},
	{
		in:   "[[ a -nt b",
		bash: `1:1: reached EOF without matching [[ with ]]`,
	},
	{
		in:   "[[ a =~ b",
		bash: `1:1: reached EOF without matching [[ with ]]`,
	},
	{
		in:   "[[ a b c ]]",
		bash: `1:6: not a valid test operator: b`,
	},
	{
		in:   "[[ a b$x c ]]",
		bash: `1:6: test operator words must consist of a single literal`,
	},
	{
		in:   "[[ a & b ]]",
		bash: `1:6: not a valid test operator: &`,
	},
	{
		in:   "[[ true && () ]]",
		bash: `1:12: parentheses must enclose an expression`,
	},
	{
		in:   "[[ a == ! b ]]",
		bash: `1:11: not a valid test operator: b`,
	},
	{
		in:   "[[ (a) == b ]]",
		bash: `1:8: expected &&, || or ]] after complex expr`,
	},
	{
		in:   "[[ a =~ ; ]]",
		bash: `1:6: =~ must be followed by a word`,
	},
	{
		in:   "[[ >",
		bash: `1:1: [[ must be followed by a word`,
	},
	{
		in:   "local (",
		bash: `1:7: "local" must be followed by words`,
	},
	{
		in:   "declare 0=${o})",
		bash: `1:15: statements must be separated by &, ; or a newline`,
	},
	{
		in:   "a=(<)",
		bash: `1:4: array elements must be words`,
	},
	{
		in:   "function",
		bash: `1:1: "function" must be followed by a word`,
	},
	{
		in:   "function foo(",
		bash: `1:10: "foo(" must be followed by )`,
	},
	{
		in:   "function `function",
		bash: `1:11: "function" must be followed by a word`,
	},
	{
		in:   `function "foo"(){}`,
		bash: `1:10: invalid func name`,
	},
	{
		in:   "function foo()",
		bash: `1:1: "foo()" must be followed by a statement`,
	},
	{
		in:   "echo <<<",
		bash: `1:6: <<< must be followed by a word`,
	},
	{
		in:   "a[",
		bash: `1:2: [ must be followed by an expression`,
	},
	{
		in:   "a[b",
		bash: `1:2: reached EOF without matching [ with ]`,
	},
	{
		in:   "a[] #INVBASH allows as a cmd",
		bash: `1:2: [ must be followed by an expression`,
	},
	{
		in:   "echo $((a[))",
		bash: `1:11: [ must be followed by an expression`,
	},
	{
		in:   "echo $((a[b))",
		bash: `1:11: reached ) without matching [ with ]`,
	},
	{
		in:   "echo $((a[]))",
		bash: `1:11: [ must be followed by an expression`,
	},
	{
		in:   "a[1] #INVBASH allows as a cmd",
		bash: `1:1: "a[b]" must be followed by =`,
	},
	{
		in:   "echo $[foo",
		bash: `1:6: reached EOF without matching $[ with ]`,
	},
	{
		in:   "echo $'",
		bash: `1:6: reached EOF without closing quote '`,
	},
	{
		in:   `echo $"`,
		bash: `1:6: reached EOF without closing quote "`,
	},
	{
		in:   `$"foo$" #INVBASH`,
		bash: `1:6: $ must be escaped or followed by a literal`,
	},
	{
		in:   "echo @(",
		bash: `1:6: reached EOF without matching @( with )`,
	},
	{
		in:   "echo @(a",
		bash: `1:6: reached EOF without matching @( with )`,
	},
	{
		in:   "((@(",
		bash: `1:1: reached ( without matching (( with ))`,
	},
	{
		in:   "echo $((\"a`b((",
		bash: `1:9: arithmetic expressions must consist of names and numbers`,
	},
	{
		in:   "coproc",
		bash: `1:1: coproc clause requires a command`,
	},
	{
		in:   "coproc\n$",
		bash: `1:1: coproc clause requires a command`,
	},
	{
		in:   "coproc declare (",
		bash: `1:16: "declare" must be followed by words`,
	},
	{
		in:   "`let` { foo; }",
		bash: `1:2: let clause requires at least one expression`,
	},
	{
		in:   "echo ${foo[1 2]}",
		bash: `1:14: not a valid arithmetic operator: 2`,
	},
	{
		in:   "echo ${foo[}",
		bash: `1:11: [ must be followed by an expression`,
	},
	{
		in:   "echo ${foo[]}",
		bash: `1:11: [ must be followed by an expression`,
	},
	{
		in:   "echo ${a/\n",
		bash: `1:6: reached EOF without matching ${ with }`,
	},
	{
		in:   "echo ${a-\n",
		bash: `1:6: reached EOF without matching ${ with }`,
	},
	{
		in:   "echo ${foo:",
		bash: `1:11: : must be followed by an expression`,
	},
	{
		in:   "echo ${foo:1 2} #INVBASH lazy eval",
		bash: `1:14: not a valid arithmetic operator: 2`,
	},
	{
		in:   "echo ${foo:1",
		bash: `1:6: reached EOF without matching ${ with }`,
	},
	{
		in:   "echo ${foo:1:",
		bash: `1:13: : must be followed by an expression`,
	},
	{
		in:   "echo ${foo:1:2",
		bash: `1:6: reached EOF without matching ${ with }`,
	},
	{
		in:   "echo ${foo,",
		bash: `1:6: reached EOF without matching ${ with }`,
	},
	{
		in:   "echo ${foo@",
		bash: `1:6: reached EOF without matching ${ with }`,
	},
	{
		in:   `echo $((echo a); (echo b)) #INVBASH bash does backtrack`,
		bash: `1:14: not a valid arithmetic operator: a`,
	},
	{
		in:   `((echo a); (echo b)) #INVBASH bash does backtrack`,
		bash: `1:8: not a valid arithmetic operator: a`,
	},
	{
		in:   "for ((;;0000000",
		bash: `1:5: reached EOF without matching (( with ))`,
	},
	{
		in:   "a <<EOF\n$''$bar\nEOF #INVBASH",
		bash: `2:1: $ must be escaped or followed by a literal`,
	},
	{
		in:    "]] )",
		posix: `1:4: a command can only contain words and redirects`,
	},
	{
		in:    "((foo",
		posix: `1:2: reached EOF without matching ( with )`,
	},
	{
		in:    "echo ((foo",
		posix: `1:1: "foo(" must be followed by )`,
	},
	{
		in:    "function foo() { bar; } #INVBASH --posix is wrong",
		posix: `1:13: a command can only contain words and redirects`,
	},
	{
		in:    "echo <(",
		posix: `1:6: < must be followed by a word`,
	},
	{
		in:    "echo >(",
		posix: `1:6: > must be followed by a word`,
	},
	{
		in:    "echo |&",
		posix: `1:6: | must be followed by a statement`,
	},
	{
		in:    "echo ;&",
		posix: `1:7: & can only immediately follow a statement`,
	},
	{
		in:    "echo ;;&",
		posix: `1:6: ;; can only be used in a case clause`,
	},
	{
		in:    "for ((i=0; i<5; i++)); do echo; done #INVBASH --posix is wrong",
		posix: `1:1: "for" must be followed by a literal`,
	},
	{
		in:    `$'' #INVBASH`,
		posix: `1:1: $ must be escaped or followed by a literal`,
	},
	{
		in:    `$"" #INVBASH`,
		posix: `1:1: $ must be escaped or followed by a literal`,
	},
	{
		in:    `$[foo] #INVBASH`,
		posix: `1:1: $ must be escaped or followed by a literal`,
	},
	{
		in:    `"$[foo]" #INVBASH`,
		posix: `1:2: $ must be escaped or followed by a literal`,
	},
	{
		in:    "echo !(a) #INVBASH --posix is wrong",
		posix: `1:6: extended globs are a bash feature`,
	},
	{
		in:    "echo $a@(b) #INVBASH --posix is wrong",
		posix: `1:8: extended globs are a bash feature`,
	},
	{
		in:    "foo=(1 2) #INVBASH --posix is wrong",
		posix: `1:5: arrays are a bash feature`,
	},
	{
		in:    "echo ${foo[1]} #INVBASH --posix is wrong",
		posix: `1:11: arrays are a bash feature`,
	},
	{
		in:    "echo ${foo/a/b} #INVBASH --posix is wrong",
		posix: `1:11: search and replace is a bash feature`,
	},
	{
		in:    "echo ${foo:1} #INVBASH --posix is wrong",
		posix: `1:11: slicing is a bash feature`,
	},
	{
		in:    "echo ${foo,bar} #INVBASH --posix is wrong",
		posix: `1:11: this expansion operator is a bash feature`,
	},
	{
		in:    "echo ${foo@bar} #INVBASH --posix is wrong",
		posix: `1:11: this expansion operator is a bash feature`,
	},
}

func checkError(p *Parser, in, want string) func(*testing.T) {
	return func(t *testing.T) {
		if i := strings.Index(in, " #INVBASH"); i >= 0 {
			in = in[:i]
		}
		_, err := p.Parse(newStrictReader(in), "")
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
	t.Parallel()
	p := NewParser(Variant(LangPOSIX))
	i := 0
	for _, c := range shellTests {
		want := c.common
		if c.posix != nil {
			want = c.posix
		}
		if want == nil {
			continue
		}
		t.Run(fmt.Sprintf("%03d", i), checkError(p, c.in, want.(string)))
		i++
	}
}

func TestParseErrBash(t *testing.T) {
	t.Parallel()
	p := NewParser()
	i := 0
	for _, c := range shellTests {
		want := c.common
		if c.bash != nil {
			want = c.bash
		}
		if want == nil {
			continue
		}
		t.Run(fmt.Sprintf("%03d", i), checkError(p, c.in, want.(string)))
		i++
	}
}

func TestInputName(t *testing.T) {
	in := shellTests[0].in
	want := "some-file.sh:" + shellTests[0].common.(string)
	p := NewParser()
	_, err := p.Parse(strings.NewReader(in), "some-file.sh")
	if err == nil {
		t.Fatalf("Expected error in %q: %v", in, want)
	}
	got := err.Error()
	if got != want {
		t.Fatalf("Error mismatch in %q\nwant: %s\ngot:  %s",
			in, want, got)
	}
}

var errBadReader = fmt.Errorf("write: expected error")

type badReader struct{}

func (b badReader) Read(p []byte) (int, error) { return 0, errBadReader }

func TestReadErr(t *testing.T) {
	p := NewParser()
	_, err := p.Parse(badReader{}, "")
	if err == nil {
		t.Fatalf("Expected error with bad reader")
	}
	if err != errBadReader {
		t.Fatalf("Error mismatch with bad reader:\nwant: %v\ngot:  %v",
			errBadReader, err)
	}
}

type strictStringReader struct {
	*strings.Reader
	gaveEOF bool
}

func newStrictReader(s string) *strictStringReader {
	return &strictStringReader{Reader: strings.NewReader(s)}
}

func (r *strictStringReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF {
		if r.gaveEOF {
			return n, fmt.Errorf("duplicate EOF read")
		}
		r.gaveEOF = true
	}
	return n, err
}
