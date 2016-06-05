// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	flag.Parse()
	for i := range astTests {
		astTests[i].ast = fullProg(astTests[i].ast)
	}
	os.Exit(m.Run())
}

func lit(s string) Lit     { return Lit{Value: s} }
func litRef(s string) *Lit { return &Lit{Value: s} }
func lits(strs ...string) []Node {
	l := make([]Node, len(strs))
	for i, s := range strs {
		l[i] = lit(s)
	}
	return l
}

func word(ns ...Node) Word  { return Word{Parts: ns} }
func litWord(s string) Word { return word(lits(s)...) }
func litWords(strs ...string) []Word {
	l := make([]Word, 0, len(strs))
	for _, s := range strs {
		l = append(l, litWord(s))
	}
	return l
}

func cmd(words ...Word) Command     { return Command{Args: words} }
func litCmd(strs ...string) Command { return cmd(litWords(strs...)...) }

func stmt(n Node) Stmt { return Stmt{Node: n} }
func stmts(ns ...Node) []Stmt {
	l := make([]Stmt, len(ns))
	for i, n := range ns {
		l[i] = stmt(n)
	}
	return l
}

func litStmt(strs ...string) Stmt { return stmt(litCmd(strs...)) }
func litStmts(strs ...string) []Stmt {
	l := make([]Stmt, len(strs))
	for i, s := range strs {
		l[i] = litStmt(s)
	}
	return l
}

func sglQuoted(s string) SglQuoted    { return SglQuoted{Value: s} }
func dblQuoted(ns ...Node) Quoted     { return Quoted{Quote: DQUOTE, Parts: ns} }
func block(sts ...Stmt) Block         { return Block{Stmts: sts} }
func subshell(sts ...Stmt) Subshell   { return Subshell{Stmts: sts} }
func arithmExpr(expr Node) ArithmExpr { return ArithmExpr{X: expr} }
func parenExpr(expr Node) ParenExpr   { return ParenExpr{X: expr} }

func cmdSubst(sts ...Stmt) CmdSubst { return CmdSubst{Stmts: sts} }
func bckQuoted(sts ...Stmt) CmdSubst {
	return CmdSubst{Backquotes: true, Stmts: sts}
}

func litParamExp(s string) ParamExp {
	return ParamExp{Short: true, Param: lit(s)}
}

type testCase struct {
	strs []string
	ast  interface{}
}

var astTests = []testCase{
	{
		[]string{"", " ", "\t", "\n \n"},
		nil,
	},
	{
		[]string{"", "# foo", "# foo ( bar", "# foo'bar"},
		nil,
	},
	{
		[]string{"foo", "foo ", " foo", "foo # bar"},
		litWord("foo"),
	},
	{
		[]string{"foo\nbar", "foo; bar;", "foo;bar;", "\nfoo\nbar\n"},
		litStmts("foo", "bar"),
	},
	{
		[]string{"foo a b", " foo  a  b ", "foo \\\n a b"},
		litCmd("foo", "a", "b"),
	},
	{
		[]string{"foobar", "foo\\\nbar"},
		litWord("foobar"),
	},
	{
		[]string{"foo'bar'"},
		word(lit("foo"), sglQuoted("bar")),
	},
	{
		[]string{"(foo)", "(foo;)", "(\nfoo\n)"},
		subshell(litStmt("foo")),
	},
	{
		[]string{"(\n\tfoo\n\tbar\n)", "(foo; bar)"},
		subshell(litStmt("foo"), litStmt("bar")),
	},
	{
		[]string{"( )"},
		subshell(),
	},
	{
		[]string{"{ foo; }", "{\nfoo\n}"},
		block(litStmt("foo")),
	},
	{
		[]string{
			"if a; then b; fi",
			"if a\nthen\nb\nfi",
		},
		IfStmt{
			Cond:      StmtCond{Stmts: litStmts("a")},
			ThenStmts: litStmts("b"),
		},
	},
	{
		[]string{
			"if a; then b; else c; fi",
			"if a\nthen b\nelse\nc\nfi",
		},
		IfStmt{
			Cond:      StmtCond{Stmts: litStmts("a")},
			ThenStmts: litStmts("b"),
			ElseStmts: litStmts("c"),
		},
	},
	{
		[]string{
			"if a; then a; elif b; then b; elif c; then c; else d; fi",
			"if a\nthen a\nelif b\nthen b\nelif c\nthen c\nelse\nd\nfi",
		},
		IfStmt{
			Cond:      StmtCond{Stmts: litStmts("a")},
			ThenStmts: litStmts("a"),
			Elifs: []Elif{
				{
					Cond:      StmtCond{Stmts: litStmts("b")},
					ThenStmts: litStmts("b"),
				},
				{
					Cond:      StmtCond{Stmts: litStmts("c")},
					ThenStmts: litStmts("c"),
				},
			},
			ElseStmts: litStmts("d"),
		},
	},
	{
		[]string{
			"if\n\ta1\n\ta2 foo\n\ta3 bar\nthen b; fi",
			"if a1; a2 foo; a3 bar; then b; fi",
		},
		IfStmt{
			Cond: StmtCond{Stmts: []Stmt{
				litStmt("a1"),
				litStmt("a2", "foo"),
				litStmt("a3", "bar"),
			}},
			ThenStmts: litStmts("b"),
		},
	},
	{
		[]string{"if ((1 > 2)); then b; fi"},
		IfStmt{
			Cond: CStyleCond{Cond: BinaryExpr{
				Op: GTR,
				X:  litWord("1"),
				Y:  litWord("2"),
			}},
			ThenStmts: litStmts("b"),
		},
	},
	{
		[]string{"while a; do b; done", "while a\ndo\nb\ndone"},
		WhileStmt{
			Cond:    StmtCond{Stmts: litStmts("a")},
			DoStmts: litStmts("b"),
		},
	},
	{
		[]string{"while { a; }; do b; done", "while { a; } do b; done"},
		WhileStmt{
			Cond: StmtCond{Stmts: []Stmt{
				stmt(block(litStmt("a"))),
			}},
			DoStmts: litStmts("b"),
		},
	},
	{
		[]string{"while (a); do b; done", "while (a) do b; done"},
		WhileStmt{
			Cond: StmtCond{Stmts: []Stmt{
				stmt(subshell(litStmt("a"))),
			}},
			DoStmts: litStmts("b"),
		},
	},
	{
		[]string{"while ((1 > 2)); do b; done"},
		WhileStmt{
			Cond: CStyleCond{Cond: BinaryExpr{
				Op: GTR,
				X:  litWord("1"),
				Y:  litWord("2"),
			}},
			DoStmts: litStmts("b"),
		},
	},
	{
		[]string{"until a; do b; done", "until a\ndo\nb\ndone"},
		UntilStmt{
			Cond:    StmtCond{Stmts: litStmts("a")},
			DoStmts: litStmts("b"),
		},
	},
	{
		[]string{
			"for i; do foo; done",
			"for i in; do foo; done",
		},
		ForStmt{
			Cond: WordIter{
				Name: lit("i"),
			},
			DoStmts: litStmts("foo"),
		},
	},
	{
		[]string{
			"for i in 1 2 3; do echo $i; done",
			"for i in 1 2 3\ndo echo $i\ndone",
			"for i in 1 2 3 #foo\ndo echo $i\ndone",
		},
		ForStmt{
			Cond: WordIter{
				Name: lit("i"),
				List: litWords("1", "2", "3"),
			},
			DoStmts: stmts(cmd(
				litWord("echo"),
				word(litParamExp("i")),
			)),
		},
	},
	{
		[]string{
			"for ((i = 0; i < 10; i++)); do echo $i; done",
			"for ((i=0;i<10;i++)) do echo $i; done",
			"for (( i = 0 ; i < 10 ; i++ ))\ndo echo $i\ndone",
		},
		ForStmt{
			Cond: CStyleLoop{
				Init: BinaryExpr{
					Op: ASSIGN,
					X:  litWord("i"),
					Y:  litWord("0"),
				},
				Cond: BinaryExpr{
					Op: LSS,
					X:  litWord("i"),
					Y:  litWord("10"),
				},
				Post: UnaryExpr{
					Op:   INC,
					Post: true,
					X:    litWord("i"),
				},
			},
			DoStmts: stmts(cmd(
				litWord("echo"),
				word(litParamExp("i")),
			)),
		},
	},
	{
		[]string{`' ' "foo bar"`},
		cmd(
			word(sglQuoted(" ")),
			word(dblQuoted(lits("foo bar")...)),
		),
	},
	{
		[]string{`"foo \" bar"`},
		word(dblQuoted(lits(`foo \" bar`)...)),
	},
	{
		[]string{"\">foo\" \"\nbar\""},
		cmd(
			word(dblQuoted(lit(">foo"))),
			word(dblQuoted(lit("\nbar"))),
		),
	},
	{
		[]string{`foo \" bar`},
		litCmd(`foo`, `\"`, `bar`),
	},
	{
		[]string{`'"'`},
		word(sglQuoted(`"`)),
	},
	{
		[]string{"'`'"},
		word(sglQuoted("`")),
	},
	{
		[]string{`"'"`},
		word(dblQuoted(lit("'"))),
	},
	{
		[]string{"=a s{s s=s"},
		litCmd("=a", "s{s", "s=s"),
	},
	{
		[]string{"foo && bar", "foo&&bar", "foo &&\nbar"},
		BinaryExpr{
			Op: LAND,
			X:  litStmt("foo"),
			Y:  litStmt("bar"),
		},
	},
	{
		[]string{"foo \\\n\t&& bar"},
		BinaryExpr{
			Op: LAND,
			X:  litStmt("foo"),
			Y:  litStmt("bar"),
		},
	},
	{
		[]string{"foo || bar", "foo||bar", "foo ||\nbar"},
		BinaryExpr{
			Op: LOR,
			X:  litStmt("foo"),
			Y:  litStmt("bar"),
		},
	},
	{
		[]string{"if a; then b; fi || while a; do b; done"},
		BinaryExpr{
			Op: LOR,
			X: stmt(IfStmt{
				Cond:      StmtCond{Stmts: litStmts("a")},
				ThenStmts: litStmts("b"),
			}),
			Y: stmt(WhileStmt{
				Cond:    StmtCond{Stmts: litStmts("a")},
				DoStmts: litStmts("b"),
			}),
		},
	},
	{
		[]string{"foo && bar1 || bar2"},
		BinaryExpr{
			Op: LAND,
			X:  litStmt("foo"),
			Y: stmt(BinaryExpr{
				Op: LOR,
				X:  litStmt("bar1"),
				Y:  litStmt("bar2"),
			}),
		},
	},
	{
		[]string{"foo | bar", "foo|bar", "foo |\n#etc\nbar"},
		BinaryExpr{
			Op: OR,
			X:  litStmt("foo"),
			Y:  litStmt("bar"),
		},
	},
	{
		[]string{"foo | bar | extra"},
		BinaryExpr{
			Op: OR,
			X:  litStmt("foo"),
			Y: stmt(BinaryExpr{
				Op: OR,
				X:  litStmt("bar"),
				Y:  litStmt("extra"),
			}),
		},
	},
	{
		[]string{"foo |& bar", "foo|&bar"},
		BinaryExpr{
			Op: PIPEALL,
			X:  litStmt("foo"),
			Y:  litStmt("bar"),
		},
	},
	{
		[]string{
			"foo() {\n\ta\n\tb\n}",
			"foo() { a; b; }",
			"foo ( ) {\na\nb\n}",
		},
		FuncDecl{
			Name: lit("foo"),
			Body: stmt(block(litStmts("a", "b")...)),
		},
	},
	{
		[]string{"foo() { a; }\nbar", "foo() {\na\n}; bar"},
		[]Node{
			FuncDecl{
				Name: lit("foo"),
				Body: stmt(block(litStmts("a")...)),
			},
			litCmd("bar"),
		},
	},
	{
		[]string{"-foo_.,+-bar() { a; }"},
		FuncDecl{
			Name: lit("-foo_.,+-bar"),
			Body: stmt(block(litStmts("a")...)),
		},
	},
	{
		[]string{
			"function foo() {\n\ta\n\tb\n}",
			"function foo {\n\ta\n\tb\n}",
			"function foo() { a; b; }",
		},
		FuncDecl{
			BashStyle: true,
			Name:      lit("foo"),
			Body:      stmt(block(litStmts("a", "b")...)),
		},
	},
	{
		[]string{"function foo() (a)"},
		FuncDecl{
			BashStyle: true,
			Name:      lit("foo"),
			Body:      stmt(subshell(litStmt("a"))),
		},
	},
	{
		[]string{"a=b foo=$bar"},
		Stmt{
			Assigns: []Assign{
				{Name: lit("a"), Value: litWord("b")},
				{Name: lit("foo"), Value: word(litParamExp("bar"))},
			},
		},
	},
	{
		[]string{"a=\"\nbar\""},
		Stmt{
			Assigns: []Assign{
				{
					Name:  lit("a"),
					Value: word(dblQuoted(lit("\nbar"))),
				},
			},
		},
	},
	{
		[]string{"a= foo"},
		Stmt{
			Node:    litCmd("foo"),
			Assigns: []Assign{{Name: lit("a")}},
		},
	},
	{
		[]string{
			"foo >a >>b <c",
			"foo > a >> b < c",
			">a >>b <c foo",
		},
		Stmt{
			Node: litCmd("foo"),
			Redirs: []Redirect{
				{Op: GTR, Word: litWord("a")},
				{Op: SHR, Word: litWord("b")},
				{Op: LSS, Word: litWord("c")},
			},
		},
	},
	{
		[]string{
			"foo bar >a",
			"foo >a bar",
		},
		Stmt{
			Node: litCmd("foo", "bar"),
			Redirs: []Redirect{
				{Op: GTR, Word: litWord("a")},
			},
		},
	},
	{
		[]string{`>a >\b`},
		Stmt{
			Redirs: []Redirect{
				{Op: GTR, Word: litWord("a")},
				{Op: GTR, Word: litWord(`\b`)},
			},
		},
	},
	{
		[]string{">a\n>b", ">a; >b"},
		[]Stmt{
			{Redirs: []Redirect{
				{Op: GTR, Word: litWord("a")},
			}},
			{Redirs: []Redirect{
				{Op: GTR, Word: litWord("b")},
			}},
		},
	},
	{
		[]string{"foo1\nfoo2 >r2", "foo1; >r2 foo2"},
		[]Stmt{
			litStmt("foo1"),
			{
				Node: litCmd("foo2"),
				Redirs: []Redirect{
					{Op: GTR, Word: litWord("r2")},
				},
			},
		},
	},
	{
		[]string{
			"foo <<EOF\nbar\nEOF",
			"foo <<EOF\nbar",
		},
		Stmt{
			Node: litCmd("foo"),
			Redirs: []Redirect{{
				Op:   SHL,
				Word: litWord("EOF"),
				Hdoc: litRef("bar\n"),
			}},
		},
	},
	{
		[]string{"foo <<EOF\n1\n2\n3\nEOF"},
		Stmt{
			Node: litCmd("foo"),
			Redirs: []Redirect{{
				Op:   SHL,
				Word: litWord("EOF"),
				Hdoc: litRef("1\n2\n3\n"),
			}},
		},
	},
	{
		[]string{"{ foo <<EOF\nbar\nEOF\n}"},
		block(Stmt{
			Node: litCmd("foo"),
			Redirs: []Redirect{{
				Op:   SHL,
				Word: litWord("EOF"),
				Hdoc: litRef("bar\n"),
			}},
		}),
	},
	{
		[]string{"$(foo <<EOF\nbar\nEOF\n)"},
		word(cmdSubst(Stmt{
			Node: litCmd("foo"),
			Redirs: []Redirect{{
				Op:   SHL,
				Word: litWord("EOF"),
				Hdoc: litRef("bar\n"),
			}},
		})),
	},
	{
		[]string{"foo >f <<EOF\nbar\nEOF"},
		Stmt{
			Node: litCmd("foo"),
			Redirs: []Redirect{
				{Op: GTR, Word: litWord("f")},
				{
					Op:   SHL,
					Word: litWord("EOF"),
					Hdoc: litRef("bar\n"),
				},
			},
		},
	},
	{
		[]string{"foo <<EOF >f\nbar\nEOF"},
		Stmt{
			Node: litCmd("foo"),
			Redirs: []Redirect{
				{
					Op:   SHL,
					Word: litWord("EOF"),
					Hdoc: litRef("bar\n"),
				},
				{Op: GTR, Word: litWord("f")},
			},
		},
	},
	{
		[]string{"if true; then foo <<-EOF\n\tbar\n\tEOF\nfi"},
		IfStmt{
			Cond: StmtCond{Stmts: litStmts("true")},
			ThenStmts: []Stmt{{
				Node: litCmd("foo"),
				Redirs: []Redirect{{
					Op:   DHEREDOC,
					Word: litWord("EOF"),
					Hdoc: litRef("\tbar\n\t"),
				}},
			}},
		},
	},
	{
		[]string{"foo <<EOF\nbar\nEOF\nfoo2"},
		[]Stmt{
			{
				Node: litCmd("foo"),
				Redirs: []Redirect{{
					Op:   SHL,
					Word: litWord("EOF"),
					Hdoc: litRef("bar\n"),
				}},
			},
			litStmt("foo2"),
		},
	},
	{
		[]string{"foo <<FOOBAR\nbar\nFOOBAR"},
		Stmt{
			Node: litCmd("foo"),
			Redirs: []Redirect{{
				Op:   SHL,
				Word: litWord("FOOBAR"),
				Hdoc: litRef("bar\n"),
			}},
		},
	},
	{
		[]string{"foo <<\"EOF\"\nbar\nEOF"},
		Stmt{
			Node: litCmd("foo"),
			Redirs: []Redirect{{
				Op:   SHL,
				Word: word(dblQuoted(lit("EOF"))),
				Hdoc: litRef("bar\n"),
			}},
		},
	},
	{
		[]string{"foo <<'EOF'\nbar\nEOF"},
		Stmt{
			Node: litCmd("foo"),
			Redirs: []Redirect{
				{
					Op:   SHL,
					Word: word(sglQuoted("EOF")),
					Hdoc: litRef("bar\n"),
				},
			},
		},
	},
	{
		[]string{"foo <<\"EOF\"2\nbar\nEOF2"},
		Stmt{
			Node: litCmd("foo"),
			Redirs: []Redirect{{
				Op:   SHL,
				Word: word(dblQuoted(lit("EOF")), lit("2")),
				Hdoc: litRef("bar\n"),
			}},
		},
	},
	{
		[]string{"foo <<\\EOF\nbar\nEOF"},
		Stmt{
			Node: litCmd("foo"),
			Redirs: []Redirect{{
				Op:   SHL,
				Word: litWord("\\EOF"),
				Hdoc: litRef("bar\n"),
			}},
		},
	},
	{
		[]string{"foo <<$EOF\nbar\n$EOF"},
		Stmt{
			Node: litCmd("foo"),
			Redirs: []Redirect{{
				Op:   SHL,
				Word: word(litParamExp("EOF")),
				Hdoc: litRef("bar\n"),
			}},
		},
	},
	{
		[]string{
			"foo <<-EOF\nbar\nEOF",
			"foo <<- EOF\nbar\nEOF",
		},
		Stmt{
			Node: litCmd("foo"),
			Redirs: []Redirect{{
				Op:   DHEREDOC,
				Word: litWord("EOF"),
				Hdoc: litRef("bar\n"),
			}},
		},
	},
	{
		[]string{
			"f1 <<EOF1\nh1\nEOF1\nf2 <<EOF2\nh2\nEOF2",
			"f1 <<EOF1; f2 <<EOF2\nh1\nEOF1\nh2\nEOF2",
		},
		[]Stmt{
			{
				Node: litCmd("f1"),
				Redirs: []Redirect{{
					Op:   SHL,
					Word: litWord("EOF1"),
					Hdoc: litRef("h1\n"),
				}},
			},
			{
				Node: litCmd("f2"),
				Redirs: []Redirect{{
					Op:   SHL,
					Word: litWord("EOF2"),
					Hdoc: litRef("h2\n"),
				}},
			},
		},
	},
	{
		[]string{
			"a <<EOF\nfoo\nEOF\nb\nb\nb\nb\nb\nb\nb\nb\nb",
			"a <<EOF;b;b;b;b;b;b;b;b;b\nfoo\nEOF",
		},
		[]Stmt{
			{
				Node: litCmd("a"),
				Redirs: []Redirect{{
					Op:   SHL,
					Word: litWord("EOF"),
					Hdoc: litRef("foo\n"),
				}},
			},
			litStmt("b"), litStmt("b"), litStmt("b"),
			litStmt("b"), litStmt("b"), litStmt("b"),
			litStmt("b"), litStmt("b"), litStmt("b"),
		},
	},
	{
		[]string{"foo >&2 <&0 2>file <>f2 &>f3 &>>f4"},
		Stmt{
			Node: litCmd("foo"),
			Redirs: []Redirect{
				{Op: DPLOUT, Word: litWord("2")},
				{Op: DPLIN, Word: litWord("0")},
				{Op: GTR, N: lit("2"), Word: litWord("file")},
				{Op: RDRINOUT, Word: litWord("f2")},
				{Op: RDRALL, Word: litWord("f3")},
				{Op: APPALL, Word: litWord("f4")},
			},
		},
	},
	{
		[]string{"a >f1\nb >f2", "a >f1; b >f2"},
		[]Stmt{
			{
				Node:   litCmd("a"),
				Redirs: []Redirect{{Op: GTR, Word: litWord("f1")}},
			},
			{
				Node:   litCmd("b"),
				Redirs: []Redirect{{Op: GTR, Word: litWord("f2")}},
			},
		},
	},
	{
		[]string{
			"foo <<<input",
			"foo <<< input",
		},
		Stmt{
			Node: litCmd("foo"),
			Redirs: []Redirect{
				{Op: WHEREDOC, Word: litWord("input")},
			},
		},
	},
	{
		[]string{
			`foo <<<"spaced input"`,
			`foo <<< "spaced input"`,
		},
		Stmt{
			Node: litCmd("foo"),
			Redirs: []Redirect{
				{
					Op:   WHEREDOC,
					Word: word(dblQuoted(lit("spaced input"))),
				},
			},
		},
	},
	{
		[]string{"cat <(foo)"},
		Stmt{
			Node: cmd(
				litWord("cat"),
				word(CmdInput{Stmts: litStmts("foo")}),
			),
		},
	},
	{
		[]string{"cat < <(foo)"},
		Stmt{
			Node: litCmd("cat"),
			Redirs: []Redirect{{
				Op:   LSS,
				Word: word(CmdInput{Stmts: litStmts("foo")}),
			}},
		},
	},
	{
		[]string{"!"},
		Stmt{Negated: true},
	},
	{
		[]string{"! foo"},
		Stmt{
			Negated: true,
			Node:    litCmd("foo"),
		},
	},
	{
		[]string{"foo &\nbar", "foo &; bar", "foo & bar", "foo&bar"},
		[]Stmt{
			{
				Node:       litCmd("foo"),
				Background: true,
			},
			litStmt("bar"),
		},
	},
	{
		[]string{"! if foo; then bar; fi >/dev/null &"},
		Stmt{
			Negated: true,
			Node: IfStmt{
				Cond:      StmtCond{Stmts: litStmts("foo")},
				ThenStmts: litStmts("bar"),
			},
			Redirs: []Redirect{
				{Op: GTR, Word: litWord("/dev/null")},
			},
			Background: true,
		},
	},
	{
		[]string{"foo#bar"},
		litWord("foo#bar"),
	},
	{
		[]string{"{ echo } }; }"},
		block(litStmt("echo", "}", "}")),
	},
	{
		[]string{"`{ echo; }`"},
		word(bckQuoted(stmt(
			block(litStmt("echo")),
		))),
	},
	{
		[]string{`{foo}`},
		litWord(`{foo}`),
	},
	{
		[]string{`{"foo"`},
		word(lit("{"), dblQuoted(lit("foo"))),
	},
	{
		[]string{`!foo`},
		litWord(`!foo`),
	},
	{
		[]string{"$(foo bar)"},
		word(cmdSubst(litStmt("foo", "bar"))),
	},
	{
		[]string{"$(foo | bar)"},
		word(cmdSubst(
			stmt(BinaryExpr{
				Op: OR,
				X:  litStmt("foo"),
				Y:  litStmt("bar"),
			}),
		)),
	},
	{
		[]string{"$(foo $(b1 b2))"},
		word(cmdSubst(
			stmt(cmd(
				litWord("foo"),
				word(cmdSubst(litStmt("b1", "b2"))),
			)),
		)),
	},
	{
		[]string{`"$(foo "bar")"`},
		word(dblQuoted(cmdSubst(
			stmt(cmd(
				litWord("foo"),
				word(dblQuoted(lit("bar"))),
			)),
		))),
	},
	{
		[]string{"`foo`"},
		word(bckQuoted(litStmt("foo"))),
	},
	{
		[]string{"`foo | bar`"},
		word(bckQuoted(
			stmt(BinaryExpr{
				Op: OR,
				X:  litStmt("foo"),
				Y:  litStmt("bar"),
			}),
		)),
	},
	{
		[]string{"`foo 'bar'`"},
		word(bckQuoted(stmt(cmd(
			litWord("foo"),
			word(sglQuoted("bar")),
		)))),
	},
	{
		[]string{"`foo \"bar\"`"},
		word(bckQuoted(
			stmt(Command{Args: []Word{
				litWord("foo"),
				word(dblQuoted(lit("bar"))),
			}}),
		)),
	},
	{
		[]string{`"$foo"`},
		word(dblQuoted(litParamExp("foo"))),
	},
	{
		[]string{`"#foo"`},
		word(dblQuoted(lit("#foo"))),
	},
	{
		[]string{`$@ $# $$ $?`},
		cmd(
			word(litParamExp("@")),
			word(litParamExp("#")),
			word(litParamExp("$")),
			word(litParamExp("?")),
		),
	},
	{
		[]string{`$`, `$ #`},
		litWord("$"),
	},
	{
		[]string{`${@} ${$} ${?}`},
		cmd(
			word(ParamExp{Param: lit("@")}),
			word(ParamExp{Param: lit("$")}),
			word(ParamExp{Param: lit("?")}),
		),
	},
	{
		[]string{`${foo}`},
		word(ParamExp{Param: lit("foo")}),
	},
	{
		[]string{`${foo}"bar"`},
		word(
			ParamExp{Param: lit("foo")},
			dblQuoted(lit("bar")),
		),
	},
	{
		[]string{`${foo-bar}`},
		word(ParamExp{
			Param: lit("foo"),
			Exp: &Expansion{
				Op:   SUB,
				Word: litWord("bar"),
			},
		}),
	},
	{
		[]string{`${foo-bar}"bar"`},
		word(
			ParamExp{
				Param: lit("foo"),
				Exp: &Expansion{
					Op:   SUB,
					Word: litWord("bar"),
				},
			},
			dblQuoted(lit("bar")),
		),
	},
	{
		[]string{`${foo:="bar"}`},
		word(ParamExp{
			Param: lit("foo"),
			Exp: &Expansion{
				Op:   CASSIGN,
				Word: word(dblQuoted(lit("bar"))),
			},
		}),
	},
	{
		[]string{`${foo?"${bar}"}`},
		word(ParamExp{
			Param: lit("foo"),
			Exp: &Expansion{
				Op: QUEST,
				Word: word(dblQuoted(
					ParamExp{Param: lit("bar")},
				)),
			},
		}),
	},
	{
		[]string{`${foo:?bar1 bar2}`},
		word(ParamExp{
			Param: lit("foo"),
			Exp: &Expansion{
				Op:   CQUEST,
				Word: litWord("bar1 bar2"),
			},
		}),
	},
	{
		[]string{`${foo%bar}`},
		word(ParamExp{
			Param: lit("foo"),
			Exp: &Expansion{
				Op:   REM,
				Word: litWord("bar"),
			},
		}),
	},
	{
		[]string{`${foo##f*}`},
		word(ParamExp{
			Param: lit("foo"),
			Exp: &Expansion{
				Op:   DHASH,
				Word: litWord("f*"),
			},
		}),
	},
	{
		[]string{`${foo%?}`},
		word(ParamExp{
			Param: lit("foo"),
			Exp: &Expansion{
				Op:   REM,
				Word: litWord("?"),
			},
		}),
	},
	{
		[]string{`${foo::}`},
		word(ParamExp{
			Param: lit("foo"),
			Exp: &Expansion{
				Op:   COLON,
				Word: litWord(":"),
			},
		}),
	},
	{
		[]string{`${foo[bar]}`},
		word(ParamExp{
			Param: lit("foo"),
			Ind: &Index{
				Word: litWord("bar"),
			},
		}),
	},
	{
		[]string{`${foo[bar]-etc}`},
		word(ParamExp{
			Param: lit("foo"),
			Ind: &Index{
				Word: litWord("bar"),
			},
			Exp: &Expansion{
				Op:   SUB,
				Word: litWord("etc"),
			},
		}),
	},
	{
		[]string{`${foo[${bar}]}`},
		word(ParamExp{
			Param: lit("foo"),
			Ind: &Index{
				Word: word(ParamExp{Param: lit("bar")}),
			},
		}),
	},
	{
		[]string{`${foo/b1/b2}`},
		word(ParamExp{
			Param: lit("foo"),
			Repl: &Replace{
				Orig: litWord("b1"),
				With: litWord("b2"),
			},
		}),
	},
	{
		[]string{`${foo/a b/c d}`},
		word(ParamExp{
			Param: lit("foo"),
			Repl: &Replace{
				Orig: litWord("a b"),
				With: litWord("c d"),
			},
		}),
	},
	{
		[]string{`${foo/[/]}`},
		word(ParamExp{
			Param: lit("foo"),
			Repl: &Replace{
				Orig: litWord("["),
				With: litWord("]"),
			},
		}),
	},
	{
		[]string{`${foo/bar/b/a/r}`},
		word(ParamExp{
			Param: lit("foo"),
			Repl: &Replace{
				Orig: litWord("bar"),
				With: litWord("b/a/r"),
			},
		}),
	},
	{
		[]string{`${foo//b1/b2}`},
		word(ParamExp{
			Param: lit("foo"),
			Repl: &Replace{
				All:  true,
				Orig: litWord("b1"),
				With: litWord("b2"),
			},
		}),
	},
	{
		[]string{
			`${foo//#/}`,
			`${foo//#}`,
		},
		word(ParamExp{
			Param: lit("foo"),
			Repl: &Replace{
				All:  true,
				Orig: litWord("#"),
			},
		}),
	},
	{
		[]string{`${#foo}`},
		word(ParamExp{
			Length: true,
			Param:  lit("foo"),
		}),
	},
	{
		[]string{`${#} ${#?}`},
		cmd(
			word(ParamExp{Length: true}),
			word(ParamExp{Length: true, Param: lit("?")}),
		),
	},
	{
		[]string{`"${foo}"`},
		word(dblQuoted(ParamExp{Param: lit("foo")})),
	},
	{
		[]string{`"(foo)"`},
		word(dblQuoted(lit("(foo)"))),
	},
	{
		[]string{`"${foo}>"`},
		word(dblQuoted(
			ParamExp{Param: lit("foo")},
			lit(">"),
		)),
	},
	{
		[]string{`"$(foo)"`},
		word(dblQuoted(cmdSubst(litStmt("foo")))),
	},
	{
		[]string{`"$(foo bar)"`, `"$(foo  bar)"`},
		word(dblQuoted(cmdSubst(litStmt("foo", "bar")))),
	},
	{
		[]string{"\"`foo`\""},
		word(dblQuoted(bckQuoted(litStmt("foo")))),
	},
	{
		[]string{"\"`foo bar`\"", "\"`foo  bar`\""},
		word(dblQuoted(bckQuoted(litStmt("foo", "bar")))),
	},
	{
		[]string{`'${foo}'`},
		word(sglQuoted("${foo}")),
	},
	{
		[]string{"$(())"},
		word(arithmExpr(nil)),
	},
	{
		[]string{"$((1))"},
		word(arithmExpr(litWord("1"))),
	},
	{
		[]string{"$((1 + 3))", "$((1+3))"},
		word(arithmExpr(BinaryExpr{
			Op: ADD,
			X:  litWord("1"),
			Y:  litWord("3"),
		})),
	},
	{
		[]string{"$((5 * 2 - 1))", "$((5*2-1))"},
		word(arithmExpr(BinaryExpr{
			Op: MUL,
			X:  litWord("5"),
			Y: BinaryExpr{
				Op: SUB,
				X:  litWord("2"),
				Y:  litWord("1"),
			},
		})),
	},
	{
		[]string{"$(($i + 13))"},
		word(arithmExpr(BinaryExpr{
			Op: ADD,
			X:  word(litParamExp("i")),
			Y:  litWord("13"),
		})),
	},
	{
		[]string{"$((3 + $((4))))"},
		word(arithmExpr(BinaryExpr{
			Op: ADD,
			X:  litWord("3"),
			Y:  word(arithmExpr(litWord("4"))),
		})),
	},
	{
		[]string{"$((3 & 7))"},
		word(arithmExpr(BinaryExpr{
			Op: AND,
			X:  litWord("3"),
			Y:  litWord("7"),
		})),
	},
	{
		[]string{`"$((1 + 3))"`},
		word(dblQuoted(arithmExpr(BinaryExpr{
			Op: ADD,
			X:  litWord("1"),
			Y:  litWord("3"),
		}))),
	},
	{
		[]string{"$((2 ** 10))"},
		word(arithmExpr(BinaryExpr{
			Op: POW,
			X:  litWord("2"),
			Y:  litWord("10"),
		})),
	},
	{
		[]string{`$(((1) + 3))`},
		word(arithmExpr(BinaryExpr{
			Op: ADD,
			X:  parenExpr(litWord("1")),
			Y:  litWord("3"),
		})),
	},
	{
		[]string{`$((1 + (3 - 2)))`},
		word(arithmExpr(BinaryExpr{
			Op: ADD,
			X:  litWord("1"),
			Y: parenExpr(BinaryExpr{
				Op: SUB,
				X:  litWord("3"),
				Y:  litWord("2"),
			}),
		})),
	},
	{
		[]string{`$((-(1)))`},
		word(arithmExpr(UnaryExpr{
			Op: SUB,
			X:  parenExpr(litWord("1")),
		})),
	},
	{
		[]string{`$((i++))`},
		word(arithmExpr(UnaryExpr{
			Op:   INC,
			Post: true,
			X:    litWord("i"),
		})),
	},
	{
		[]string{`$((--i))`},
		word(arithmExpr(UnaryExpr{
			Op: DEC,
			X:  litWord("i"),
		})),
	},
	{
		[]string{`$((!i))`},
		word(arithmExpr(UnaryExpr{
			Op: NOT,
			X:  litWord("i"),
		})),
	},
	{
		[]string{`$((-!+i))`},
		word(arithmExpr(UnaryExpr{
			Op: SUB,
			X: UnaryExpr{
				Op: NOT,
				X: UnaryExpr{
					Op: ADD,
					X:  litWord("i"),
				},
			},
		})),
	},
	{
		[]string{`$((!!i))`},
		word(arithmExpr(UnaryExpr{
			Op: NOT,
			X: UnaryExpr{
				Op: NOT,
				X:  litWord("i"),
			},
		})),
	},
	{
		[]string{`$((1 < 3))`},
		word(arithmExpr(BinaryExpr{
			Op: LSS,
			X:  litWord("1"),
			Y:  litWord("3"),
		})),
	},
	{
		[]string{`$((i = 2))`, `$((i=2))`},
		word(arithmExpr(BinaryExpr{
			Op: ASSIGN,
			X:  litWord("i"),
			Y:  litWord("2"),
		})),
	},
	{
		[]string{"$((a = 2, b = 3))"},
		word(arithmExpr(BinaryExpr{
			Op: ASSIGN,
			X:  litWord("a"),
			Y: BinaryExpr{
				Op: COMMA,
				X:  litWord("2"),
				Y: BinaryExpr{
					Op: ASSIGN,
					X:  litWord("b"),
					Y:  litWord("3"),
				},
			},
		})),
	},
	{
		[]string{"$((i *= 3))"},
		word(arithmExpr(BinaryExpr{
			Op: MUL_ASSIGN,
			X:  litWord("i"),
			Y:  litWord("3"),
		})),
	},
	{
		[]string{"$((2 >= 10))"},
		word(arithmExpr(BinaryExpr{
			Op: GEQ,
			X:  litWord("2"),
			Y:  litWord("10"),
		})),
	},
	{
		[]string{"$((foo ? b1 : b2))"},
		word(arithmExpr(BinaryExpr{
			Op: QUEST,
			X:  litWord("foo"),
			Y: BinaryExpr{
				Op: COLON,
				X:  litWord("b1"),
				Y:  litWord("b2"),
			},
		})),
	},
	{
		[]string{`$((a = (1 + 2)))`},
		word(arithmExpr(BinaryExpr{
			Op: ASSIGN,
			X:  litWord("a"),
			Y: parenExpr(BinaryExpr{
				Op: ADD,
				X:  litWord("1"),
				Y:  litWord("2"),
			}),
		})),
	},
	{
		[]string{"foo$"},
		word(lit("foo"), lit("$")),
	},
	{
		[]string{`$'foo'`},
		word(Quoted{Quote: DOLLSQ, Parts: lits("foo")}),
	},
	{
		[]string{`$'foo$'`},
		word(Quoted{Quote: DOLLSQ, Parts: lits("foo$")}),
	},
	{
		[]string{`$'foo bar'`},
		word(Quoted{Quote: DOLLSQ, Parts: lits(`foo bar`)}),
	},
	{
		[]string{`$'f\'oo'`},
		word(Quoted{Quote: DOLLSQ, Parts: lits(`f\'oo`)}),
	},
	{
		[]string{`$"foo"`},
		word(Quoted{Quote: DOLLDQ, Parts: lits("foo")}),
	},
	{
		[]string{`$"foo$"`},
		word(Quoted{Quote: DOLLDQ, Parts: lits("foo$")}),
	},
	{
		[]string{`$"foo bar"`},
		word(Quoted{Quote: DOLLDQ, Parts: lits(`foo bar`)}),
	},
	{
		[]string{`$"f\"oo"`},
		word(Quoted{Quote: DOLLDQ, Parts: lits(`f\"oo`)}),
	},
	{
		[]string{`"foo$"`},
		word(dblQuoted(lit("foo$"))),
	},
	{
		[]string{`"foo$$"`},
		word(dblQuoted(lit("foo"), litParamExp("$"))),
	},
	{
		[]string{"`foo$`"},
		word(bckQuoted(
			stmt(cmd(word(lit("foo"), lit("$")))),
		)),
	},
	{
		[]string{"foo$bar"},
		word(lit("foo"), litParamExp("bar")),
	},
	{
		[]string{"foo$(bar)"},
		word(lit("foo"), cmdSubst(litStmt("bar"))),
	},
	{
		[]string{"foo${bar}"},
		word(lit("foo"), ParamExp{Param: lit("bar")}),
	},
	{
		[]string{"'foo${bar'"},
		word(sglQuoted("foo${bar")),
	},
	{
		[]string{"(foo)\nbar", "(foo); bar"},
		[]Node{
			subshell(litStmt("foo")),
			litCmd("bar"),
		},
	},
	{
		[]string{"foo\n(bar)", "foo; (bar)"},
		[]Node{
			litCmd("foo"),
			subshell(litStmt("bar")),
		},
	},
	{
		[]string{"foo\n(bar)", "foo; (bar)"},
		[]Node{
			litCmd("foo"),
			subshell(litStmt("bar")),
		},
	},
	{
		[]string{
			"case $i in 1) foo ;; 2 | 3*) bar ;; esac",
			"case $i in 1) foo;; 2 | 3*) bar; esac",
			"case $i in (1) foo;; 2 | 3*) bar;; esac",
			"case $i\nin\n#etc\n1)\nfoo\n;;\n2 | 3*)\nbar\n;;\nesac",
		},
		CaseStmt{
			Word: word(litParamExp("i")),
			List: []PatternList{
				{
					Patterns: litWords("1"),
					Stmts:    litStmts("foo"),
				},
				{
					Patterns: litWords("2", "3*"),
					Stmts:    litStmts("bar"),
				},
			},
		},
	},
	{
		[]string{"foo | while read a; do b; done"},
		BinaryExpr{
			Op: OR,
			X:  litStmt("foo"),
			Y: stmt(WhileStmt{
				Cond: StmtCond{Stmts: []Stmt{
					litStmt("read", "a"),
				}},
				DoStmts: litStmts("b"),
			}),
		},
	},
	{
		[]string{"while read l; do foo || bar; done"},
		WhileStmt{
			Cond: StmtCond{Stmts: []Stmt{litStmt("read", "l")}},
			DoStmts: stmts(BinaryExpr{
				Op: LOR,
				X:  litStmt("foo"),
				Y:  litStmt("bar"),
			}),
		},
	},
	{
		[]string{"echo if while"},
		litCmd("echo", "if", "while"),
	},
	{
		[]string{"${foo}if"},
		word(ParamExp{Param: lit("foo")}, lit("if")),
	},
	{
		[]string{"$if"},
		word(litParamExp("if")),
	},
	{
		[]string{"if; then; fi", "if\nthen\nfi"},
		IfStmt{},
	},
	{
		[]string{"if; then a=; fi", "if; then a=\nfi"},
		IfStmt{
			ThenStmts: []Stmt{
				{Assigns: []Assign{
					{Name: lit("a")},
				}},
			},
		},
	},
	{
		[]string{"if; then >f; fi", "if; then >f\nfi"},
		IfStmt{
			ThenStmts: []Stmt{
				{Redirs: []Redirect{
					{Op: GTR, Word: litWord("f")},
				}},
			},
		},
	},
	{
		[]string{"a=b\nc=d", "a=b; c=d"},
		[]Stmt{
			{Assigns: []Assign{
				{Name: lit("a"), Value: litWord("b")},
			}},
			{Assigns: []Assign{
				{Name: lit("c"), Value: litWord("d")},
			}},
		},
	},
	{
		[]string{"while; do; done", "while\ndo\ndone"},
		WhileStmt{},
	},
	{
		[]string{"while; do; done", "while\ndo\n#foo\ndone"},
		WhileStmt{},
	},
	{
		[]string{"until; do; done", "until\ndo\ndone"},
		UntilStmt{},
	},
	{
		[]string{"for i; do; done", "for i\ndo\ndone"},
		ForStmt{Cond: WordIter{Name: lit("i")}},
	},
	{
		[]string{"case i in; esac"},
		CaseStmt{Word: litWord("i")},
	},
	{
		[]string{"foo && write | read"},
		BinaryExpr{
			Op: LAND,
			X:  litStmt("foo"),
			Y: stmt(BinaryExpr{
				Op: OR,
				X:  litStmt("write"),
				Y:  litStmt("read"),
			}),
		},
	},
	{
		[]string{"write | read && bar"},
		BinaryExpr{
			Op: LAND,
			X: stmt(BinaryExpr{
				Op: OR,
				X:  litStmt("write"),
				Y:  litStmt("read"),
			}),
			Y: litStmt("bar"),
		},
	},
	{
		[]string{"foo >f | bar"},
		BinaryExpr{
			Op: OR,
			X: Stmt{
				Node: litCmd("foo"),
				Redirs: []Redirect{
					{Op: GTR, Word: litWord("f")},
				},
			},
			Y: litStmt("bar"),
		},
	},
	{
		[]string{"(foo) >f | bar"},
		BinaryExpr{
			Op: OR,
			X: Stmt{
				Node: subshell(litStmt("foo")),
				Redirs: []Redirect{
					{Op: GTR, Word: litWord("f")},
				},
			},
			Y: litStmt("bar"),
		},
	},
	{
		[]string{"foo | >f"},
		BinaryExpr{
			Op: OR,
			X:  litStmt("foo"),
			Y: Stmt{
				Redirs: []Redirect{
					{Op: GTR, Word: litWord("f")},
				},
			},
		},
	},
	{
		[]string{"declare alone foo=bar"},
		DeclStmt{
			Assigns: []Assign{
				{Value: litWord("alone")},
				{Name: lit("foo"), Value: litWord("bar")},
			},
		},
	},
	{
		[]string{"declare -a -bc foo=bar"},
		DeclStmt{
			Opts: litWords("-a", "-bc"),
			Assigns: []Assign{
				{Name: lit("foo"), Value: litWord("bar")},
			},
		},
	},
	{
		[]string{"declare -a foo=(b1 `b2`)"},
		DeclStmt{
			Opts: litWords("-a"),
			Assigns: []Assign{
				{Name: lit("foo"), Value: word(
					ArrayExpr{List: []Word{
						litWord("b1"),
						word(bckQuoted(litStmt("b2"))),
					}},
				)},
			},
		},
	},
	{
		[]string{"local -a foo=(b1 `b2`)"},
		DeclStmt{
			Local: true,
			Opts:  litWords("-a"),
			Assigns: []Assign{{
				Name: lit("foo"),
				Value: word(
					ArrayExpr{List: []Word{
						litWord("b1"),
						word(bckQuoted(litStmt("b2"))),
					}},
				),
			}},
		},
	},
	{
		[]string{"eval a=b foo"},
		EvalStmt{Stmt: Stmt{
			Node:    litCmd("foo"),
			Assigns: []Assign{{Name: lit("a"), Value: litWord("b")}},
		}},
	},
	{
		[]string{`let i++`},
		LetStmt{Exprs: []Node{
			UnaryExpr{
				Op:   INC,
				Post: true,
				X:    litWord("i"),
			},
		}},
	},
	{
		[]string{`let a++ b++ c +d`},
		LetStmt{Exprs: []Node{
			UnaryExpr{
				Op:   INC,
				Post: true,
				X:    litWord("a"),
			},
			UnaryExpr{
				Op:   INC,
				Post: true,
				X:    litWord("b"),
			},
			litWord("c"),
			UnaryExpr{
				Op: ADD,
				X:  litWord("d"),
			},
		}},
	},
	{
		[]string{`let "--i"`},
		LetStmt{Exprs: []Node{
			word(dblQuoted(lit("--i"))),
		}},
	},
	{
		[]string{
			`let a=(1 + 2) b=3+4`,
			`let a=(1+2) b=3+4`,
		},
		LetStmt{Exprs: []Node{
			BinaryExpr{
				Op: ASSIGN,
				X:  litWord("a"),
				Y: parenExpr(BinaryExpr{
					Op: ADD,
					X:  litWord("1"),
					Y:  litWord("2"),
				}),
			},
			BinaryExpr{
				Op: ASSIGN,
				X:  litWord("b"),
				Y: BinaryExpr{
					Op: ADD,
					X:  litWord("3"),
					Y:  litWord("4"),
				},
			},
		}},
	},
	{
		[]string{"(foo-bar)"},
		subshell(litStmt("foo-bar")),
	},
	{
		[]string{
			"let i++\nbar",
			"let i++; bar",
		},
		[]Stmt{
			stmt(LetStmt{Exprs: []Node{
				UnaryExpr{
					Op:   INC,
					Post: true,
					X:    litWord("i"),
				},
			}}),
			litStmt("bar"),
		},
	},
	{
		[]string{
			"let i++\nfoo=(bar)",
			"let i++; foo=(bar)",
		},
		[]Stmt{
			stmt(LetStmt{Exprs: []Node{
				UnaryExpr{
					Op:   INC,
					Post: true,
					X:    litWord("i"),
				},
			}}),
			{
				Assigns: []Assign{{
					Name: lit("foo"),
					Value: word(
						ArrayExpr{List: litWords("bar")},
					),
				}},
			},
		},
	},
	{
		[]string{"a=(b c) foo"},
		Stmt{
			Assigns: []Assign{{
				Name: lit("a"),
				Value: word(
					ArrayExpr{List: litWords("b", "c")},
				),
			}},
			Node: litCmd("foo"),
		},
	},
	{
		[]string{"a=(b c) foo", "a=(\nb\nc\n) foo"},
		Stmt{
			Assigns: []Assign{{
				Name: lit("a"),
				Value: word(
					ArrayExpr{List: litWords("b", "c")},
				),
			}},
			Node: litCmd("foo"),
		},
	},
	{
		[]string{"a+=1 b+=(2 3)"},
		Stmt{
			Assigns: []Assign{
				{
					Append: true,
					Name:   lit("a"),
					Value:  litWord("1"),
				},
				{
					Append: true,
					Name:   lit("b"),
					Value: word(
						ArrayExpr{List: litWords("2", "3")},
					),
				},
			},
		},
	},
	{
		[]string{"<<EOF | b\nfoo\nEOF", "<<EOF|b;\nfoo"},
		BinaryExpr{
			Op: OR,
			X: Stmt{Redirs: []Redirect{{
				Op:   SHL,
				Word: litWord("EOF"),
				Hdoc: litRef("foo\n"),
			}}},
			Y: litStmt("b"),
		},
	},
	{
		[]string{"<<EOF1 <<EOF2 | c && d\nEOF1\nEOF2"},
		BinaryExpr{
			Op: LAND,
			X: stmt(BinaryExpr{
				Op: OR,
				X: Stmt{Redirs: []Redirect{
					{
						Op:   SHL,
						Word: litWord("EOF1"),
						Hdoc: litRef(""),
					},
					{
						Op:   SHL,
						Word: litWord("EOF2"),
						Hdoc: litRef(""),
					},
				}},
				Y: litStmt("c"),
			}),
			Y: litStmt("d"),
		},
	},
}

func fullProg(v interface{}) (f File) {
	switch x := v.(type) {
	case []Stmt:
		f.Stmts = x
	case Stmt:
		f.Stmts = append(f.Stmts, x)
	case []Node:
		for _, n := range x {
			f.Stmts = append(f.Stmts, stmt(n))
		}
	case Word:
		return fullProg(cmd(x))
	case Node:
		return fullProg(stmt(x))
	}
	return
}

func emptyNode(n Node) bool {
	s := strings.TrimRight(strFprint(n, 0), "\n")
	return len(s) == 0
}

func setPosRecurse(tb testing.TB, v interface{}, to Pos, diff bool) Node {
	setPos := func(p *Pos) {
		if diff && *p == to {
			tb.Fatalf("Pos() in %T is already %v", v, to)
		}
		*p = to
	}
	checkPos := func(n Node) {
		if n == nil {
			return
		}
		if n.Pos() != to {
			tb.Fatalf("Found unexpected Pos() in %T", n)
		}
		if to.Line == 0 {
			if n.End() != to {
				tb.Fatalf("Found unexpected End() in %T", n)
			}
			return
		}
		if posGreater(n.Pos(), n.End()) {
			tb.Fatalf("Found End() before Pos() in %T", n)
		}
		if !emptyNode(n) && n.Pos() == n.End() {
			fmt.Printf("%q\n", strFprint(n, 0))
			tb.Fatalf("Found End() at Pos() in %T %#v", n, n)
		}
	}
	recurse := func(v interface{}) Node {
		n := setPosRecurse(tb, v, to, diff)
		checkPos(n)
		return n
	}
	switch x := v.(type) {
	case File:
		recurse(x.Stmts)
		checkPos(x)
	case []Stmt:
		for i := range x {
			recurse(&x[i])
		}
	case *Stmt:
		setPos(&x.Position)
		x.Node = recurse(x.Node)
		recurse(x.Assigns)
		for i := range x.Redirs {
			r := &x.Redirs[i]
			setPos(&r.OpPos)
			recurse(&r.N)
			recurse(r.Word)
			if r.Hdoc != nil {
				recurse(r.Hdoc)
			}
		}
	case []Assign:
		for i := range x {
			recurse(&x[i].Name)
			recurse(x[i].Value)
			checkPos(x[i])
		}
	case Stmt:
		recurse(&x)
		return x
	case Command:
		recurse(x.Args)
		return x
	case []Word:
		for _, w := range x {
			recurse(w)
		}
	case Word:
		recurse(x.Parts)
		return x
	case []Node:
		for i := range x {
			recurse(&x[i])
		}
	case *Node:
		*x = recurse(*x)
	case *Lit:
		setPos(&x.ValuePos)
	case Lit:
		recurse(&x)
		return x
	case Subshell:
		setPos(&x.Lparen)
		setPos(&x.Rparen)
		recurse(x.Stmts)
		return x
	case Block:
		setPos(&x.Lbrace)
		setPos(&x.Rbrace)
		recurse(x.Stmts)
		return x
	case IfStmt:
		setPos(&x.If)
		setPos(&x.Then)
		setPos(&x.Fi)
		recurse(&x.Cond)
		recurse(x.ThenStmts)
		for i := range x.Elifs {
			setPos(&x.Elifs[i].Elif)
			setPos(&x.Elifs[i].Then)
			recurse(x.Elifs[i].Cond)
			recurse(x.Elifs[i].ThenStmts)
		}
		if len(x.ElseStmts) > 0 {
			setPos(&x.Else)
			recurse(x.ElseStmts)
		}
		return x
	case StmtCond:
		recurse(x.Stmts)
		return x
	case CStyleCond:
		setPos(&x.Lparen)
		setPos(&x.Rparen)
		recurse(&x.Cond)
		return x
	case WhileStmt:
		setPos(&x.While)
		setPos(&x.Do)
		setPos(&x.Done)
		recurse(&x.Cond)
		recurse(x.DoStmts)
		return x
	case UntilStmt:
		setPos(&x.Until)
		setPos(&x.Do)
		setPos(&x.Done)
		recurse(&x.Cond)
		recurse(x.DoStmts)
		return x
	case ForStmt:
		setPos(&x.For)
		setPos(&x.Do)
		setPos(&x.Done)
		recurse(&x.Cond)
		recurse(x.DoStmts)
		return x
	case WordIter:
		recurse(&x.Name)
		recurse(x.List)
		return x
	case CStyleLoop:
		setPos(&x.Lparen)
		setPos(&x.Rparen)
		recurse(&x.Init)
		recurse(&x.Cond)
		recurse(&x.Post)
		return x
	case SglQuoted:
		setPos(&x.Quote)
		return x
	case Quoted:
		setPos(&x.QuotePos)
		recurse(x.Parts)
		return x
	case UnaryExpr:
		setPos(&x.OpPos)
		recurse(&x.X)
		return x
	case BinaryExpr:
		setPos(&x.OpPos)
		recurse(&x.X)
		recurse(&x.Y)
		return x
	case FuncDecl:
		setPos(&x.Position)
		recurse(&x.Name)
		recurse(&x.Body)
		return x
	case ParamExp:
		setPos(&x.Dollar)
		recurse(&x.Param)
		if x.Ind != nil {
			recurse(x.Ind.Word)
		}
		if x.Repl != nil {
			recurse(x.Repl.Orig)
			recurse(x.Repl.With)
		}
		if x.Exp != nil {
			recurse(x.Exp.Word)
		}
		return x
	case ArithmExpr:
		setPos(&x.Dollar)
		setPos(&x.Rparen)
		recurse(&x.X)
		return x
	case ParenExpr:
		setPos(&x.Lparen)
		setPos(&x.Rparen)
		recurse(&x.X)
		return x
	case CmdSubst:
		setPos(&x.Left)
		setPos(&x.Right)
		recurse(x.Stmts)
		return x
	case CaseStmt:
		setPos(&x.Case)
		setPos(&x.Esac)
		recurse(x.Word)
		for i := range x.List {
			pl := &x.List[i]
			setPos(&pl.Dsemi)
			recurse(pl.Patterns)
			recurse(pl.Stmts)
		}
		return x
	case DeclStmt:
		setPos(&x.Declare)
		recurse(x.Opts)
		recurse(x.Assigns)
		return x
	case EvalStmt:
		setPos(&x.Eval)
		recurse(&x.Stmt)
		return x
	case LetStmt:
		setPos(&x.Let)
		recurse(x.Exprs)
		return x
	case ArrayExpr:
		setPos(&x.Lparen)
		setPos(&x.Rparen)
		recurse(x.List)
		return x
	case CmdInput:
		setPos(&x.Lss)
		setPos(&x.Rparen)
		recurse(x.Stmts)
		return x
	}
	return nil
}

func TestNodePos(t *testing.T) {
	defaultPos = Pos{
		Line:   12,
		Column: 34,
	}
	for i, c := range astTests {
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			want := c.ast.(File)
			setPosRecurse(t, want, defaultPos, true)
		})
	}
}
