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
	t.Parallel()
	in := "# foo\ncmd\n# bar"
	want := &File{
		Stmts: []*Stmt{{
			Comments: []Comment{{Text: " foo"}},
			Cmd:      litCall("cmd"),
		}},
		Last: []Comment{{Text: " bar"}},
	}
	singleParse(NewParser(KeepComments(true)), in, want)(t)
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

func TestParseMirBSDKorn(t *testing.T) {
	t.Parallel()
	p := NewParser(Variant(LangMirBSDKorn))
	for i, c := range append(fileTests, fileTestsNoPrint...) {
		want := c.MirBSDKorn
		if want == nil {
			continue
		}
		for j, in := range c.Strs {
			t.Run(fmt.Sprintf("%03d-%d", i, j),
				singleParse(p, in, want))
		}
	}
}

var (
	hasBash50  bool
	hasDash059 bool
	hasMksh57  bool
)

func TestMain(m *testing.M) {
	os.Setenv("LANGUAGE", "en_US.UTF8")
	os.Setenv("LC_ALL", "en_US.UTF8")
	hasBash50 = cmdContains("version 5.0", "bash", "--version")
	// dash provides no way to check its version, so we have to
	// check if it's new enough as to not have the bug that breaks
	// our integration tests. Blergh.
	hasDash059 = cmdContains("Bad subst", "dash", "-c", "echo ${#<}")
	hasMksh57 = cmdContains(" R57 ", "mksh", "-c", "echo $KSH_VERSION")
	os.Exit(m.Run())
}

func cmdContains(substr string, cmd string, args ...string) bool {
	out, err := exec.Command(cmd, args...).CombinedOutput()
	got := string(out)
	if err != nil {
		got += "\n" + err.Error()
	}
	return strings.Contains(got, substr)
}

var extGlobRe = regexp.MustCompile(`[@?*+!]\(`)

func confirmParse(in, cmd string, wantErr bool) func(*testing.T) {
	return func(t *testing.T) {
		t.Parallel()
		var opts []string
		if cmd == "bash" && extGlobRe.MatchString(in) {
			// otherwise bash refuses to parse these
			// properly. Also avoid -n since that too makes
			// bash bail.
			in = "shopt -s extglob\n" + in
		} else if !wantErr {
			// -n makes bash accept invalid inputs like
			// "let" or "`{`", so only use it in
			// non-erroring tests. Should be safe to not use
			// -n anyway since these are supposed to just
			// fail.
			// also, -n will break if we are using extglob
			// as extglob is not actually applied.
			opts = append(opts, "-n")
		}
		cmd := exec.Command(cmd, opts...)
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
		if wantErr && err == nil {
			t.Fatalf("Expected error in `%s` of %q, found none", strings.Join(cmd.Args, " "), in)
		} else if !wantErr && err != nil {
			t.Fatalf("Unexpected error in `%s` of %q: %v", strings.Join(cmd.Args, " "), in, err)
		}
	}
}

func TestParseBashConfirm(t *testing.T) {
	if testing.Short() {
		t.Skip("calling bash is slow.")
	}
	if !hasBash50 {
		t.Skip("bash 5.0 required to run")
	}
	i := 0
	for _, c := range append(fileTests, fileTestsNoPrint...) {
		if c.Bash == nil {
			continue
		}
		for j, in := range c.Strs {
			t.Run(fmt.Sprintf("%03d-%d", i, j),
				confirmParse(in, "bash", false))
		}
		i++
	}
}

func TestParsePosixConfirm(t *testing.T) {
	if testing.Short() {
		t.Skip("calling dash is slow.")
	}
	if !hasDash059 {
		t.Skip("dash 0.5.9 or newer required to run")
	}
	i := 0
	for _, c := range append(fileTests, fileTestsNoPrint...) {
		if c.Posix == nil {
			continue
		}
		for j, in := range c.Strs {
			t.Run(fmt.Sprintf("%03d-%d", i, j),
				confirmParse(in, "dash", false))
		}
		i++
	}
}

func TestParseMirBSDKornConfirm(t *testing.T) {
	if testing.Short() {
		t.Skip("calling mksh is slow.")
	}
	if !hasMksh57 {
		t.Skip("mksh 57 required to run")
	}
	i := 0
	for _, c := range append(fileTests, fileTestsNoPrint...) {
		if c.MirBSDKorn == nil {
			continue
		}
		for j, in := range c.Strs {
			t.Run(fmt.Sprintf("%03d-%d", i, j),
				confirmParse(in, "mksh", false))
		}
		i++
	}
}

func TestParseErrBashConfirm(t *testing.T) {
	if testing.Short() {
		t.Skip("calling bash is slow.")
	}
	if !hasBash50 {
		t.Skip("bash 5.0 required to run")
	}
	i := 0
	for _, c := range shellTests {
		want := c.common
		if c.bsmk != nil {
			want = c.bsmk
		}
		if c.bash != nil {
			want = c.bash
		}
		if want == nil {
			continue
		}
		wantErr := !strings.Contains(want.(string), " #NOERR")
		t.Run(fmt.Sprintf("%03d", i), confirmParse(c.in, "bash", wantErr))
		i++
	}
}

func TestParseErrPosixConfirm(t *testing.T) {
	if testing.Short() {
		t.Skip("calling dash is slow.")
	}
	if !hasDash059 {
		t.Skip("dash 0.5.9 or newer required to run")
	}
	i := 0
	for _, c := range shellTests {
		want := c.common
		if c.posix != nil {
			want = c.posix
		}
		if want == nil {
			continue
		}
		wantErr := !strings.Contains(want.(string), " #NOERR")
		t.Run(fmt.Sprintf("%03d", i), confirmParse(c.in, "dash", wantErr))
		i++
	}
}

func TestParseErrMirBSDKornConfirm(t *testing.T) {
	if testing.Short() {
		t.Skip("calling mksh is slow.")
	}
	if !hasMksh57 {
		t.Skip("mksh 57 required to run")
	}
	i := 0
	for _, c := range shellTests {
		want := c.common
		if c.bsmk != nil {
			want = c.bsmk
		}
		if c.mksh != nil {
			want = c.mksh
		}
		if want == nil {
			continue
		}
		wantErr := !strings.Contains(want.(string), " #NOERR")
		t.Run(fmt.Sprintf("%03d", i), confirmParse(c.in, "mksh", wantErr))
		i++
	}
}

func singleParse(p *Parser, in string, want *File) func(t *testing.T) {
	return func(t *testing.T) {
		t.Helper()
		got, err := p.Parse(newStrictReader(in), "")
		if err != nil {
			t.Fatalf("Unexpected error in %q: %v", in, err)
		}
		clearPosRecurse(t, in, got)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("syntax tree mismatch in %q\ndiff:\n%s", in,
				strings.Join(pretty.Diff(want, got), "\n"))
		}
	}
}

func BenchmarkParse(b *testing.B) {
	b.ReportAllocs()
	src := "" +
		strings.Repeat("\n\n\t\t        \n", 10) +
		"# " + strings.Repeat("foo bar ", 10) + "\n" +
		strings.Repeat("longlit_", 10) + "\n" +
		"'" + strings.Repeat("foo bar ", 10) + "'\n" +
		`"` + strings.Repeat("foo bar ", 10) + `"` + "\n" +
		strings.Repeat("aa bb cc dd; ", 6) +
		"a() { (b); { c; }; }; $(d; `e`)\n" +
		"foo=bar; a=b; c=d$foo${bar}e $simple ${complex:-default}\n" +
		"if a; then while b; do for c in d e; do f; done; done; fi\n" +
		"a | b && c || d | e && g || f\n" +
		"foo >a <b <<<c 2>&1 <<EOF\n" +
		strings.Repeat("somewhat long heredoc line\n", 10) +
		"EOF" +
		""
	p := NewParser(KeepComments(true))
	in := strings.NewReader(src)
	for i := 0; i < b.N; i++ {
		if _, err := p.Parse(in, ""); err != nil {
			b.Fatal(err)
		}
		in.Reset(src)
	}
}

type errorCase struct {
	in          string
	common      interface{}
	bash, posix interface{}
	bsmk, mksh  interface{}
}

var shellTests = []errorCase{
	{
		in:     "echo \x80",
		common: `1:6: invalid UTF-8 encoding #NOERR common shells use bytes`,
	},
	{
		in:     "\necho \x80",
		common: `2:6: invalid UTF-8 encoding #NOERR common shells use bytes`,
	},
	{
		in:     "echo foo\x80bar",
		common: `1:9: invalid UTF-8 encoding #NOERR common shells use bytes`,
	},
	{
		in:     "echo foo\xc3",
		common: `1:9: invalid UTF-8 encoding #NOERR common shells use bytes`,
	},
	{
		in:     "#foo\xc3",
		common: `1:5: invalid UTF-8 encoding #NOERR common shells use bytes`,
	},
	{
		in:     "echo a\x80",
		common: `1:7: invalid UTF-8 encoding #NOERR common shells use bytes`,
	},
	{
		in:     "<<$\xc8\n$\xc8",
		common: `1:4: invalid UTF-8 encoding #NOERR common shells use bytes`,
	},
	{
		in:     "echo $((foo\x80bar",
		common: `1:12: invalid UTF-8 encoding`,
	},
	{
		in:   "z=($\\\n#\\\n\\\n$#\x91\\\n",
		bash: `4:3: invalid UTF-8 encoding`,
	},
	{
		in:   `((# 1 + 2))`,
		bash: `1:1: unsigned expressions are a mksh feature`,
	},
	{
		in:    `$((# 1 + 2))`,
		posix: `1:1: unsigned expressions are a mksh feature`,
		bash:  `1:1: unsigned expressions are a mksh feature`,
	},
	{
		in:    `${ foo;}`,
		posix: `1:1: "${ stmts;}" is a mksh feature`,
		bash:  `1:1: "${ stmts;}" is a mksh feature`,
	},
	{
		in:   `${ `,
		mksh: `1:1: reached EOF without matching ${ with }`,
	},
	{
		in:   `${ foo;`,
		mksh: `1:1: reached EOF without matching ${ with }`,
	},
	{
		in:   `${ foo }`,
		mksh: `1:1: reached EOF without matching ${ with }`,
	},
	{
		in:    `${|foo;}`,
		posix: `1:1: "${|stmts;}" is a mksh feature`,
		bash:  `1:1: "${|stmts;}" is a mksh feature`,
	},
	{
		in:   `${|`,
		mksh: `1:1: reached EOF without matching ${ with }`,
	},
	{
		in:   `${|foo;`,
		mksh: `1:1: reached EOF without matching ${ with }`,
	},
	{
		in:   `${|foo }`,
		mksh: `1:1: reached EOF without matching ${ with }`,
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
		in:     "${a\x80",
		common: `1:4: invalid UTF-8 encoding`,
	},
	{
		in:     "${a#\x80",
		common: `1:5: invalid UTF-8 encoding`,
	},
	{
		in:     "${a-'\x80",
		common: `1:6: invalid UTF-8 encoding`,
	},
	{
		in:     "echo $((a |\x80",
		common: `1:12: invalid UTF-8 encoding`,
	},
	{
		in:     "!",
		common: `1:1: "!" cannot form a statement alone`,
	},
	{
		// bash allows lone '!', unlike dash, mksh, and us.
		in:     "! !",
		common: `1:1: cannot negate a command multiple times`,
		bash:   `1:1: cannot negate a command multiple times #NOERR`,
	},
	{
		in:     "! ! foo",
		common: `1:1: cannot negate a command multiple times #NOERR`,
		posix:  `1:1: cannot negate a command multiple times`,
	},
	{
		in:     "}",
		common: `1:1: "}" can only be used to close a block`,
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
		in:     "a=b { foo; }",
		common: `1:12: "}" can only be used to close a block`,
	},
	{
		in:     "a=b foo() { bar; }",
		common: `1:8: a command can only contain words and redirects`,
	},
	{
		in:     "a=b if foo; then bar; fi",
		common: `1:13: "then" can only be used in an if`,
	},
	{
		in:     ">f { foo; }",
		common: `1:11: "}" can only be used to close a block`,
	},
	{
		in:     ">f foo() { bar; }",
		common: `1:7: a command can only contain words and redirects`,
	},
	{
		in:     ">f if foo; then bar; fi",
		common: `1:12: "then" can only be used in an if`,
	},
	{
		in:     "if done; then b; fi",
		common: `1:4: "done" can only be used to end a loop`,
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
		in:     `"foo"(){ :; }`,
		common: `1:1: invalid func name`,
		mksh:   `1:1: invalid func name #NOERR`,
	},
	{
		in:     `foo$bar(){ :; }`,
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
		mksh:   `1:1: "foo()" must be followed by a statement #NOERR`,
	},
	{
		in:     "foo() {",
		common: `1:7: reached EOF without matching { with }`,
	},
	{
		in:    "foo-bar() { x; }",
		posix: `1:1: invalid func name`,
	},
	{
		in:    "foò() { x; }",
		posix: `1:1: invalid func name`,
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
		in:     "echo | #bar",
		common: `1:6: | must be followed by a statement`,
	},
	{
		in:     "echo && #bar",
		common: `1:6: && must be followed by a statement`,
	},
	{
		in:     "`echo &&`",
		common: `1:7: && must be followed by a statement`,
	},
	{
		in:     "`echo |`",
		common: `1:7: | must be followed by a statement`,
	},
	{
		in:     "echo | ! bar",
		common: `1:8: "!" can only be used in full statements`,
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
		common: `1:6: < must be followed by a word`,
	},
	{
		in:     "echo | < #bar",
		common: `1:8: < must be followed by a word`,
	},
	{
		in:     "echo && > #",
		common: `1:9: > must be followed by a word`,
	},
	{
		in:     "<<",
		common: `1:1: << must be followed by a word`,
	},
	{
		in:     "<<EOF",
		common: `1:1: unclosed here-document 'EOF' #NOERR`,
		mksh:   `1:1: unclosed here-document 'EOF'`,
	},
	{
		in:     "<<EOF\n\\",
		common: `1:1: unclosed here-document 'EOF' #NOERR`,
		mksh:   `1:1: unclosed here-document 'EOF'`,
	},
	{
		in:     "<<EOF\n\\\n",
		common: `1:1: unclosed here-document 'EOF' #NOERR`,
		mksh:   `1:1: unclosed here-document 'EOF'`,
	},
	{
		in:     "<<'EOF'\n\\\n",
		common: `1:1: unclosed here-document 'EOF' #NOERR`,
		mksh:   `1:1: unclosed here-document 'EOF'`,
	},
	{
		in:     "<<EOF <`\n#\n`\n``",
		common: `1:1: unclosed here-document 'EOF'`,
		mksh:   `1:1: unclosed here-document 'EOF'`,
	},
	{
		in:     "<<'EOF'",
		common: `1:1: unclosed here-document 'EOF' #NOERR`,
		mksh:   `1:1: unclosed here-document 'EOF'`,
	},
	{
		in:     "<<\\EOF",
		common: `1:1: unclosed here-document 'EOF' #NOERR`,
		mksh:   `1:1: unclosed here-document 'EOF'`,
	},
	{
		in:     "<<\\\\EOF",
		common: `1:1: unclosed here-document '\EOF' #NOERR`,
		mksh:   `1:1: unclosed here-document '\EOF'`,
	},
	{
		in:     "<<-EOF",
		common: `1:1: unclosed here-document 'EOF' #NOERR`,
		mksh:   `1:1: unclosed here-document 'EOF'`,
	},
	{
		in:     "<<-EOF\n\t",
		common: `1:1: unclosed here-document 'EOF' #NOERR`,
		mksh:   `1:1: unclosed here-document 'EOF'`,
	},
	{
		in:     "<<-'EOF'\n\t",
		common: `1:1: unclosed here-document 'EOF' #NOERR`,
		mksh:   `1:1: unclosed here-document 'EOF'`,
	},
	{
		in:     "<<\nEOF\nbar\nEOF",
		common: `1:1: << must be followed by a word`,
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
		common: `1:1: "for foo" must be followed by "in", "do", ;, or a newline`,
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
		common: `1:1: "for foo [in words]" must be followed by "do"`,
	},
	{
		in:     "for i in 1 2 (",
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
		common: `1:1: "for foo" must be followed by "in", "do", ;, or a newline`,
	},
	{
		in:   "select",
		bsmk: `1:1: "select" must be followed by a literal`,
	},
	{
		in:   "select i",
		bsmk: `1:1: "select foo" must be followed by "in", "do", ;, or a newline`,
	},
	{
		in:   "select i in;",
		bsmk: `1:1: "select foo [in words]" must be followed by "do"`,
	},
	{
		in:   "select i in 1 2 3;",
		bsmk: `1:1: "select foo [in words]" must be followed by "do"`,
	},
	{
		in:   "select i in 1 2 3; do echo $i;",
		bsmk: `1:1: select statement must end with "done"`,
	},
	{
		in:   "select i in 1 2 3; echo $i;",
		bsmk: `1:1: "select foo [in words]" must be followed by "do"`,
	},
	{
		in:   "select 'i' in 1 2 3; do echo $i; done",
		bsmk: `1:1: "select" must be followed by a literal`,
	},
	{
		in:   "select in 1 2 3; do echo $i; done",
		bsmk: `1:1: "select foo" must be followed by "in", "do", ;, or a newline`,
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
		in:     `echo $(($(a"`,
		common: `1:12: reached EOF without closing quote "`,
	},
	{
		in:     "echo $((`echo 0`",
		common: `1:6: reached EOF without matching $(( with ))`,
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
		in:     "echo $(())",
		common: `1:6: $(( must be followed by an expression #NOERR`,
	},
	{
		in:     "echo $((()))",
		common: `1:9: ( must be followed by an expression`,
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
		in:   "echo $((foo) )",
		bsmk: `1:6: reached ) without matching $(( with )) #NOERR`,
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
		mksh:   `1:12: += must follow a name #NOERR`,
	},
	{
		in:     "echo $((1=2))",
		common: `1:10: = must follow a name`,
	},
	{
		in:     "echo $(($0=2))",
		common: `1:11: = must follow a name #NOERR`,
	},
	{
		in:     "echo $(($(a)=2))",
		common: `1:13: = must follow a name #NOERR`,
	},
	{
		in:     "echo $((1'2'))",
		common: `1:10: not a valid arithmetic operator: '`,
	},
	{
		in:     "<<EOF\n$(()a",
		common: `2:1: $(( must be followed by an expression`,
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
		common: `1:13: parameter expansion requires a literal`,
	},
	{
		in:     "echo ${à}",
		common: `1:8: invalid parameter name`,
	},
	{
		in:     "echo ${1a}",
		common: `1:8: invalid parameter name`,
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
		common: `1:15: not a valid parameter expansion operator: "`,
	},
	{
		in:     "echo ${%",
		common: `1:6: "${%foo}" is a mksh feature`,
		mksh:   `1:8: parameter expansion requires a literal`,
	},
	{
		in:     "echo ${##",
		common: `1:6: reached EOF without matching ${ with }`,
	},
	{
		in:     "echo ${#<}",
		common: `1:9: parameter expansion requires a literal`,
	},
	{
		in:   "echo ${%<}",
		mksh: `1:9: parameter expansion requires a literal`,
	},
	{
		in:   "echo ${!<}",
		bsmk: `1:9: parameter expansion requires a literal`,
	},
	{
		in:     "echo ${@foo}",
		common: `1:9: @ cannot be followed by a word`,
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
		in:   "echo ${@[@]} ${@[*]}",
		bsmk: `1:9: cannot index a special parameter name`,
	},
	{
		in:   "echo ${*[@]} ${*[*]}",
		bsmk: `1:9: cannot index a special parameter name`,
	},
	{
		in:   "echo ${#[x]}",
		bsmk: `1:9: cannot index a special parameter name`,
	},
	{
		in:   "echo ${$[0]}",
		bsmk: `1:9: cannot index a special parameter name`,
	},
	{
		in:   "echo ${?[@]}",
		bsmk: `1:9: cannot index a special parameter name`,
	},
	{
		in:   "echo ${2[@]}",
		bsmk: `1:9: cannot index a special parameter name`,
	},
	{
		in:   "echo ${foo*}",
		bsmk: `1:11: not a valid parameter expansion operator: *`,
	},
	{
		in:   "echo ${foo;}",
		bsmk: `1:11: not a valid parameter expansion operator: ;`,
	},
	{
		in:   "echo ${foo!}",
		bsmk: `1:11: not a valid parameter expansion operator: !`,
	},
	{
		in:   "echo ${#foo:-bar}",
		bsmk: `1:12: cannot combine multiple parameter expansion operators`,
	},
	{
		in:   "echo ${%foo:1:3}",
		mksh: `1:12: cannot combine multiple parameter expansion operators`,
	},
	{
		in:   "echo ${#foo%x}",
		mksh: `1:12: cannot combine multiple parameter expansion operators`,
	},
	{
		in:     "echo foo\n;",
		common: `2:1: ; can only immediately follow a statement`,
	},
	{
		in:   "<<$ <<0\n$(<<$<<",
		bsmk: `2:6: << must be followed by a word`,
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
		in:     "case i {",
		common: `1:1: "case i {" is a mksh feature`,
		mksh:   `1:1: case statement must end with "}"`,
	},
	{
		in:   "case i { x) y ;;",
		mksh: `1:1: case statement must end with "}"`,
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
		in:     "`\\```",
		common: "1:2: reached EOF without closing quote `",
	},
	{
		in:     "`{\n`",
		common: "1:2: reached ` without matching { with }",
	},
	{
		in:    "echo \"`)`\"",
		bsmk:  `1:8: ) can only be used to close a subshell`,
		posix: `1:8: ) can only be used to close a subshell #NOERR dash bug`,
	},
	{
		in:     "<<$bar\n$bar",
		common: `1:3: expansions not allowed in heredoc words #NOERR`,
	},
	{
		in:     "<<${bar}\n${bar}",
		common: `1:3: expansions not allowed in heredoc words #NOERR`,
	},
	{
		in:    "<<$(bar)\n$",
		bsmk:  `1:3: expansions not allowed in heredoc words #NOERR`,
		posix: `1:3: expansions not allowed in heredoc words`,
	},
	{
		in:     "<<$-\n$-",
		common: `1:3: expansions not allowed in heredoc words #NOERR`,
	},
	{
		in:     "<<`bar`\n`bar`",
		common: `1:3: expansions not allowed in heredoc words #NOERR`,
	},
	{
		in:     "<<\"$bar\"\n$bar",
		common: `1:4: expansions not allowed in heredoc words #NOERR`,
	},
	{
		in:     "<<a <<0\n$(<<$<<",
		common: `2:6: << must be followed by a word`,
	},
	{
		in:     `""()`,
		common: `1:1: invalid func name`,
		mksh:   `1:1: invalid func name #NOERR`,
	},
	{
		// bash errors on the empty condition here, this is to
		// add coverage for empty statement lists
		in:     `if; then bar; fi; ;`,
		common: `1:19: ; can only immediately follow a statement`,
	},
	{
		in:    "]] )",
		bsmk:  `1:1: "]]" can only be used to close a test`,
		posix: `1:4: a command can only contain words and redirects`,
	},
	{
		in:    "((foo",
		bsmk:  `1:1: reached EOF without matching (( with ))`,
		posix: `1:2: reached EOF without matching ( with )`,
	},
	{
		in:   "(())",
		bsmk: `1:1: (( must be followed by an expression`,
	},
	{
		in:    "echo ((foo",
		bsmk:  `1:6: (( can only be used to open an arithmetic cmd`,
		posix: `1:1: "foo(" must be followed by )`,
	},
	{
		in:    "echo |&",
		bash:  `1:6: |& must be followed by a statement`,
		posix: `1:6: | must be followed by a statement`,
	},
	{
		in:   "|& a",
		bsmk: `1:1: |& is not a valid start for a statement`,
	},
	{
		in:    "foo |& bar",
		posix: `1:5: | must be followed by a statement`,
	},
	{
		in:   "let",
		bsmk: `1:1: "let" must be followed by an expression`,
	},
	{
		in:   "let a+ b",
		bsmk: `1:6: + must be followed by an expression`,
	},
	{
		in:   "let + a",
		bsmk: `1:5: + must be followed by an expression`,
	},
	{
		in:   "let a ++",
		bsmk: `1:7: ++ must be followed by a literal`,
	},
	{
		in:   "let (a)++",
		bsmk: `1:8: ++ must follow a name`,
	},
	{
		in:   "let 1++",
		bsmk: `1:6: ++ must follow a name`,
	},
	{
		in:   "let $0++",
		bsmk: `1:7: ++ must follow a name`,
	},
	{
		in:   "let --(a)",
		bsmk: `1:5: -- must be followed by a literal`,
	},
	{
		in:   "let --$a",
		bsmk: `1:5: -- must be followed by a literal`,
	},
	{
		in:   "let a+\n",
		bsmk: `1:6: + must be followed by an expression`,
	},
	{
		in:   "let ))",
		bsmk: `1:1: "let" must be followed by an expression`,
	},
	{
		in:   "`let !`",
		bsmk: `1:6: ! must be followed by an expression`,
	},
	{
		in:   "let a:b",
		bsmk: `1:5: ternary operator missing ? before :`,
	},
	{
		in:   "let a+b=c",
		bsmk: `1:8: = must follow a name`,
	},
	{
		in:   "`let` { foo; }",
		bsmk: `1:2: "let" must be followed by an expression`,
	},
	{
		in:   "[[",
		bsmk: `1:1: test clause requires at least one expression`,
	},
	{
		in:   "[[ ]]",
		bsmk: `1:1: test clause requires at least one expression`,
	},
	{
		in:   "[[ a",
		bsmk: `1:1: reached EOF without matching [[ with ]]`,
	},
	{
		in:   "[[ a ||",
		bsmk: `1:6: || must be followed by an expression`,
	},
	{
		in:   "[[ a ==",
		bsmk: `1:6: == must be followed by a word`,
	},
	{
		in:   "[[ a =~",
		bash: `1:6: =~ must be followed by a word`,
		mksh: `1:6: regex tests are a bash feature`,
	},
	{
		in:   "[[ -f a",
		bsmk: `1:1: reached EOF without matching [[ with ]]`,
	},
	{
		in:   "[[ -n\na ]]",
		bsmk: `1:4: -n must be followed by a word`,
	},
	{
		in:   "[[ a -nt b",
		bsmk: `1:1: reached EOF without matching [[ with ]]`,
	},
	{
		in:   "[[ a =~ b",
		bash: `1:1: reached EOF without matching [[ with ]]`,
	},
	{
		in:   "[[ a b c ]]",
		bsmk: `1:6: not a valid test operator: b`,
	},
	{
		in:   "[[ a b$x c ]]",
		bsmk: `1:6: test operator words must consist of a single literal`,
	},
	{
		in:   "[[ a & b ]]",
		bsmk: `1:6: not a valid test operator: &`,
	},
	{
		in:   "[[ true && () ]]",
		bsmk: `1:12: ( must be followed by an expression`,
	},
	{
		in:   "[[ a == ! b ]]",
		bsmk: `1:11: not a valid test operator: b`,
	},
	{
		in:   "[[ (! ) ]]",
		bsmk: `1:5: ! must be followed by an expression`,
	},
	{
		in:   "[[ (-e ) ]]",
		bsmk: `1:5: -e must be followed by a word`,
	},
	{
		in:   "[[ (a) == b ]]",
		bsmk: `1:8: expected &&, || or ]] after complex expr`,
	},
	{
		in:   "[[ a =~ ; ]]",
		bash: `1:6: =~ must be followed by a word`,
	},
	{
		in:   "[[ a =~ )",
		bash: `1:6: =~ must be followed by a word`,
	},
	{
		in:   "[[ a =~ ())",
		bash: `1:1: reached ) without matching [[ with ]]`,
	},
	{
		in:   "[[ >",
		bsmk: `1:1: [[ must be followed by a word`,
	},
	{
		in:   "local (",
		bash: `1:7: "local" must be followed by names or assignments`,
	},
	{
		in:   "declare 0=${o})",
		bash: `1:9: invalid var name`,
	},
	{
		in:   "a=(<)",
		bsmk: `1:4: array element values must be words`,
	},
	{
		in:   "a=([)",
		bash: `1:4: [ must be followed by an expression`,
	},
	{
		in:   "a=([i)",
		bash: `1:4: reached ) without matching [ with ]`,
	},
	{
		in:   "a=([i])",
		bash: `1:4: "[x]" must be followed by = #NOERR`,
	},
	{
		in:   "a[i]=(y)",
		bash: `1:6: arrays cannot be nested`,
	},
	{
		in:   "a=([i]=(y))",
		bash: `1:8: arrays cannot be nested`,
	},
	{
		in:   "o=([0]=#",
		bash: `1:8: array element values must be words`,
	},
	{
		in:   "a[b] ==[",
		bash: `1:1: "a[b]" must be followed by = #NOERR stringifies`,
	},
	{
		in:   "a[b] +=c",
		bash: `1:1: "a[b]" must be followed by = #NOERR stringifies`,
	},
	{
		in:   "a=(x y) foo",
		bash: `1:1: inline variables cannot be arrays #NOERR stringifies`,
	},
	{
		in:   "a[2]=x foo",
		bash: `1:1: inline variables cannot be arrays #NOERR stringifies`,
	},
	{
		in:   "function",
		bsmk: `1:1: "function" must be followed by a name`,
	},
	{
		in:   "function foo(",
		bsmk: `1:10: "foo(" must be followed by )`,
	},
	{
		in:   "function `function",
		bsmk: `1:1: "function" must be followed by a name`,
	},
	{
		in:   `function "foo"(){}`,
		bsmk: `1:1: "function" must be followed by a name`,
	},
	{
		in:   "function foo()",
		bsmk: `1:1: "foo()" must be followed by a statement`,
	},
	{
		in:   "echo <<<",
		bsmk: `1:6: <<< must be followed by a word`,
	},
	{
		in:   "a[",
		bsmk: `1:2: [ must be followed by an expression`,
	},
	{
		in:   "a[b",
		bsmk: `1:2: reached EOF without matching [ with ]`,
	},
	{
		in:   "a[]",
		bsmk: `1:2: [ must be followed by an expression #NOERR is cmd`,
	},
	{
		in:   "a[[",
		bsmk: `1:3: [ must follow a name`,
	},
	{
		in:   "echo $((a[))",
		bsmk: `1:10: [ must be followed by an expression`,
	},
	{
		in:   "echo $((a[b))",
		bsmk: `1:10: reached ) without matching [ with ]`,
	},
	{
		in:   "echo $((a[]))",
		bash: `1:10: [ must be followed by an expression`,
		mksh: `1:10: [ must be followed by an expression #NOERR wrong?`,
	},
	{
		in:   "echo $((x$t[",
		bsmk: `1:12: [ must follow a name`,
	},
	{
		in:   "a[1]",
		bsmk: `1:1: "a[b]" must be followed by = #NOERR is cmd`,
	},
	{
		in:   "a[i]+",
		bsmk: `1:1: "a[b]+" must be followed by = #NOERR is cmd`,
	},
	{
		in:   "a[1]#",
		bsmk: `1:1: "a[b]" must be followed by = #NOERR is cmd`,
	},
	{
		in:   "echo $[foo",
		bash: `1:6: reached EOF without matching $[ with ]`,
	},
	{
		in:   "echo $'",
		bsmk: `1:6: reached EOF without closing quote '`,
	},
	{
		in:   `echo $"`,
		bsmk: `1:6: reached EOF without closing quote "`,
	},
	{
		in:   "echo @(",
		bsmk: `1:6: reached EOF without matching @( with )`,
	},
	{
		in:   "echo @(a",
		bsmk: `1:6: reached EOF without matching @( with )`,
	},
	{
		in:   "((@(",
		bsmk: `1:1: reached ( without matching (( with ))`,
	},
	{
		in:   "time {",
		bsmk: `1:6: reached EOF without matching { with }`,
	},
	{
		in:   "time ! foo",
		bash: `1:6: "!" can only be used in full statements #NOERR wrong`,
		mksh: `1:6: "!" can only be used in full statements`,
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
		bash: `1:16: "declare" must be followed by names or assignments`,
	},
	{
		in:   "echo ${foo[1 2]}",
		bsmk: `1:14: not a valid arithmetic operator: 2`,
	},
	{
		in:   "echo ${foo[}",
		bsmk: `1:11: [ must be followed by an expression`,
	},
	{
		in:   "echo ${foo]}",
		bsmk: `1:11: not a valid parameter expansion operator: ]`,
	},
	{
		in:   "echo ${foo[]}",
		bash: `1:11: [ must be followed by an expression`,
		mksh: `1:11: [ must be followed by an expression #NOERR wrong?`,
	},
	{
		in:   "echo ${a/\n",
		bsmk: `1:6: reached EOF without matching ${ with }`,
	},
	{
		in:   "echo ${a-\n",
		bsmk: `1:6: reached EOF without matching ${ with }`,
	},
	{
		in:   "echo ${foo:",
		bsmk: `1:11: : must be followed by an expression`,
	},
	{
		in:   "echo ${foo:1 2}",
		bsmk: `1:14: not a valid arithmetic operator: 2 #NOERR lazy eval`,
	},
	{
		in:   "echo ${foo:1",
		bsmk: `1:6: reached EOF without matching ${ with }`,
	},
	{
		in:   "echo ${foo:1:",
		bsmk: `1:13: : must be followed by an expression`,
	},
	{
		in:   "echo ${foo:1:2",
		bsmk: `1:6: reached EOF without matching ${ with }`,
	},
	{
		in:   "echo ${foo,",
		bash: `1:6: reached EOF without matching ${ with }`,
	},
	{
		in:   "echo ${foo@",
		bash: `1:11: @ expansion operator requires a literal`,
	},
	{
		in:   "echo ${foo@}",
		bash: `1:12: @ expansion operator requires a literal #NOERR empty string fallback`,
	},
	{
		in:   "echo ${foo@Q",
		bash: `1:6: reached EOF without matching ${ with }`,
	},
	{
		in:   "echo ${foo@bar}",
		bash: `1:12: invalid @ expansion operator #NOERR at runtime`,
	},
	{
		in:   "echo ${foo@'Q'}",
		bash: `1:12: @ expansion operator requires a literal #NOERR at runtime`,
	},
	{
		in:   `echo $((echo a); (echo b))`,
		bsmk: `1:14: not a valid arithmetic operator: a #NOERR backtrack`,
	},
	{
		in:   `((echo a); (echo b))`,
		bsmk: `1:8: not a valid arithmetic operator: a #NOERR backtrack`,
	},
	{
		in:   "for ((;;",
		bash: `1:5: reached EOF without matching (( with ))`,
	},
	{
		in:   "for ((;;0000000",
		bash: `1:5: reached EOF without matching (( with ))`,
	},
	{
		in:    "function foo() { bar; }",
		posix: `1:13: a command can only contain words and redirects`,
	},
	{
		in:    "echo <(",
		posix: `1:6: < must be followed by a word`,
		mksh:  `1:6: < must be followed by a word`,
	},
	{
		in:    "echo >(",
		posix: `1:6: > must be followed by a word`,
		mksh:  `1:6: > must be followed by a word`,
	},
	{
		// shells treat {var} as an argument, but we are a bit stricter
		// so that users won't think this will work like they expect in
		// POSIX shell.
		in:    "echo {var}>foo",
		posix: `1:6: {varname} redirects are a bash feature #NOERR`,
		mksh:  `1:6: {varname} redirects are a bash feature #NOERR`,
	},
	{
		in:    "echo ;&",
		posix: `1:7: & can only immediately follow a statement`,
		bsmk:  `1:6: ;& can only be used in a case clause`,
	},
	{
		in:    "echo ;;&",
		posix: `1:6: ;; can only be used in a case clause`,
		mksh:  `1:6: ;; can only be used in a case clause`,
	},
	{
		in:    "echo ;|",
		posix: `1:7: | can only immediately follow a statement`,
		bash:  `1:7: | can only immediately follow a statement`,
	},
	{
		in:    "for i in 1 2 3; { echo; }",
		posix: `1:17: for loops with braces are a bash/mksh feature`,
	},
	{
		in:    "for ((i=0; i<5; i++)); do echo; done",
		posix: `1:5: c-style fors are a bash feature`,
		mksh:  `1:5: c-style fors are a bash feature`,
	},
	{
		in:    "echo !(a)",
		posix: `1:6: extended globs are a bash/mksh feature`,
	},
	{
		in:    "echo $a@(b)",
		posix: `1:8: extended globs are a bash/mksh feature`,
	},
	{
		in:    "foo=(1 2)",
		posix: `1:5: arrays are a bash/mksh feature`,
	},
	{
		in:     "a=$c\n'",
		common: `2:1: reached EOF without closing quote '`,
	},
	{
		in:    "echo ${!foo}",
		posix: `1:8: ${!foo} is a bash/mksh feature`,
	},
	{
		in:    "echo ${foo[1]}",
		posix: `1:11: arrays are a bash/mksh feature`,
	},
	{
		in:    "echo ${foo/a/b}",
		posix: `1:11: search and replace is a bash/mksh feature`,
	},
	{
		in:    "echo ${foo:1}",
		posix: `1:11: slicing is a bash/mksh feature`,
	},
	{
		in:    "echo ${foo,bar}",
		posix: `1:11: this expansion operator is a bash feature`,
		mksh:  `1:11: this expansion operator is a bash feature`,
	},
	{
		in:    "echo ${foo@Q}",
		posix: `1:11: this expansion operator is a bash/mksh feature`,
	},
	{
		in:     "`\"`\\",
		common: "1:3: reached EOF without closing quote `",
	},
}

func checkError(p *Parser, in, want string) func(*testing.T) {
	return func(t *testing.T) {
		if i := strings.Index(want, " #NOERR"); i >= 0 {
			want = want[:i]
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
	p := NewParser(KeepComments(true), Variant(LangPOSIX))
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
	p := NewParser(KeepComments(true))
	i := 0
	for _, c := range shellTests {
		want := c.common
		if c.bsmk != nil {
			want = c.bsmk
		}
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

func TestParseErrMirBSDKorn(t *testing.T) {
	t.Parallel()
	p := NewParser(KeepComments(true), Variant(LangMirBSDKorn))
	i := 0
	for _, c := range shellTests {
		want := c.common
		if c.bsmk != nil {
			want = c.bsmk
		}
		if c.mksh != nil {
			want = c.mksh
		}
		if want == nil {
			continue
		}
		t.Run(fmt.Sprintf("%03d", i), checkError(p, c.in, want.(string)))
		i++
	}
}

func TestInputName(t *testing.T) {
	t.Parallel()
	in := "("
	want := "some-file.sh:1:1: reached EOF without matching ( with )"
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
	t.Parallel()
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

func TestParseStmts(t *testing.T) {
	t.Parallel()
	p := NewParser()
	inReader, inWriter := io.Pipe()
	recv := make(chan bool, 10)
	errc := make(chan error, 1)
	go func() {
		errc <- p.Stmts(inReader, func(s *Stmt) bool {
			recv <- true
			return true
		})
	}()
	io.WriteString(inWriter, "foo\n")
	<-recv
	io.WriteString(inWriter, "bar; baz")
	inWriter.Close()
	<-recv
	<-recv
	if err := <-errc; err != nil {
		t.Fatalf("Expected no error: %v", err)
	}
}

func TestParseStmtsStopEarly(t *testing.T) {
	t.Parallel()
	p := NewParser()
	inReader, inWriter := io.Pipe()
	defer inWriter.Close()
	recv := make(chan bool, 10)
	errc := make(chan error, 1)
	go func() {
		errc <- p.Stmts(inReader, func(s *Stmt) bool {
			recv <- true
			return !s.Background
		})
	}()
	io.WriteString(inWriter, "a\n")
	<-recv
	io.WriteString(inWriter, "b &\n") // stop here
	<-recv
	if err := <-errc; err != nil {
		t.Fatalf("Expected no error: %v", err)
	}
}

func TestParseStmtsError(t *testing.T) {
	t.Parallel()
	in := "foo; )"
	p := NewParser()
	recv := make(chan bool, 10)
	errc := make(chan error, 1)
	go func() {
		errc <- p.Stmts(strings.NewReader(in), func(s *Stmt) bool {
			recv <- true
			return true
		})
	}()
	<-recv
	if err := <-errc; err == nil {
		t.Fatalf("Expected an error in %q, but got nil", in)
	}
}

func TestParseWords(t *testing.T) {
	t.Parallel()
	p := NewParser()
	inReader, inWriter := io.Pipe()
	recv := make(chan bool, 10)
	errc := make(chan error, 1)
	go func() {
		errc <- p.Words(inReader, func(w *Word) bool {
			recv <- true
			return true
		})
	}()
	// TODO: Allow a single space to end parsing a word. At the moment, the
	// parser must read the next non-space token (the next literal or
	// newline, in this case) to finish parsing a word.
	io.WriteString(inWriter, "foo ")
	io.WriteString(inWriter, "bar\n")
	<-recv
	io.WriteString(inWriter, "baz etc")
	inWriter.Close()
	<-recv
	<-recv
	<-recv
	if err := <-errc; err != nil {
		t.Fatalf("Expected no error: %v", err)
	}
}

func TestParseWordsStopEarly(t *testing.T) {
	t.Parallel()
	p := NewParser()
	r := strings.NewReader("a\nb\nc\n")
	parsed := 0
	err := p.Words(r, func(w *Word) bool {
		parsed++
		return w.Lit() != "b"
	})
	if err != nil {
		t.Fatalf("Expected no error: %v", err)
	}
	if want := 2; parsed != want {
		t.Fatalf("wanted %d words parsed, got %d", want, parsed)
	}
}

func TestParseWordsError(t *testing.T) {
	t.Parallel()
	in := "foo )"
	p := NewParser()
	recv := make(chan bool, 10)
	errc := make(chan error, 1)
	go func() {
		errc <- p.Words(strings.NewReader(in), func(w *Word) bool {
			recv <- true
			return true
		})
	}()
	<-recv
	want := "1:5: ) is not a valid word"
	got := fmt.Sprintf("%v", <-errc)
	if got != want {
		t.Fatalf("Expected %q as an error, but got %q", want, got)
	}
}

var documentTests = []struct {
	in   string
	want []WordPart
}{
	{
		"foo",
		[]WordPart{lit("foo")},
	},
	{
		" foo  $bar",
		[]WordPart{
			lit(" foo  "),
			litParamExp("bar"),
		},
	},
	{
		"$bar\n\n",
		[]WordPart{
			litParamExp("bar"),
			lit("\n\n"),
		},
	},
}

func TestParseDocument(t *testing.T) {
	t.Parallel()
	p := NewParser()

	for i, tc := range documentTests {
		t.Run(fmt.Sprintf("%02d", i), func(t *testing.T) {
			got, err := p.Document(strings.NewReader(tc.in))
			if err != nil {
				t.Fatal(err)
			}
			clearPosRecurse(t, "", got)
			want := &Word{Parts: tc.want}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("syntax tree mismatch in %q\ndiff:\n%s", tc.in,
					strings.Join(pretty.Diff(want, got), "\n"))
			}
		})
	}
}

func TestParseDocumentError(t *testing.T) {
	t.Parallel()
	in := "foo $("
	p := NewParser()
	_, err := p.Document(strings.NewReader(in))
	want := "1:5: reached EOF without matching ( with )"
	got := fmt.Sprintf("%v", err)
	if got != want {
		t.Fatalf("Expected %q as an error, but got %q", want, got)
	}
}

var arithmeticTests = []struct {
	in   string
	want ArithmExpr
}{
	{
		"foo",
		litWord("foo"),
	},
	{
		"3 + 4",
		&BinaryArithm{
			Op: Add,
			X:  litWord("3"),
			Y:  litWord("4"),
		},
	},
}

func TestParseArithmetic(t *testing.T) {
	t.Parallel()
	p := NewParser()

	for i, tc := range arithmeticTests {
		t.Run(fmt.Sprintf("%02d", i), func(t *testing.T) {
			got, err := p.Arithmetic(strings.NewReader(tc.in))
			if err != nil {
				t.Fatal(err)
			}
			clearPosRecurse(t, "", got)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("syntax tree mismatch in %q\ndiff:\n%s", tc.in,
					strings.Join(pretty.Diff(tc.want, got), "\n"))
			}
		})
	}
}

func TestParseArithmeticError(t *testing.T) {
	t.Parallel()
	in := "3 +"
	p := NewParser()
	_, err := p.Arithmetic(strings.NewReader(in))
	want := "1:3: + must be followed by an expression"
	got := fmt.Sprintf("%v", err)
	if got != want {
		t.Fatalf("Expected %q as an error, but got %q", want, got)
	}
}

var stopAtTests = []struct {
	in   string
	stop string
	want interface{}
}{
	{
		"foo bar", "$$",
		litCall("foo", "bar"),
	},
	{
		"$foo $", "$$",
		call(word(litParamExp("foo")), litWord("$")),
	},
	{
		"echo foo $$", "$$",
		litCall("echo", "foo"),
	},
	{
		"$$", "$$",
		&File{},
	},
	{
		"echo foo\n$$\n", "$$",
		litCall("echo", "foo"),
	},
	{
		"echo foo; $$", "$$",
		litCall("echo", "foo"),
	},
	{
		"echo foo; $$", "$$",
		litCall("echo", "foo"),
	},
	{
		"echo foo;$$", "$$",
		litCall("echo", "foo"),
	},
	{
		"echo '$$'", "$$",
		call(litWord("echo"), word(sglQuoted("$$"))),
	},
}

func TestParseStmtsStopAt(t *testing.T) {
	t.Parallel()
	for i, c := range stopAtTests {
		p := NewParser(StopAt(c.stop))
		want := fullProg(c.want)
		t.Run(fmt.Sprintf("%02d", i), singleParse(p, c.in, want))
	}
}

func TestValidName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"Empty", "", false},
		{"Simple", "foo", true},
		{"MixedCase", "Foo", true},
		{"Underscore", "_foo", true},
		{"NumberPrefix", "3foo", false},
		{"NumberSuffix", "foo3", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ValidName(tc.in)
			if got != tc.want {
				t.Fatalf("ValidName(%q) got %t, wanted %t",
					tc.in, got, tc.want)
			}
		})
	}
}

func TestIsIncomplete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want bool
	}{
		{"foo\n", false},
		{"foo;", false},
		{"\n", false},
		{"'incomp", true},
		{"foo; 'incomp", true},
		{" (incomp", true},
		{"badsyntax)", false},
	}
	p := NewParser()
	for i, tc := range tests {
		t.Run(fmt.Sprintf("Parse%02d", i), func(t *testing.T) {
			r := strings.NewReader(tc.in)
			_, err := p.Parse(r, "")
			if got := IsIncomplete(err); got != tc.want {
				t.Fatalf("%q got %t, wanted %t", tc.in, got, tc.want)
			}
		})
		t.Run(fmt.Sprintf("Interactive%02d", i), func(t *testing.T) {
			r := strings.NewReader(tc.in)
			err := p.Interactive(r, func([]*Stmt) bool {
				return false
			})
			if got := IsIncomplete(err); got != tc.want {
				t.Fatalf("%q got %t, wanted %t", tc.in, got, tc.want)
			}
		})
	}
}
