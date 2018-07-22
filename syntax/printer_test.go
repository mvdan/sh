// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"testing"
)

func TestPrintCompact(t *testing.T) {
	t.Parallel()
	parserBash := NewParser(KeepComments)
	parserPosix := NewParser(KeepComments, Variant(LangPOSIX))
	parserMirBSD := NewParser(KeepComments, Variant(LangMirBSDKorn))
	printer := NewPrinter()
	for i, c := range fileTests {
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			in := c.Strs[0]
			parser := parserPosix
			if c.Bash != nil {
				parser = parserBash
			} else if c.MirBSDKorn != nil {
				parser = parserMirBSD
			}
			printTest(t, parser, printer, in, in)
		})
	}
}

func strPrint(p *Printer, node Node) (string, error) {
	var buf bytes.Buffer
	err := p.Print(&buf, node)
	return buf.String(), err
}

type printCase struct {
	in, want string
}

func samePrint(s string) printCase { return printCase{in: s, want: s} }

var printTests = []printCase{
	samePrint(`fo○ b\år`),
	samePrint(`"fo○ b\år"`),
	samePrint(`'fo○ b\år'`),
	samePrint(`${a#fo○ b\år}`),
	samePrint(`#fo○ b\år`),
	samePrint("<<EOF\nfo○ b\\år\nEOF"),
	samePrint(`$'○ b\år'`),
	samePrint("${a/b//○}"),
	// rune split by the chunking
	{strings.Repeat(" ", bufSize-1) + "○", "○"},
	// peekByte that would (but cannot) go to the next chunk
	{strings.Repeat(" ", bufSize-2) + ">(a)", ">(a)"},
	// escaped newline at end of chunk
	{"a" + strings.Repeat(" ", bufSize-2) + "\\\nb", "a \\\n\tb"},
	// panics if padding is only 4 (utf8.UTFMax)
	{strings.Repeat(" ", bufSize-10) + "${a/b//○}", "${a/b//○}"},
	// multiple p.fill calls
	{"a" + strings.Repeat(" ", bufSize*4) + "b", "a b"},
	// newline at the beginning of second chunk
	{"a" + strings.Repeat(" ", bufSize-2) + "\nb", "a\nb"},
	{"foo; bar", "foo\nbar"},
	{"foo\n\n\nbar", "foo\n\nbar"},
	{"foo\n\n", "foo"},
	{"\n\nfoo", "foo"},
	{"# foo \n # bar\t", "# foo\n# bar"},
	{"#", "#"},
	samePrint("a=b # inline\nbar"),
	samePrint("a=$(b) # inline"),
	samePrint("foo # inline\n# after"),
	samePrint("$(a) $(b)"),
	{"if a\nthen\n\tb\nfi", "if a; then\n\tb\nfi"},
	{"if a; then\nb\nelse\nfi", "if a; then\n\tb\nfi"},
	{"if a; then b\nelse c\nfi", "if a; then\n\tb\nelse\n\tc\nfi"},
	samePrint("foo >&2 <f bar"),
	samePrint("foo >&2 bar <f"),
	{"foo >&2 bar <f bar2", "foo >&2 bar bar2 <f"},
	{"foo <<EOF bar\nl1\nEOF", "foo bar <<EOF\nl1\nEOF"},
	samePrint("foo <<\\\\\\\\EOF\nbar\n\\\\EOF"),
	samePrint("foo <<\"\\EOF\"\nbar\n\\EOF"),
	samePrint("foo <<EOF && bar\nl1\nEOF"),
	samePrint("foo <<EOF &&\nl1\nEOF\n\tbar"),
	samePrint("foo <<EOF\nl1\nEOF\n\nfoo2"),
	samePrint("<<EOF\nEOF"),
	samePrint("foo <<EOF\nEOF\n\nbar"),
	samePrint("foo <<'EOF'\nEOF\n\nbar"),
	{
		"{ foo; bar; }",
		"{\n\tfoo\n\tbar\n}",
	},
	{
		"{ foo; bar; }\n#etc",
		"{\n\tfoo\n\tbar\n}\n#etc",
	},
	{
		"{\n\tfoo; }",
		"{\n\tfoo\n}",
	},
	{
		"{ foo\n}",
		"{\n\tfoo\n}",
	},
	{
		"(foo\n)",
		"(\n\tfoo\n)",
	},
	{
		"$(foo\n)",
		"$(\n\tfoo\n)",
	},
	{
		"a\n\n\n# etc\nb",
		"a\n\n# etc\nb",
	},
	{
		"a b\\\nc d",
		"a bc \\\n\td",
	},
	{
		"a bb\\\ncc d",
		"a bbcc \\\n\td",
	},
	samePrint("a \\\n\tb \\\n\tc \\\n\t;"),
	samePrint("a=1 \\\n\tb=2 \\\n\tc=3 \\\n\t;"),
	samePrint("if a \\\n\t; then b; fi"),
	samePrint("a 'b\nb' c"),
	samePrint("a $'b\nb' c"),
	{
		"(foo; bar)",
		"(\n\tfoo\n\tbar\n)",
	},
	{
		"{\nfoo\nbar; }",
		"{\n\tfoo\n\tbar\n}",
	},
	samePrint("\"$foo\"\n{\n\tbar\n}"),
	{
		"{\nbar\n# extra\n}",
		"{\n\tbar\n\t# extra\n}",
	},
	{
		"foo\nbar  # extra",
		"foo\nbar # extra",
	},
	{
		"foo # 1\nfooo # 2\nfo # 3",
		"foo  # 1\nfooo # 2\nfo   # 3",
	},
	{
		" foo # 1\n fooo # 2\n fo # 3",
		"foo  # 1\nfooo # 2\nfo   # 3",
	},
	{
		"foo   # 1\nfooo  # 2\nfo    # 3",
		"foo  # 1\nfooo # 2\nfo   # 3",
	},
	{
		"foooooa\nfoo # 1\nfooo # 2\nfo # 3\nfooooo",
		"foooooa\nfoo  # 1\nfooo # 2\nfo   # 3\nfooooo",
	},
	{
		"foo\nbar\nfoo # 1\nfooo # 2",
		"foo\nbar\nfoo  # 1\nfooo # 2",
	},
	samePrint("foobar # 1\nfoo\nfoo # 2"),
	samePrint("foobar # 1\n#foo\nfoo # 2"),
	samePrint("foobar # 1\n\nfoo # 2"),
	{
		"foo # 2\nfoo2 bar # 1",
		"foo      # 2\nfoo2 bar # 1",
	},
	{
		"foo bar # 1\n! foo # 2",
		"foo bar # 1\n! foo   # 2",
	},
	{
		"aa #b\nc  #d\ne\nf #g",
		"aa #b\nc  #d\ne\nf #g",
	},
	// TODO: reenable once we improve comment handling
	//{
	//	"{ a; } #x\nbbb #y\n{ #z\n}",
	//	"{ a; } #x\nbbb    #y\n{ #z\n}",
	//},
	{
		"foo; foooo # 1",
		"foo\nfoooo # 1",
	},
	{
		"aaa; b #1\nc #2",
		"aaa\nb #1\nc #2",
	},
	{
		"a #1\nbbb; c #2\nd #3",
		"a #1\nbbb\nc #2\nd #3",
	},
	samePrint("aa #c1\n{ #c2\n\tb\n}"),
	{
		"aa #c1\n{ b; c; } #c2",
		"aa #c1\n{\n\tb\n\tc\n} #c2",
	},
	samePrint("a #c1\n'b\ncc' #c2"),
	{
		"(\nbar\n# extra\n)",
		"(\n\tbar\n\t# extra\n)",
	},
	{
		"for a in 1 2\ndo\n\t# bar\ndone",
		"for a in 1 2; do\n\t# bar\ndone",
	},
	samePrint("#before\nfoo | bar"),
	samePrint("#before\nfoo && bar"),
	samePrint("foo | bar # inline"),
	samePrint("foo && bar # inline"),
	samePrint("for a in 1 2; do\n\n\tbar\ndone"),
	{
		"a \\\n\t&& b",
		"a &&\n\tb",
	},
	{
		"a \\\n\t&& b\nc",
		"a &&\n\tb\nc",
	},
	{
		"{\n(a \\\n&& b)\nc\n}",
		"{\n\t(a &&\n\t\tb)\n\tc\n}",
	},
	{
		"a && b \\\n&& c",
		"a && b &&\n\tc",
	},
	{
		"a \\\n&& $(b) && c \\\n&& d",
		"a &&\n\t$(b) && c &&\n\td",
	},
	{
		"a \\\n&& b\nc \\\n&& d",
		"a &&\n\tb\nc &&\n\td",
	},
	{
		"a \\\n&&\n#c\nb",
		"a &&\n\t#c\n\tb",
	},
	{
		"a | {\nb \\\n| c\n}",
		"a | {\n\tb |\n\t\tc\n}",
	},
	{
		"a \\\n\t&& if foo; then\nbar\nfi",
		"a &&\n\tif foo; then\n\t\tbar\n\tfi",
	},
	{
		"if\nfoo\nthen\nbar\nfi",
		"if\n\tfoo\nthen\n\tbar\nfi",
	},
	{
		"if foo \\\nbar\nthen\nbar\nfi",
		"if foo \\\n\tbar; then\n\tbar\nfi",
	},
	{
		"if foo \\\n&& bar\nthen\nbar\nfi",
		"if foo &&\n\tbar; then\n\tbar\nfi",
	},
	{
		"a |\nb |\nc",
		"a |\n\tb |\n\tc",
	},
	samePrint("a |\n\tb | c |\n\td"),
	samePrint("a | b |\n\tc |\n\td"),
	{
		"foo |\n# misplaced\nbar",
		"foo |\n\t# misplaced\n\tbar",
	},
	samePrint("{\n\tfoo\n\t#a\n\tbar\n} | etc"),
	{
		"foo &&\n#a1\n#a2\n$(bar)",
		"foo &&\n\t#a1\n\t#a2\n\t$(bar)",
	},
	{
		"{\n\tfoo\n\t#a\n} |\n# misplaced\nbar",
		"{\n\tfoo\n\t#a\n} |\n\t# misplaced\n\tbar",
	},
	samePrint("foo | bar\n#after"),
	{
		"a |\nb | #c2\nc",
		"a |\n\tb | #c2\n\tc",
	},
	{
		"{\nfoo &&\n#a1\n#a2\n$(bar)\n}",
		"{\n\tfoo &&\n\t\t#a1\n\t\t#a2\n\t\t$(bar)\n}",
	},
	{
		"foo | while read l; do\nbar\ndone",
		"foo | while read l; do\n\tbar\ndone",
	},
	samePrint("while x; do\n\t#comment\ndone"),
	{
		"while x\ndo\n\ty\ndone",
		"while x; do\n\ty\ndone",
	},
	samePrint("\"\\\nfoo\""),
	samePrint("'\\\nfoo'"),
	samePrint("\"foo\\\n  bar\""),
	samePrint("'foo\\\n  bar'"),
	{
		"foo \\\n>bar\netc",
		"foo \\\n\t>bar\netc",
	},
	{
		"foo \\\nfoo2 \\\n>bar",
		"foo \\\n\tfoo2 \\\n\t>bar",
	},
	samePrint("> >(foo)"),
	samePrint("x > >(foo) y"),
	samePrint("a | () |\n\tb"),
	samePrint("a | (\n\tx\n\ty\n) |\n\tb"),
	samePrint("a |\n\tif foo; then\n\t\tbar\n\tfi |\n\tb"),
	samePrint("a | if foo; then\n\tbar\nfi"),
	samePrint("a | b | if foo; then\n\tbar\nfi"),
	{
		"case $i in\n1)\nfoo\n;;\nesac",
		"case $i in\n1)\n\tfoo\n\t;;\nesac",
	},
	{
		"case $i in\n1)\nfoo\nesac",
		"case $i in\n1)\n\tfoo\n\t;;\nesac",
	},
	{
		"case $i in\n1) foo\nesac",
		"case $i in\n1) foo ;;\nesac",
	},
	{
		"case $i in\n1) foo; bar\nesac",
		"case $i in\n1)\n\tfoo\n\tbar\n\t;;\nesac",
	},
	{
		"case $i in\n1) foo; bar;;\nesac",
		"case $i in\n1)\n\tfoo\n\tbar\n\t;;\nesac",
	},
	{
		"case $i in\n1)\n#foo \t\n;;\nesac",
		"case $i in\n1)\n\t#foo\n\t;;\nesac",
	},
	samePrint("case $i in\n1)\n\ta\n\t#b\n\t;;\nesac"),
	samePrint("case $i in\n1) foo() { bar; } ;;\nesac"),
	samePrint("case $i in\n1) ;; #foo\nesac"),
	samePrint("case $i in\n#foo\nesac"),
	samePrint("case $i in\n#before\n1) ;;\nesac"),
	samePrint("case $i in\n#bef\n1) ;; #inl\nesac"),
	samePrint("case $i in\n1) ;; #inl1\n2) ;; #inl2\nesac"),
	samePrint("case $i in\n#bef\n1) #inl\n\tfoo\n\t;;\nesac"),
	// TODO: reenable once we improve comment handling
	//samePrint("case $i in\n1) #inl\n\t;;\nesac"),
	samePrint("case $i in\n1) a \\\n\tb ;;\nesac"),
	samePrint("case $i in\n1 | 2 | \\\n\t3 | 4) a b ;;\nesac"),
	samePrint("case $i in\n1 | 2 | \\\n\t3 | 4)\n\ta b\n\t;;\nesac"),
	samePrint("case $i in\nx) ;;\ny) for n in 1; do echo $n; done ;;\nesac"),
	{
		"a=(\nb\nc\n) b=c",
		"a=(\n\tb\n\tc\n) b=c",
	},
	samePrint("a=(\n\t#before\n\tb #inline\n)"),
	samePrint("a=(\n\tb #foo\n\tc #bar\n)"),
	samePrint("a=(\n\tb\n\n\t#foo\n\t#bar\n\tc\n)"),
	samePrint("a=(\n\t#foo\n\t#bar\n\tc\n)"),
	samePrint("a=(\n\t#lone\n)"),
	samePrint("a=(\n\n)"),
	samePrint("foo <<EOF | $(bar)\n3\nEOF"),
	{
		"a <<EOF\n$(\n\tb\n\tc)\nEOF",
		"a <<EOF\n$(\n\tb\n\tc\n)\nEOF",
	},
	{
		"<(<<EOF\nbody\nEOF\n)",
		"<(\n\t<<EOF\nbody\nEOF\n)",
	},
	{
		"( (foo) )\n$( (foo) )\n<( (foo) )",
		"( (foo))\n$( (foo))\n<((foo))",
	},
	samePrint("\"foo\n$(bar)\""),
	samePrint("\"foo\\\n$(bar)\""),
	samePrint("((foo++)) || bar"),
	{
		"a=b \\\nc=d \\\nfoo",
		"a=b \\\n\tc=d \\\n\tfoo",
	},
	{
		"a=b \\\nc=d \\\nfoo \\\nbar",
		"a=b \\\n\tc=d \\\n\tfoo \\\n\tbar",
	},
	samePrint("a $(x) \\\n\tb"),
	samePrint("\"foo\nbar\"\netc"),
	samePrint("\"foo\nbar\nbar2\"\netc"),
	samePrint("a=\"$b\n\"\nd=e"),
	samePrint("\"\n\"\n\nfoo"),
	samePrint("$\"\n\"\n\nfoo"),
	samePrint("'\n'\n\nfoo"),
	samePrint("$'\n'\n\nfoo"),
	samePrint("foo <<EOF\na\nb\nc\nd\nEOF\n{\n\tbar\n}"),
	samePrint("foo bar # one\nif a; then\n\tb\nfi # two"),
	{
		"# foo\n\n\nbar",
		"# foo\n\nbar",
	},
	{
		"# foo\n\n\nbar\nbaz",
		"# foo\n\nbar\nbaz",
	},
	samePrint("#foo\n#\n#bar"),
	{
		"(0 #\n0)#\n0",
		"(\n\t0 #\n\t0\n) #\n0",
	},
	samePrint("a | #c1\n\t(\n\t\tb\n\t)"),
	samePrint("a | #c1\n\t{\n\t\tb\n\t}"),
	samePrint("a | #c1\n\tif b; then\n\t\tc\n\tfi"),
	samePrint("a | #c1\n\t#c2\n\t#c3\n\tb"),
	samePrint("a && #c1\n\t(\n\t\tb\n\t)"),
	samePrint("f() body # comment"),
	samePrint("f <<EOF\nbody\nEOF"),
	samePrint("f <<EOF\nEOF"),
	samePrint("f <<-EOF\n\tbody\nEOF"),
	samePrint("f <<-EOF\nEOF"),
	samePrint("{\n\tf <<EOF\nEOF\n}"),
	samePrint("{\n\tf <<-EOF\n\t\tbody\n\tEOF\n}"),
	samePrint("{\n\tf <<-EOF\n\t\tbody\n\tEOF\n\tf2\n}"),
	samePrint("f <<EOF\nEOF\n# comment"),
	samePrint("f <<EOF\nEOF\n# comment\nbar"),
	samePrint("f <<EOF # inline\n$(\n\t# inside\n)\nEOF\n# outside\nbar"),
	{
		"if foo # inline\nthen\n\tbar\nfi",
		"if foo; then # inline\n\tbar\nfi",
	},
	{
		"for foo in a b # inline\ndo\n\tbar\ndone",
		"for foo in a b; do # inline\n\tbar\ndone",
	},
	{
		"if x # inline\nthen bar; fi",
		"if x; then # inline\n\tbar\nfi",
	},
	{
		"for i in a b # inline\ndo bar; done",
		"for i in a b; do # inline\n\tbar\ndone",
	},
	{
		"for i #a\n\tin 1; do #b\ndone",
		"for i in \\\n\t1; do #a\n\t#b\ndone",
	},
	{
		"foo() # inline\n{\n\tbar\n}",
		"foo() { # inline\n\tbar\n}",
	},
	samePrint("if foo; then\n\tbar\n\t# comment\nfi"),
	samePrint("if foo; then\n\tx\nelse\n\tbar\n\t# comment\nfi"),
}

func TestPrintWeirdFormat(t *testing.T) {
	t.Parallel()

	parser := NewParser(KeepComments)
	printer := NewPrinter()
	for i, tc := range printTests {
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			printTest(t, parser, printer, tc.in, tc.want)
		})
		t.Run(fmt.Sprintf("%03d-nl", i), func(t *testing.T) {
			printTest(t, parser, printer, "\n"+tc.in+"\n", tc.want)
		})
		t.Run(fmt.Sprintf("%03d-redo", i), func(t *testing.T) {
			printTest(t, parser, printer, tc.want, tc.want)
		})
	}
}

func parsePath(tb testing.TB, path string) *File {
	f, err := os.Open(path)
	if err != nil {
		tb.Fatal(err)
	}
	defer f.Close()
	prog, err := NewParser(KeepComments).Parse(f, "")
	if err != nil {
		tb.Fatal(err)
	}
	return prog
}

const canonicalPath = "canonical.sh"

func TestPrintMultiline(t *testing.T) {
	prog := parsePath(t, canonicalPath)
	got, err := strPrint(NewPrinter(), prog)
	if err != nil {
		t.Fatal(err)
	}

	want, err := ioutil.ReadFile(canonicalPath)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("Print mismatch in canonical.sh")
	}
}

func BenchmarkPrint(b *testing.B) {
	prog := parsePath(b, canonicalPath)
	printer := NewPrinter()
	for i := 0; i < b.N; i++ {
		if err := printer.Print(ioutil.Discard, prog); err != nil {
			b.Fatal(err)
		}
	}
}

func TestPrintSpaces(t *testing.T) {
	var spaceFormats = [...]struct {
		spaces   uint
		in, want string
	}{
		{
			0,
			"{\nfoo \\\nbar\n}",
			"{\n\tfoo \\\n\t\tbar\n}",
		},
		{
			2,
			"{\nfoo \\\nbar\n}",
			"{\n  foo \\\n    bar\n}",
		},
		{
			4,
			"{\nfoo \\\nbar\n}",
			"{\n    foo \\\n        bar\n}",
		},
	}

	parser := NewParser(KeepComments)
	for i, tc := range spaceFormats {
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			printer := NewPrinter(Indent(tc.spaces))
			printTest(t, parser, printer, tc.in, tc.want)
		})
	}
}

var errBadWriter = fmt.Errorf("write: expected error")

type badWriter struct{}

func (b badWriter) Write(p []byte) (int, error) { return 0, errBadWriter }

func TestWriteErr(t *testing.T) {
	_ = (*byteCounter)(nil).Flush()
	f := &File{StmtList: StmtList{Stmts: []*Stmt{
		{
			Redirs: []*Redirect{{
				Op:   RdrOut,
				Word: litWord("foo"),
			}},
			Cmd: &Subshell{},
		},
	}}}
	err := NewPrinter().Print(badWriter{}, f)
	if err == nil {
		t.Fatalf("Expected error with bad writer")
	}
	if err != errBadWriter {
		t.Fatalf("Error mismatch with bad writer:\nwant: %v\ngot:  %v",
			errBadWriter, err)
	}
}

func TestPrintBinaryNextLine(t *testing.T) {
	var tests = [...]printCase{
		{
			"foo <<EOF &&\nl1\nEOF\nbar",
			"foo <<EOF && bar\nl1\nEOF",
		},
		samePrint("a \\\n\t&& b"),
		samePrint("a \\\n\t&& b\nc"),
		{
			"{\n(a \\\n&& b)\nc\n}",
			"{\n\t(a \\\n\t\t&& b)\n\tc\n}",
		},
		{
			"a && b \\\n&& c",
			"a && b \\\n\t&& c",
		},
		{
			"a \\\n&& $(b) && c \\\n&& d",
			"a \\\n\t&& $(b) && c \\\n\t&& d",
		},
		{
			"a \\\n&& b\nc \\\n&& d",
			"a \\\n\t&& b\nc \\\n\t&& d",
		},
		{
			"a | {\nb \\\n| c\n}",
			"a | {\n\tb \\\n\t\t| c\n}",
		},
		{
			"a \\\n\t&& if foo; then\nbar\nfi",
			"a \\\n\t&& if foo; then\n\t\tbar\n\tfi",
		},
		{
			"if foo \\\n&& bar\nthen\nbar\nfi",
			"if foo \\\n\t&& bar; then\n\tbar\nfi",
		},
		{
			"a |\nb |\nc",
			"a \\\n\t| b \\\n\t| c",
		},
		{
			"foo |\n# misplaced\nbar",
			"foo \\\n\t|\n\t# misplaced\n\tbar",
		},
		samePrint("{\n\tfoo\n\t#a\n\tbar\n} | etc"),
		{
			"foo &&\n#a1\n#a2\n$(bar)",
			"foo \\\n\t&&\n\t#a1\n\t#a2\n\t$(bar)",
		},
		{
			"{\n\tfoo\n\t#a\n} |\n# misplaced\nbar",
			"{\n\tfoo\n\t#a\n} \\\n\t|\n\t# misplaced\n\tbar",
		},
		samePrint("foo | bar\n#after"),
		{
			"a |\nb | #c2\nc",
			"a \\\n\t| b \\\n\t|\n\t#c2\n\tc",
		},
	}
	parser := NewParser(KeepComments)
	printer := NewPrinter(BinaryNextLine)
	for i, tc := range tests {
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			printTest(t, parser, printer, tc.in, tc.want)
		})
	}
}

func TestPrintSwitchCaseIndent(t *testing.T) {
	var tests = [...]printCase{
		{
			"case $i in\n1)\nfoo\n;;\nesac",
			"case $i in\n\t1)\n\t\tfoo\n\t\t;;\nesac",
		},
		{
			"case $i in\n1)\na\n;;\n2)\nb\n;;\nesac",
			"case $i in\n\t1)\n\t\ta\n\t\t;;\n\t2)\n\t\tb\n\t\t;;\nesac",
		},
		samePrint("case $i in\n\t#foo\nesac"),
	}
	parser := NewParser(KeepComments)
	printer := NewPrinter(SwitchCaseIndent)
	for i, tc := range tests {
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			printTest(t, parser, printer, tc.in, tc.want)
		})
	}
}

func TestPrintSpaceRedirects(t *testing.T) {
	var tests = [...]printCase{
		samePrint("echo foo bar > f"),
		samePrint("echo > f foo bar"),
		samePrint("echo >(cmd)"),
		samePrint("echo > >(cmd)"),
		samePrint("<< EOF\nfoo\nEOF"),
		samePrint("echo 2> f"),
		samePrint("echo foo bar >&1"),
		samePrint("echo 2<&1 foo bar"),
	}
	parser := NewParser(KeepComments)
	printer := NewPrinter(SpaceRedirects)
	for i, tc := range tests {
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			printTest(t, parser, printer, tc.in, tc.want)
		})
	}
}

func TestPrintKeepPadding(t *testing.T) {
	var tests = [...]printCase{
		samePrint("echo foo bar"),
		samePrint("echo  foo   bar"),
		samePrint("a=b  c=d   bar"),
		samePrint("echo foo    >bar"),
		samePrint("echo foo    2>bar"),
		samePrint("{ foo;  }"),
		samePrint("a()   { foo; }"),
		samePrint("a   && b"),
		samePrint("a   | b"),
		samePrint("a |  b"),
		samePrint("{  a b c; }"),
		samePrint("foo    # x\nbaaar  # y"),
		samePrint("{ { a; }; }"),
		samePrint("{  a;  }"),
		samePrint("(  a   )"),
		samePrint("'foo\nbar'   # x"),
	}
	parser := NewParser(KeepComments)
	printer := NewPrinter(KeepPadding)
	for i, tc := range tests {
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			printTest(t, parser, printer, tc.in, tc.want)
		})
	}
}

func TestPrintMinify(t *testing.T) {
	var tests = [...]printCase{
		samePrint("echo foo bar $a $(b)"),
		{
			"#comment",
			"",
		},
		{
			"foo #comment",
			"foo",
		},
		{
			"foo\n\nbar",
			"foo\nbar",
		},
		{
			"foo &",
			"foo&",
		},
		samePrint("foo >bar 2>baz <etc"),
		{
			"{\n\tfoo\n}",
			"{\nfoo\n}",
		},
		{
			"(\n\ta\n)\n(\n\tb\n\tc\n)",
			"(a)\n(b\nc)",
		},
		{
			"$(\n\ta\n)\n$(\n\tb\n\tc\n)",
			"$(a)\n$(b\nc)",
		},
		{
			"f() { x; }",
			"f(){ x;}",
		},
		{
			"((1 + 2))",
			"((1+2))",
		},
		{
			"echo $a ${b} ${c}-d ${e}f ${g}_h",
			"echo $a $b $c-d ${e}f ${g}_h",
		},
		{
			"echo ${0} ${3} ${10} ${22}",
			"echo $0 $3 ${10} ${22}",
		},
		{
			"case $a in\nx) c ;;\ny | z)\n\td\n\t;;\nesac",
			"case $a in\nx)c;;\ny|z)d\nesac",
		},
		{
			"a && b | c",
			"a&&b|c",
		},
		{
			"a &&\n\tb |\n\tc",
			"a&&b|c",
		},
	}
	parser := NewParser(KeepComments)
	printer := NewPrinter(Minify)
	for i, tc := range tests {
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			printTest(t, parser, printer, tc.in, tc.want)
		})
	}
}

func TestPrintMinifyNotBroken(t *testing.T) {
	parserBash := NewParser(KeepComments)
	parserPosix := NewParser(KeepComments, Variant(LangPOSIX))
	parserMirBSD := NewParser(KeepComments, Variant(LangMirBSDKorn))
	printer := NewPrinter(Minify)
	for i, tc := range fileTests {
		t.Run(fmt.Sprintf("File%03d", i), func(t *testing.T) {
			parser := parserPosix
			if tc.Bash != nil {
				parser = parserBash
			} else if tc.MirBSDKorn != nil {
				parser = parserMirBSD
			}
			in := tc.Strs[0]
			prog, err := parser.Parse(strings.NewReader(in), "")
			if err != nil {
				t.Fatal(err)
			}
			got, err := strPrint(printer, prog)
			if err != nil {
				t.Fatal(err)
			}
			_, err = parser.Parse(strings.NewReader(got), "")
			if err != nil {
				t.Fatalf("minified program was broken: %v\n%s", err, got)
			}
		})
	}
	for i, tc := range printTests {
		t.Run(fmt.Sprintf("Print%03d", i), func(t *testing.T) {
			prog, err := parserBash.Parse(strings.NewReader(tc.in), "")
			if err != nil {
				t.Fatal(err)
			}
			got, err := strPrint(printer, prog)
			if err != nil {
				t.Fatal(err)
			}
			_, err = parserBash.Parse(strings.NewReader(got), "")
			if err != nil {
				t.Fatalf("minified program was broken: %v\n%s", err, got)
			}
		})
	}
}

func printTest(t *testing.T, parser *Parser, printer *Printer, in, want string) {
	prog, err := parser.Parse(strings.NewReader(in), "")
	if err != nil {
		t.Fatal(err)
	}
	wantNewl := want + "\n"
	got, err := strPrint(printer, prog)
	if err != nil {
		t.Fatal(err)
	}
	if got != wantNewl {
		t.Fatalf("Print mismatch:\nin:\n%s\nwant:\n%sgot:\n%s",
			in, wantNewl, got)
	}
	_, err = parser.Parse(strings.NewReader(want), "")
	if err != nil {
		t.Fatalf("Result is not valid shell:\n%s", want)
	}
}

func TestPrintNodeTypes(t *testing.T) {
	var tests = [...]struct {
		in   Node
		want string
	}{
		{
			&File{StmtList: litStmts("foo")},
			"foo\n",
		},
		{
			&File{StmtList: litStmts("foo", "bar")},
			"foo\nbar\n",
		},
		{
			litStmt("foo", "bar"),
			"foo bar",
		},
		{
			litCall("foo", "bar"),
			"foo bar",
		},
		{
			litWord("foo"),
			"foo",
		},
	}
	printer := NewPrinter()
	for i, tc := range tests {
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			got, err := strPrint(printer, tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("Print mismatch:\nwant:\n%s\ngot:\n%s",
					tc.want, got)
			}
		})
	}
}

func TestPrintManyStmts(t *testing.T) {
	var tests = [...]struct {
		in, want string
	}{
		{"foo; bar", "foo\nbar\n"},
		{"foo\nbar", "foo\nbar\n"},
		{"\n\nfoo\nbar\n\n", "foo\nbar\n"},
		{"foo\nbar <<EOF\nbody\nEOF\n", "foo\nbar <<EOF\nbody\nEOF\n"},
		{"foo\nbar # inline", "foo\nbar # inline\n"},
		{"# comment before\nfoo bar", "# comment before\nfoo bar\n"},
	}
	parser := NewParser(KeepComments)
	printer := NewPrinter()
	for i, tc := range tests {
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			f, err := parser.Parse(strings.NewReader(tc.in), "")
			if err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			for _, stmt := range f.Stmts {
				printer.Print(&buf, stmt)
				buf.WriteByte('\n')
			}
			got := buf.String()
			if got != tc.want {
				t.Fatalf("Print mismatch:\nwant:\n%s\ngot:\n%s",
					tc.want, got)
			}
		})
	}
}
