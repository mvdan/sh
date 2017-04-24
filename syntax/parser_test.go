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

func TestParseComments(t *testing.T) {
	in := "# foo\ncmd\n# bar"
	want := &File{
		Comments: []*Comment{
			{Text: " foo"},
			{Text: " bar"},
		},
		Stmts: litStmts("cmd"),
	}
	singleParse(in, want, ParseComments)(t)
}

func TestParseBash(t *testing.T) {
	t.Parallel()
	for i, c := range append(fileTests, fileTestsNoPrint...) {
		want := c.Bash
		if want == nil {
			continue
		}
		for j, in := range c.Strs {
			t.Run(fmt.Sprintf("%03d-%d", i, j), singleParse(in, want, 0))
		}
	}
}

func TestParsePosix(t *testing.T) {
	t.Parallel()
	for i, c := range append(fileTests, fileTestsNoPrint...) {
		want := c.Posix
		if want == nil {
			continue
		}
		for j, in := range c.Strs {
			t.Run(fmt.Sprintf("%03d-%d", i, j),
				singleParse(in, want, PosixConformant))
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
	for i, c := range append(shellTests, bashTests...) {
		t.Run(fmt.Sprintf("%03d", i), confirmParse(c.in, false, true))
	}
}

func TestParseErrPosixConfirm(t *testing.T) {
	if testing.Short() {
		t.Skip("calling bash is slow.")
	}
	if !hasBash44 {
		t.Skip("bash 4.4 required to run")
	}
	for i, c := range append(shellTests, posixTests...) {
		t.Run(fmt.Sprintf("%03d", i), confirmParse(c.in, true, true))
	}
}

func singleParse(in string, want *File, mode ParseMode) func(t *testing.T) {
	return func(t *testing.T) {
		if i := strings.Index(in, " #INVBASH"); i >= 0 {
			in = in[:i]
		}
		got, err := Parse(newStrictReader(in), "", mode)
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
		in := strings.NewReader(c.in)
		b.Run(c.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := Parse(in, "", ParseComments); err != nil {
					b.Fatal(err)
				}
				in.Reset(c.in)
			}
		})
	}
}

type errorCase struct {
	in, want string
}

var shellTests = []errorCase{
	{
		"echo \x80 #INVBASH bash uses bytes",
		`1:6: invalid UTF-8 encoding`,
	},
	{
		"\necho \x80 #INVBASH bash uses bytes",
		`2:6: invalid UTF-8 encoding`,
	},
	{
		"echo foo\x80bar #INVBASH bash uses bytes",
		`1:9: invalid UTF-8 encoding`,
	},
	{
		"echo foo\xc3 #INVBASH bash uses bytes",
		`1:9: invalid UTF-8 encoding`,
	},
	{
		"#foo\xc3 #INVBASH bash uses bytes",
		`1:5: invalid UTF-8 encoding`,
	},
	{
		"$((foo\x80bar",
		`1:7: invalid UTF-8 encoding`,
	},
	{
		"((foo\x80bar",
		`1:6: invalid UTF-8 encoding`,
	},
	{
		";\x80",
		`1:2: invalid UTF-8 encoding`,
	},
	{
		"echo a\x80 #INVBASH bash uses bytes",
		`1:7: invalid UTF-8 encoding`,
	},
	{
		"${a\x80",
		`1:4: invalid UTF-8 encoding`,
	},
	{
		"${a#\x80",
		`1:5: invalid UTF-8 encoding`,
	},
	{
		"$((a |\x80",
		`1:7: invalid UTF-8 encoding`,
	},
	{
		"<<$\xc8 #INVBASH bash uses bytes",
		`1:4: invalid UTF-8 encoding`,
	},
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
		`1:1: invalid func name`,
	},
	{
		`foo$bar(){}`,
		`1:1: invalid func name`,
	},
	{
		"{",
		`1:1: reached EOF without matching { with }`,
	},
	{
		"}",
		`1:1: } can only be used to close a block`,
	},
	{
		"{ #}",
		`1:1: reached EOF without matching { with }`,
	},
	{
		"(",
		`1:1: reached EOF without matching ( with )`,
	},
	{
		")",
		`1:1: ) can only be used to close a subshell`,
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
		`1:1: reached EOF without matching ( with )`,
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
		"echo & || bar",
		`1:8: || can only immediately follow a statement`,
	},
	{
		"echo & ; bar",
		`1:8: ; can only immediately follow a statement`,
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
		`"foobar\`,
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
		"foo() {",
		`1:7: reached EOF without matching { with }`,
	},
	{
		"echo foo(",
		`1:9: a command can only contain words and redirects`,
	},
	{
		"echo &&",
		`1:6: && must be followed by a statement`,
	},
	{
		"echo |",
		`1:6: | must be followed by a statement`,
	},
	{
		"echo ||",
		`1:6: || must be followed by a statement`,
	},
	{
		"echo >",
		`1:6: > must be followed by a word`,
	},
	{
		"echo >>",
		`1:6: >> must be followed by a word`,
	},
	{
		"echo <",
		`1:6: < must be followed by a word`,
	},
	{
		"echo 2>",
		`1:7: > must be followed by a word`,
	},
	{
		"echo <\nbar",
		`2:1: redirect word must be on the same line`,
	},
	{
		"echo <<",
		`1:6: << must be followed by a word`,
	},
	{
		"echo <<\nEOF\nbar\nEOF",
		`2:1: redirect word must be on the same line`,
	},
	{
		"if",
		`1:1: "if" must be followed by a statement list`,
	},
	{
		"if true;",
		`1:1: "if <cond>" must be followed by "then"`,
	},
	{
		"if true then",
		`1:1: "if <cond>" must be followed by "then"`,
	},
	{
		"if true; then bar;",
		`1:1: if statement must end with "fi"`,
	},
	{
		"if true; then bar; fi#etc",
		`1:1: if statement must end with "fi"`,
	},
	{
		"if a; then b; elif c;",
		`1:15: "elif <cond>" must be followed by "then"`,
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
		"while true;",
		`1:1: "while <cond>" must be followed by "do"`,
	},
	{
		"while true; do bar",
		`1:1: while statement must end with "done"`,
	},
	{
		"while true; do bar;",
		`1:1: while statement must end with "done"`,
	},
	{
		"until",
		`1:1: "until" must be followed by a statement list`,
	},
	{
		"until true;",
		`1:1: "until <cond>" must be followed by "do"`,
	},
	{
		"until true; do bar",
		`1:1: until statement must end with "done"`,
	},
	{
		"until true; do bar;",
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
		"echo foo &\n;",
		`2:1: ; can only immediately follow a statement`,
	},
	{
		"echo $(foo",
		`1:6: reached EOF without matching ( with )`,
	},
	{
		"echo $((foo",
		`1:6: reached EOF without matching $(( with ))`,
	},
	{
		`echo $((\`,
		`1:6: reached EOF without matching $(( with ))`,
	},
	{
		`fo $((o\`,
		`1:4: reached EOF without matching $(( with ))`,
	},
	{
		`echo $((foo\a`,
		`1:6: reached EOF without matching $(( with ))`,
	},
	{
		`$(("`,
		`1:4: reached EOF without closing quote "`,
	},
	{
		`$((a"`,
		`1:5: reached EOF without closing quote "`,
	},
	{
		`$(($((a"`,
		`1:8: reached EOF without closing quote "`,
	},
	{
		`$(('`,
		`1:4: reached EOF without closing quote '`,
	},
	{
		`$((& $(`,
		`1:4: & must follow an expression`,
	},
	{
		`$((& 0 $(`,
		`1:4: & must follow an expression`,
	},
	{
		`$((a'`,
		`1:5: not a valid arithmetic operator: '`,
	},
	{
		`$((a b"`,
		`1:6: not a valid arithmetic operator: b`,
	},
	{
		`$((a"'`,
		`1:5: reached EOF without closing quote "`,
	},
	{
		"$((\"`)",
		`1:6: ) can only be used to close a subshell`,
	},
	{
		"echo $((()))",
		`1:9: parentheses must enclose an expression`,
	},
	{
		"echo $(((3))",
		`1:6: reached ) without matching $(( with ))`,
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
		"echo $((a ; c))",
		`1:11: not a valid arithmetic operator: ;`,
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
		"echo $((a ? b))",
		`1:9: ternary operator missing : after ?`,
	},
	{
		"echo $((a : b))",
		`1:9: ternary operator missing ? before :`,
	},
	{
		"echo $((/",
		`1:9: / must follow an expression`,
	},
	{
		"echo $((:",
		`1:9: : must follow an expression`,
	},
	{
		"echo $(((a)+=b))",
		`1:12: += must follow a name`,
	},
	{
		"echo $((1=2))",
		`1:10: = must follow a name`,
	},
	{
		"<<EOF\n$(()a",
		`2:1: reached ) without matching $(( with ))`,
	},
	{
		"<<EOF\n`))",
		`2:2: ) can only be used to close a subshell`,
	},
	{
		"echo ${foo",
		`1:6: reached EOF without matching ${ with }`,
	},
	{
		"echo $foo ${}",
		`1:11: parameter expansion requires a literal`,
	},
	{
		"echo ${foo-bar",
		`1:6: reached EOF without matching ${ with }`,
	},
	{
		"#foo\n{",
		`2:1: reached EOF without matching { with }`,
	},
	{
		`echo "foo${bar"`,
		`1:10: reached EOF without matching ${ with }`,
	},
	{
		"echo ${##",
		`1:6: reached EOF without matching ${ with }`,
	},
	{
		"echo ${$foo}",
		`1:9: $ cannot be followed by a word`,
	},
	{
		"echo ${?foo}",
		`1:9: ? cannot be followed by a word`,
	},
	{
		"echo ${-foo}",
		`1:9: - cannot be followed by a word`,
	},
	{
		"echo foo\n;",
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
		"1:2: reached ` without matching { with }",
	},
	{
		"echo \"`)`\"",
		`1:8: ) can only be used to close a subshell`,
	},
	{
		"<<$bar #INVBASH bash allows this",
		`1:3: expansions not allowed in heredoc words`,
	},
	{
		"<<${bar} #INVBASH bash allows this",
		`1:3: expansions not allowed in heredoc words`,
	},
	{
		"<<$(bar) #INVBASH bash allows this",
		`1:3: expansions not allowed in heredoc words`,
	},
	{
		"<<$+ #INVBASH bash allows this",
		`1:3: expansions not allowed in heredoc words`,
	},
	{
		"<<`bar` #INVBASH bash allows this",
		`1:3: expansions not allowed in heredoc words`,
	},
	{
		`<<"$bar" #INVBASH bash allows this`,
		`1:4: expansions not allowed in heredoc words`,
	},
	{
		"<<$ <<0\n$(<<$<<",
		`2:6: << must be followed by a word`,
	},
	{
		`""()`,
		`1:1: invalid func name`,
	},
	{
		// bash errors on the empty condition here, this is to
		// add coverage for empty statement lists
		`if; then bar; fi; ;`,
		`1:19: ; can only immediately follow a statement`,
	},
}

func checkError(in, want string, mode ParseMode) func(*testing.T) {
	return func(t *testing.T) {
		if i := strings.Index(in, " #INVBASH"); i >= 0 {
			in = in[:i]
		}
		_, err := Parse(newStrictReader(in), "", mode)
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
	for i, c := range append(shellTests, posixTests...) {
		t.Run(fmt.Sprintf("%03d", i), checkError(c.in, c.want, PosixConformant))
	}
}

func TestParseErrBash(t *testing.T) {
	t.Parallel()
	for i, c := range append(shellTests, bashTests...) {
		t.Run(fmt.Sprintf("%03d", i), checkError(c.in, c.want, 0))
	}
}

var bashTests = []errorCase{
	{
		"((foo",
		`1:1: reached EOF without matching (( with ))`,
	},
	{
		"echo ((foo",
		`1:6: (( can only be used to open an arithmetic cmd`,
	},
	{
		"echo |&",
		`1:6: |& must be followed by a statement`,
	},
	{
		"|& a",
		`1:1: |& is not a valid start for a statement`,
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
		"let (a)++",
		`1:8: ++ must follow a name`,
	},
	{
		"let 1++",
		`1:6: ++ must follow a name`,
	},
	{
		"let --(a)",
		`1:5: -- must be followed by a word`,
	},
	{
		"let a+\n",
		`1:6: + must be followed by an expression`,
	},
	{
		"let ))",
		`1:1: let clause requires at least one expression`,
	},
	{
		"`let !`",
		`1:6: ! must be followed by an expression`,
	},
	{
		"let 'foo'\n'",
		`2:1: reached EOF without closing quote '`,
	},
	{
		"let a:b",
		`1:5: ternary operator missing ? before :`,
	},
	{
		"let a+b=c",
		`1:8: = must follow a name`,
	},
	{
		"[[",
		`1:1: test clause requires at least one expression`,
	},
	{
		"[[ ]]",
		`1:1: test clause requires at least one expression`,
	},
	{
		"[[ a",
		`1:1: reached EOF without matching [[ with ]]`,
	},
	{
		"[[ a ||",
		`1:6: || must be followed by an expression`,
	},
	{
		"[[ a ==",
		`1:6: == must be followed by a word`,
	},
	{
		"[[ a =~",
		`1:6: =~ must be followed by a word`,
	},
	{
		"[[ -f a",
		`1:1: reached EOF without matching [[ with ]]`,
	},
	{
		"[[ a -nt b",
		`1:1: reached EOF without matching [[ with ]]`,
	},
	{
		"[[ a =~ b",
		`1:1: reached EOF without matching [[ with ]]`,
	},
	{
		"[[ a b c ]]",
		`1:6: not a valid test operator: b`,
	},
	{
		"[[ a b$ c ]]",
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
		"[[ a == ! b ]]",
		`1:11: not a valid test operator: b`,
	},
	{
		"[[ (a) == b ]]",
		`1:8: expected &&, || or ]] after complex expr`,
	},
	{
		"[[ a =~ ; ]]",
		`1:6: =~ must be followed by a word`,
	},
	{
		"[[ >",
		`1:1: [[ must be followed by a word`,
	},
	{
		"local (",
		`1:7: "local" must be followed by words`,
	},
	{
		"declare 0=${o})",
		`1:15: statements must be separated by &, ; or a newline`,
	},
	{
		"a=(<)",
		`1:4: array elements must be words`,
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
		`function "foo"(){}`,
		`1:10: invalid func name`,
	},
	{
		"function foo()",
		`1:1: "foo()" must be followed by a statement`,
	},
	{
		"echo <<<",
		`1:6: <<< must be followed by a word`,
	},
	{
		"echo $[foo",
		`1:6: reached EOF without matching $[ with ]`,
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
		"echo @(",
		`1:6: reached EOF without matching @( with )`,
	},
	{
		"echo @(a",
		`1:6: reached EOF without matching @( with )`,
	},
	{
		"((@(",
		`1:1: reached ( without matching (( with ))`,
	},
	{
		"$((\"a`b((",
		`1:8: (( can only be used to open an arithmetic cmd`,
	},
	{
		"coproc",
		`1:1: coproc clause requires a command`,
	},
	{
		"coproc\n$",
		`1:1: coproc clause requires a command`,
	},
	{
		"coproc declare (",
		`1:16: "declare" must be followed by words`,
	},
	{
		"`let` { foo; }",
		`1:2: let clause requires at least one expression`,
	},
	{
		"echo ${foo[1 2]}",
		`1:14: not a valid arithmetic operator: 2`,
	},
	{
		"echo ${foo[}",
		`1:11: [ must be followed by an expression`,
	},
	{
		"echo ${foo[]}",
		`1:11: [ must be followed by an expression`,
	},
	{
		"echo ${a/\n",
		`1:6: reached EOF without matching ${ with }`,
	},
	{
		"echo ${a-\n",
		`1:6: reached EOF without matching ${ with }`,
	},
	{
		"echo ${foo:",
		`1:11: : must be followed by an expression`,
	},
	{
		"echo ${foo:1 2} #INVBASH lazy eval",
		`1:14: not a valid arithmetic operator: 2`,
	},
	{
		"echo ${foo:1",
		`1:6: reached EOF without matching ${ with }`,
	},
	{
		"echo ${foo:1:",
		`1:13: : must be followed by an expression`,
	},
	{
		"echo ${foo:1:2",
		`1:6: reached EOF without matching ${ with }`,
	},
	{
		"echo ${foo,",
		`1:6: reached EOF without matching ${ with }`,
	},
	{
		"echo ${foo@",
		`1:6: reached EOF without matching ${ with }`,
	},
	{
		`$((echo a); (echo b)) #INVBASH bash does backtrack`,
		`1:9: not a valid arithmetic operator: a`,
	},
	{
		`((echo a); (echo b)) #INVBASH bash does backtrack`,
		`1:8: not a valid arithmetic operator: a`,
	},
	{
		"for ((;;0000000",
		`1:5: reached EOF without matching (( with ))`,
	},
}

var posixTests = []errorCase{
	{
		"((foo",
		`1:2: reached EOF without matching ( with )`,
	},
	{
		"echo ((foo",
		`1:1: "foo(" must be followed by )`,
	},
	{
		"function foo() { bar; } #INVBASH --posix is wrong",
		`1:13: a command can only contain words and redirects`,
	},
	{
		"echo <(",
		`1:6: < must be followed by a word`,
	},
	{
		"echo >(",
		`1:6: > must be followed by a word`,
	},
	{
		"echo |&",
		`1:6: | must be followed by a statement`,
	},
	{
		"echo ;&",
		`1:7: & can only immediately follow a statement`,
	},
	{
		"echo ;;&",
		`1:6: ;; can only be used in a case clause`,
	},
	{
		"for ((i=0; i<5; i++)); do echo; done #INVBASH --posix is wrong",
		`1:1: "for" must be followed by a literal`,
	},
	{
		"echo !(a) #INVBASH --posix is wrong",
		`1:6: extended globs are a bash feature`,
	},
	{
		"echo $a@(b) #INVBASH --posix is wrong",
		`1:8: extended globs are a bash feature`,
	},
	{
		"foo=(1 2) #INVBASH --posix is wrong",
		`1:5: arrays are a bash feature`,
	},
	{
		"echo ${foo[1]} #INVBASH --posix is wrong",
		`1:11: arrays are a bash feature`,
	},
	{
		"echo ${foo/a/b} #INVBASH --posix is wrong",
		`1:11: search and replace is a bash feature`,
	},
	{
		"echo ${foo:1} #INVBASH --posix is wrong",
		`1:11: slicing is a bash feature`,
	},
	{
		"echo ${foo,bar} #INVBASH --posix is wrong",
		`1:11: this expansion operator is a bash feature`,
	},
	{
		"echo ${foo@bar} #INVBASH --posix is wrong",
		`1:11: this expansion operator is a bash feature`,
	},
}

func TestInputName(t *testing.T) {
	in := shellTests[0].in
	want := "some-file.sh:" + shellTests[0].want
	_, err := Parse(strings.NewReader(in), "some-file.sh", 0)
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
	_, err := Parse(badReader{}, "", 0)
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
