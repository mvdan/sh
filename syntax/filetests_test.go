// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"reflect"
	"strings"
	"testing"
)

func prepareTest(c *testCase) {
	c.common = fullProg(c.common)
	c.bash = fullProg(c.bash)
	c.posix = fullProg(c.posix)
	if c.minBash < 42 {
		c.minBash = 42
	}
	if f, ok := c.common.(*File); ok && f != nil {
		c.All = append(c.All, f)
		c.Bash = f
		c.Posix = f
	}
	if f, ok := c.bash.(*File); ok && f != nil {
		c.All = append(c.All, f)
		c.Bash = f
	}
	if f, ok := c.posix.(*File); ok && f != nil {
		c.All = append(c.All, f)
		c.Posix = f
	}
}

func init() {
	for i := range fileTests {
		prepareTest(&fileTests[i])
	}
	for i := range fileTestsNoPrint {
		prepareTest(&fileTestsNoPrint[i])
	}
}

func lit(s string) *Lit         { return &Lit{Value: s} }
func word(ps ...WordPart) *Word { return &Word{Parts: ps} }
func litWord(s string) *Word    { return word(lit(s)) }
func litWords(strs ...string) []*Word {
	l := make([]*Word, 0, len(strs))
	for _, s := range strs {
		l = append(l, litWord(s))
	}
	return l
}

func call(words ...*Word) *CallExpr    { return &CallExpr{Args: words} }
func litCall(strs ...string) *CallExpr { return call(litWords(strs...)...) }

func stmt(cmd Command) *Stmt { return &Stmt{Cmd: cmd} }
func stmts(cmds ...Command) []*Stmt {
	l := make([]*Stmt, len(cmds))
	for i, cmd := range cmds {
		l[i] = stmt(cmd)
	}
	return l
}

func litStmt(strs ...string) *Stmt { return stmt(litCall(strs...)) }
func litStmts(strs ...string) []*Stmt {
	l := make([]*Stmt, len(strs))
	for i, s := range strs {
		l[i] = litStmt(s)
	}
	return l
}

func sglQuoted(s string) *SglQuoted        { return &SglQuoted{Value: s} }
func sglDQuoted(s string) *SglQuoted       { return &SglQuoted{Dollar: true, Value: s} }
func dblQuoted(ps ...WordPart) *DblQuoted  { return &DblQuoted{Parts: ps} }
func dblDQuoted(ps ...WordPart) *DblQuoted { return &DblQuoted{Dollar: true, Parts: ps} }
func block(sts ...*Stmt) *Block            { return &Block{Stmts: sts} }
func subshell(sts ...*Stmt) *Subshell      { return &Subshell{Stmts: sts} }
func arithmExp(e ArithmExpr) *ArithmExp    { return &ArithmExp{X: e} }
func arithmExpBr(e ArithmExpr) *ArithmExp  { return &ArithmExp{Bracket: true, X: e} }
func arithmCmd(e ArithmExpr) *ArithmCmd    { return &ArithmCmd{X: e} }
func parenArit(e ArithmExpr) *ParenArithm  { return &ParenArithm{X: e} }
func parenTest(e TestExpr) *ParenTest      { return &ParenTest{X: e} }

func cmdSubst(sts ...*Stmt) *CmdSubst { return &CmdSubst{Stmts: sts} }
func litParamExp(s string) *ParamExp {
	return &ParamExp{Short: true, Param: lit(s)}
}
func letClause(exps ...ArithmExpr) *LetClause {
	return &LetClause{Exprs: exps}
}

type testCase struct {
	Strs                []string
	common, bash, posix interface{}
	All                 []*File
	Bash, Posix         *File
	minBash             int
}

var fileTests = []testCase{
	{
		Strs:   []string{"", " ", "\t", "\n \n", "\r \r\n"},
		common: &File{},
	},
	{
		Strs:   []string{"", "# foo", "# foo ( bar", "# foo'bar"},
		common: &File{},
	},
	{
		Strs:   []string{"foo", "foo ", " foo", "foo # bar"},
		common: litWord("foo"),
	},
	{
		Strs:   []string{`\`},
		common: litWord(`\`),
	},
	{
		Strs:   []string{`foo\`, "f\\\noo\\"},
		common: litWord(`foo\`),
	},
	{
		Strs:   []string{`foo\a`, "f\\\noo\\a"},
		common: litWord(`foo\a`),
	},
	{
		Strs: []string{
			"foo\nbar",
			"foo; bar;",
			"foo;bar;",
			"\nfoo\nbar\n",
			"foo\r\nbar\r\n",
		},
		common: litStmts("foo", "bar"),
	},
	{
		Strs:   []string{"foo a b", " foo  a  b ", "foo \\\n a b"},
		common: litCall("foo", "a", "b"),
	},
	{
		Strs:   []string{"foobar", "foo\\\nbar", "foo\\\nba\\\nr"},
		common: litWord("foobar"),
	},
	{
		Strs:   []string{"foo", "foo \\\n"},
		common: litWord("foo"),
	},
	{
		Strs:   []string{"foo'bar'"},
		common: word(lit("foo"), sglQuoted("bar")),
	},
	{
		Strs:   []string{"(foo)", "(foo;)", "(\nfoo\n)"},
		common: subshell(litStmt("foo")),
	},
	{
		Strs:   []string{"(\n\tfoo\n\tbar\n)", "(foo; bar)"},
		common: subshell(litStmt("foo"), litStmt("bar")),
	},
	{
		Strs:   []string{"{ foo; }", "{\nfoo\n}"},
		common: block(litStmt("foo")),
	},
	{
		Strs: []string{
			"if a; then b; fi",
			"if a\nthen\nb\nfi",
			"if a \nthen\nb\nfi",
		},
		common: &IfClause{
			CondStmts: litStmts("a"),
			ThenStmts: litStmts("b"),
		},
	},
	{
		Strs: []string{
			"if a; then b; else c; fi",
			"if a\nthen b\nelse\nc\nfi",
		},
		common: &IfClause{
			CondStmts: litStmts("a"),
			ThenStmts: litStmts("b"),
			ElseStmts: litStmts("c"),
		},
	},
	{
		Strs: []string{
			"if a; then a; elif b; then b; elif c; then c; else d; fi",
			"if a\nthen a\nelif b\nthen b\nelif c\nthen c\nelse\nd\nfi",
		},
		common: &IfClause{
			CondStmts: litStmts("a"),
			ThenStmts: litStmts("a"),
			Elifs: []*Elif{
				{
					CondStmts: litStmts("b"),
					ThenStmts: litStmts("b"),
				},
				{
					CondStmts: litStmts("c"),
					ThenStmts: litStmts("c"),
				},
			},
			ElseStmts: litStmts("d"),
		},
	},
	{
		Strs: []string{
			"if\n\ta1\n\ta2 foo\n\ta3 bar\nthen b; fi",
			"if a1; a2 foo; a3 bar; then b; fi",
		},
		common: &IfClause{
			CondStmts: []*Stmt{
				litStmt("a1"),
				litStmt("a2", "foo"),
				litStmt("a3", "bar"),
			},
			ThenStmts: litStmts("b"),
		},
	},
	{
		Strs: []string{`((a == 2))`},
		bash: stmt(arithmCmd(&BinaryArithm{
			Op: Eql,
			X:  litWord("a"),
			Y:  litWord("2"),
		})),
		posix: subshell(stmt(subshell(litStmt("a", "==", "2")))),
	},
	{
		Strs: []string{"if ((1 > 2)); then b; fi"},
		bash: &IfClause{
			CondStmts: stmts(arithmCmd(&BinaryArithm{
				Op: Gtr,
				X:  litWord("1"),
				Y:  litWord("2"),
			})),
			ThenStmts: litStmts("b"),
		},
	},
	{
		Strs: []string{
			"while a; do b; done",
			"wh\\\nile a; do b; done",
			"while a\ndo\nb\ndone",
		},
		common: &WhileClause{
			CondStmts: litStmts("a"),
			DoStmts:   litStmts("b"),
		},
	},
	{
		Strs: []string{"while { a; }; do b; done", "while { a; } do b; done"},
		common: &WhileClause{
			CondStmts: stmts(block(litStmt("a"))),
			DoStmts:   litStmts("b"),
		},
	},
	{
		Strs: []string{"while (a); do b; done", "while (a) do b; done"},
		common: &WhileClause{
			CondStmts: stmts(subshell(litStmt("a"))),
			DoStmts:   litStmts("b"),
		},
	},
	{
		Strs: []string{"while ((1 > 2)); do b; done"},
		bash: &WhileClause{
			CondStmts: stmts(arithmCmd(&BinaryArithm{
				Op: Gtr,
				X:  litWord("1"),
				Y:  litWord("2"),
			})),
			DoStmts: litStmts("b"),
		},
	},
	{
		Strs: []string{"until a; do b; done", "until a\ndo\nb\ndone"},
		common: &UntilClause{
			CondStmts: litStmts("a"),
			DoStmts:   litStmts("b"),
		},
	},
	{
		Strs: []string{
			"for i; do foo; done",
			"for i in; do foo; done",
		},
		common: &ForClause{
			Loop:    &WordIter{Name: lit("i")},
			DoStmts: litStmts("foo"),
		},
	},
	{
		Strs: []string{
			"for i in 1 2 3; do echo $i; done",
			"for i in 1 2 3\ndo echo $i\ndone",
			"for i in 1 2 3 #foo\ndo echo $i\ndone",
		},
		common: &ForClause{
			Loop: &WordIter{
				Name: lit("i"),
				List: litWords("1", "2", "3"),
			},
			DoStmts: stmts(call(
				litWord("echo"),
				word(litParamExp("i")),
			)),
		},
	},
	{
		Strs: []string{
			"for ((i = 0; i < 10; i++)); do echo $i; done",
			"for ((i=0;i<10;i++)) do echo $i; done",
			"for (( i = 0 ; i < 10 ; i++ ))\ndo echo $i\ndone",
		},
		bash: &ForClause{
			Loop: &CStyleLoop{
				Init: &BinaryArithm{
					Op: Assgn,
					X:  litWord("i"),
					Y:  litWord("0"),
				},
				Cond: &BinaryArithm{
					Op: Lss,
					X:  litWord("i"),
					Y:  litWord("10"),
				},
				Post: &UnaryArithm{
					Op:   Inc,
					Post: true,
					X:    litWord("i"),
				},
			},
			DoStmts: stmts(call(
				litWord("echo"),
				word(litParamExp("i")),
			)),
		},
	},
	{
		Strs: []string{
			"for (( ; ; )); do foo; done",
			"for ((;;)); do foo; done",
		},
		bash: &ForClause{
			Loop:    &CStyleLoop{},
			DoStmts: litStmts("foo"),
		},
	},
	{
		Strs: []string{
			"for ((i = 0; ; )); do foo; done",
			"for ((i = 0;;)); do foo; done",
		},
		bash: &ForClause{
			Loop: &CStyleLoop{
				Init: &BinaryArithm{
					Op: Assgn,
					X:  litWord("i"),
					Y:  litWord("0"),
				},
			},
			DoStmts: litStmts("foo"),
		},
	},
	{
		Strs: []string{`' ' "foo bar"`},
		common: call(
			word(sglQuoted(" ")),
			word(dblQuoted(lit("foo bar"))),
		),
	},
	{
		Strs:   []string{`"foo \" bar"`},
		common: word(dblQuoted(lit(`foo \" bar`))),
	},
	{
		Strs: []string{"\">foo\" \"\nbar\""},
		common: call(
			word(dblQuoted(lit(">foo"))),
			word(dblQuoted(lit("\nbar"))),
		),
	},
	{
		Strs:   []string{`foo \" bar`},
		common: litCall(`foo`, `\"`, `bar`),
	},
	{
		Strs:   []string{`'"'`},
		common: sglQuoted(`"`),
	},
	{
		Strs:   []string{"'`'"},
		common: sglQuoted("`"),
	},
	{
		Strs:   []string{`"'"`},
		common: dblQuoted(lit("'")),
	},
	{
		Strs:   []string{`""`},
		common: dblQuoted(),
	},
	{
		Strs:   []string{"=a s{s s=s"},
		common: litCall("=a", "s{s", "s=s"),
	},
	{
		Strs: []string{"foo && bar", "foo&&bar", "foo &&\nbar"},
		common: &BinaryCmd{
			Op: AndStmt,
			X:  litStmt("foo"),
			Y:  litStmt("bar"),
		},
	},
	{
		Strs: []string{"foo \\\n\t&& bar"},
		common: &BinaryCmd{
			Op: AndStmt,
			X:  litStmt("foo"),
			Y:  litStmt("bar"),
		},
	},
	{
		Strs: []string{"foo || bar", "foo||bar", "foo ||\nbar"},
		common: &BinaryCmd{
			Op: OrStmt,
			X:  litStmt("foo"),
			Y:  litStmt("bar"),
		},
	},
	{
		Strs: []string{"if a; then b; fi || while a; do b; done"},
		common: &BinaryCmd{
			Op: OrStmt,
			X: stmt(&IfClause{
				CondStmts: litStmts("a"),
				ThenStmts: litStmts("b"),
			}),
			Y: stmt(&WhileClause{
				CondStmts: litStmts("a"),
				DoStmts:   litStmts("b"),
			}),
		},
	},
	{
		Strs: []string{"foo && bar1 || bar2"},
		common: &BinaryCmd{
			Op: AndStmt,
			X:  litStmt("foo"),
			Y: stmt(&BinaryCmd{
				Op: OrStmt,
				X:  litStmt("bar1"),
				Y:  litStmt("bar2"),
			}),
		},
	},
	{
		Strs: []string{"foo | bar", "foo|bar", "foo |\n#etc\nbar"},
		common: &BinaryCmd{
			Op: Pipe,
			X:  litStmt("foo"),
			Y:  litStmt("bar"),
		},
	},
	{
		Strs: []string{"foo | bar | extra"},
		common: &BinaryCmd{
			Op: Pipe,
			X:  litStmt("foo"),
			Y: stmt(&BinaryCmd{
				Op: Pipe,
				X:  litStmt("bar"),
				Y:  litStmt("extra"),
			}),
		},
	},
	{
		Strs: []string{"foo |& bar", "foo|&bar"},
		bash: &BinaryCmd{
			Op: PipeAll,
			X:  litStmt("foo"),
			Y:  litStmt("bar"),
		},
	},
	{
		Strs: []string{
			"foo() {\n\ta\n\tb\n}",
			"foo() { a; b; }",
			"foo ( ) {\na\nb\n}",
		},
		common: &FuncDecl{
			Name: lit("foo"),
			Body: stmt(block(litStmts("a", "b")...)),
		},
	},
	{
		Strs: []string{"foo() { a; }\nbar", "foo() {\na\n}; bar"},
		common: []Command{
			&FuncDecl{
				Name: lit("foo"),
				Body: stmt(block(litStmt("a"))),
			},
			litCall("bar"),
		},
	},
	{
		Strs: []string{"-foo_.,+-bar() { a; }"},
		common: &FuncDecl{
			Name: lit("-foo_.,+-bar"),
			Body: stmt(block(litStmt("a"))),
		},
	},
	{
		Strs: []string{
			"function foo() {\n\ta\n\tb\n}",
			"function foo {\n\ta\n\tb\n}",
			"function foo() { a; b; }",
		},
		bash: &FuncDecl{
			BashStyle: true,
			Name:      lit("foo"),
			Body:      stmt(block(litStmts("a", "b")...)),
		},
	},
	{
		Strs: []string{"function foo() (a)"},
		bash: &FuncDecl{
			BashStyle: true,
			Name:      lit("foo"),
			Body:      stmt(subshell(litStmt("a"))),
		},
	},
	{
		Strs: []string{"a=b foo=$bar foo=start$bar"},
		common: &Stmt{
			Assigns: []*Assign{
				{Name: lit("a"), Value: litWord("b")},
				{Name: lit("foo"), Value: word(litParamExp("bar"))},
				{Name: lit("foo"), Value: word(
					lit("start"),
					litParamExp("bar"),
				)},
			},
		},
	},
	{
		Strs: []string{"a=\"\nbar\""},
		common: &Stmt{
			Assigns: []*Assign{{
				Name:  lit("a"),
				Value: word(dblQuoted(lit("\nbar"))),
			}},
		},
	},
	{
		Strs: []string{"A_3a= foo"},
		common: &Stmt{
			Cmd:     litCall("foo"),
			Assigns: []*Assign{{Name: lit("A_3a")}},
		},
	},
	{
		Strs:   []string{"à=b foo"},
		common: litStmt("à=b", "foo"),
	},
	{
		Strs: []string{
			"foo >a >>b <c",
			"foo > a >> b < c",
			">a >>b <c foo",
		},
		common: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{
				{Op: RdrOut, Word: litWord("a")},
				{Op: AppOut, Word: litWord("b")},
				{Op: RdrIn, Word: litWord("c")},
			},
		},
	},
	{
		Strs: []string{
			"foo bar >a",
			"foo >a bar",
		},
		common: &Stmt{
			Cmd: litCall("foo", "bar"),
			Redirs: []*Redirect{
				{Op: RdrOut, Word: litWord("a")},
			},
		},
	},
	{
		Strs: []string{`>a >\b`},
		common: &Stmt{
			Redirs: []*Redirect{
				{Op: RdrOut, Word: litWord("a")},
				{Op: RdrOut, Word: litWord(`\b`)},
			},
		},
	},
	{
		Strs: []string{">a\n>b", ">a; >b"},
		common: []*Stmt{
			{Redirs: []*Redirect{
				{Op: RdrOut, Word: litWord("a")},
			}},
			{Redirs: []*Redirect{
				{Op: RdrOut, Word: litWord("b")},
			}},
		},
	},
	{
		Strs: []string{"foo1\nfoo2 >r2", "foo1; >r2 foo2"},
		common: []*Stmt{
			litStmt("foo1"),
			{
				Cmd: litCall("foo2"),
				Redirs: []*Redirect{
					{Op: RdrOut, Word: litWord("r2")},
				},
			},
		},
	},
	{
		Strs: []string{"foo >bar$(etc)", "foo >b\\\nar`etc`"},
		common: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{
				{Op: RdrOut, Word: word(
					lit("bar"),
					cmdSubst(litStmt("etc")),
				)},
			},
		},
	},
	{
		Strs: []string{
			"foo <<EOF\nbar\nEOF",
			"foo <<EOF\nbar\n",
			"foo <<EOF \nbar\nEOF",
			"foo <<EOF\t\nbar\nEOF",
		},
		common: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("bar\n"),
			}},
		},
	},
	{
		Strs: []string{"foo <<EOF\n\nbar\nEOF"},
		common: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("\nbar\n"),
			}},
		},
	},
	{
		Strs: []string{"foo <<EOF\nbar\n\nEOF"},
		common: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("bar\n\n"),
			}},
		},
	},
	{
		Strs: []string{"foo <<EOF\n1\n2\n3\nEOF"},
		common: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("1\n2\n3\n"),
			}},
		},
	},
	{
		Strs: []string{"a <<EOF\nfoo$bar\nEOF"},
		common: &Stmt{
			Cmd: litCall("a"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: word(
					lit("foo"),
					litParamExp("bar"),
					lit("\n"),
				),
			}},
		},
	},
	{
		Strs: []string{"a <<EOF\n\"$bar\"\nEOF"},
		common: &Stmt{
			Cmd: litCall("a"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: word(
					lit("\""),
					litParamExp("bar"),
					lit("\"\n"),
				),
			}},
		},
	},
	{
		Strs: []string{"a <<EOF\n$''$bar\nEOF"},
		bash: &Stmt{
			Cmd: litCall("a"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: word(
					lit("$"),
					lit("''"),
					litParamExp("bar"),
					lit("\n"),
				),
			}},
		},
	},
	{
		Strs: []string{
			"a <<EOF\n$(b)\nc\nEOF",
			"a <<EOF\n`b`\nc\nEOF",
		},
		common: &Stmt{
			Cmd: litCall("a"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: word(
					cmdSubst(litStmt("b")),
					lit("\nc\n"),
				),
			}},
		},
	},
	{
		Strs: []string{"a <<EOF\n\\${\nEOF"},
		common: &Stmt{
			Cmd: litCall("a"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("\\${\n"),
			}},
		},
	},
	{
		Strs: []string{
			"{\n\tfoo <<EOF\nbar\nEOF\n}",
			"{ foo <<EOF\nbar\nEOF\n}",
		},
		common: block(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("bar\n"),
			}},
		}),
	},
	{
		Strs: []string{
			"$(\n\tfoo <<EOF\nbar\nEOF\n)",
			"$(foo <<EOF\nbar\nEOF\n)",
		},
		common: cmdSubst(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("bar\n"),
			}},
		}),
	},
	{
		Strs: []string{"$(<foo)", "`<foo`"},
		common: cmdSubst(&Stmt{
			Redirs: []*Redirect{{
				Op:   RdrIn,
				Word: litWord("foo"),
			}},
		}),
	},
	{
		Strs: []string{"foo <<EOF >f\nbar\nEOF"},
		common: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{
				{
					Op:   Hdoc,
					Word: litWord("EOF"),
					Hdoc: litWord("bar\n"),
				},
				{Op: RdrOut, Word: litWord("f")},
			},
		},
	},
	{
		Strs: []string{"foo <<EOF && {\nbar\nEOF\n\tetc\n}"},
		common: &BinaryCmd{
			Op: AndStmt,
			X: &Stmt{
				Cmd: litCall("foo"),
				Redirs: []*Redirect{{
					Op:   Hdoc,
					Word: litWord("EOF"),
					Hdoc: litWord("bar\n"),
				}},
			},
			Y: stmt(block(litStmt("etc"))),
		},
	},
	{
		Strs: []string{
			"$(\n\tfoo\n) <<EOF\nbar\nEOF",
			"<<EOF $(\n\tfoo\n)\nbar\nEOF",
		},
		common: &Stmt{
			Cmd: call(word(cmdSubst(litStmt("foo")))),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("bar\n"),
			}},
		},
	},
	{
		Strs: []string{
			"$(\n\tfoo\n) <<EOF\nbar\nEOF",
			"`\n\tfoo\n` <<EOF\nbar\nEOF",
			"<<EOF `\n\tfoo\n`\nbar\nEOF",
		},
		common: &Stmt{
			Cmd: call(word(cmdSubst(litStmt("foo")))),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("bar\n"),
			}},
		},
	},
	{
		Strs: []string{
			"$((foo)) <<EOF\nbar\nEOF",
			"<<EOF $((\n\tfoo\n))\nbar\nEOF",
		},
		common: &Stmt{
			Cmd: call(word(arithmExp(litWord("foo")))),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("bar\n"),
			}},
		},
	},
	{
		Strs: []string{"if true; then foo <<-EOF\n\tbar\n\tEOF\nfi"},
		common: &IfClause{
			CondStmts: litStmts("true"),
			ThenStmts: []*Stmt{{
				Cmd: litCall("foo"),
				Redirs: []*Redirect{{
					Op:   DashHdoc,
					Word: litWord("EOF"),
					Hdoc: litWord("\tbar\n\t"),
				}},
			}},
		},
	},
	{
		Strs: []string{"if true; then foo <<-EOF\n\tEOF\nfi"},
		common: &IfClause{
			CondStmts: litStmts("true"),
			ThenStmts: []*Stmt{{
				Cmd: litCall("foo"),
				Redirs: []*Redirect{{
					Op:   DashHdoc,
					Word: litWord("EOF"),
					Hdoc: litWord("\t"),
				}},
			}},
		},
	},
	{
		Strs: []string{"foo <<EOF\nbar\nEOF\nfoo2"},
		common: []*Stmt{
			{
				Cmd: litCall("foo"),
				Redirs: []*Redirect{{
					Op:   Hdoc,
					Word: litWord("EOF"),
					Hdoc: litWord("bar\n"),
				}},
			},
			litStmt("foo2"),
		},
	},
	{
		Strs: []string{"foo <<FOOBAR\nbar\nFOOBAR"},
		common: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("FOOBAR"),
				Hdoc: litWord("bar\n"),
			}},
		},
	},
	{
		Strs: []string{
			"foo <<\"EOF\"\nbar\nEOF",
			"foo <<\"EOF\"\nbar\n",
		},
		common: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: word(dblQuoted(lit("EOF"))),
				Hdoc: litWord("bar\n"),
			}},
		},
	},
	{
		Strs: []string{"foo <<'EOF'\n${\nEOF"},
		common: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: word(sglQuoted("EOF")),
				Hdoc: litWord("${\n"),
			}},
		},
	},
	{
		Strs: []string{"foo <<\"EOF\"2\nbar\nEOF2"},
		common: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: word(dblQuoted(lit("EOF")), lit("2")),
				Hdoc: litWord("bar\n"),
			}},
		},
	},
	{
		Strs: []string{"foo <<\\EOF\nbar\nEOF"},
		common: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("\\EOF"),
				Hdoc: litWord("bar\n"),
			}},
		},
	},
	{
		Strs: []string{
			"foo <<-EOF\nbar\nEOF",
			"foo <<- EOF\nbar\nEOF",
		},
		common: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   DashHdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("bar\n"),
			}},
		},
	},
	{
		Strs: []string{
			"foo <<-EOF\n\tEOF",
			"foo <<-EOF\n\t",
		},
		common: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   DashHdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("\t"),
			}},
		},
	},
	{
		Strs: []string{
			"foo <<-EOF\n\tbar\n\tEOF",
			"foo <<-EOF\n\tbar\n\t",
		},
		common: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   DashHdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("\tbar\n\t"),
			}},
		},
	},
	{
		Strs: []string{
			"foo <<-'EOF'\n\tbar\n\tEOF",
			"foo <<-'EOF'\n\tbar\n\t",
		},
		common: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   DashHdoc,
				Word: word(sglQuoted("EOF")),
				Hdoc: litWord("\tbar\n\t"),
			}},
		},
	},
	{
		Strs: []string{
			"f1 <<EOF1\nh1\nEOF1\nf2 <<EOF2\nh2\nEOF2",
			"f1 <<EOF1; f2 <<EOF2\nh1\nEOF1\nh2\nEOF2",
		},
		common: []*Stmt{
			{
				Cmd: litCall("f1"),
				Redirs: []*Redirect{{
					Op:   Hdoc,
					Word: litWord("EOF1"),
					Hdoc: litWord("h1\n"),
				}},
			},
			{
				Cmd: litCall("f2"),
				Redirs: []*Redirect{{
					Op:   Hdoc,
					Word: litWord("EOF2"),
					Hdoc: litWord("h2\n"),
				}},
			},
		},
	},
	{
		Strs: []string{
			"a <<EOF\nfoo\nEOF\nb\nb\nb\nb\nb\nb\nb\nb\nb",
			"a <<EOF;b;b;b;b;b;b;b;b;b\nfoo\nEOF",
		},
		common: []*Stmt{
			{
				Cmd: litCall("a"),
				Redirs: []*Redirect{{
					Op:   Hdoc,
					Word: litWord("EOF"),
					Hdoc: litWord("foo\n"),
				}},
			},
			litStmt("b"), litStmt("b"), litStmt("b"),
			litStmt("b"), litStmt("b"), litStmt("b"),
			litStmt("b"), litStmt("b"), litStmt("b"),
		},
	},
	{
		Strs: []string{
			"foo \"\narg\" <<EOF\nbar\nEOF",
			"foo <<EOF \"\narg\"\nbar\nEOF",
		},
		common: &Stmt{
			Cmd: call(
				litWord("foo"),
				word(dblQuoted(lit("\narg"))),
			),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("bar\n"),
			}},
		},
	},
	{
		Strs: []string{"foo >&2 <&0 2>file <>f2"},
		common: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{
				{Op: DplOut, Word: litWord("2")},
				{Op: DplIn, Word: litWord("0")},
				{Op: RdrOut, N: lit("2"), Word: litWord("file")},
				{Op: RdrInOut, Word: litWord("f2")},
			},
		},
	},
	{
		Strs: []string{"foo &>a &>>b"},
		bash: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{
				{Op: RdrAll, Word: litWord("a")},
				{Op: AppAll, Word: litWord("b")},
			},
		},
		posix: []*Stmt{
			{Cmd: litCall("foo"), Background: true},
			{Redirs: []*Redirect{
				{Op: RdrOut, Word: litWord("a")},
			}, Background: true},
			{Redirs: []*Redirect{
				{Op: AppOut, Word: litWord("b")},
			}},
		},
	},
	{
		Strs: []string{"foo 2>file bar", "2>file foo bar"},
		common: &Stmt{
			Cmd: litCall("foo", "bar"),
			Redirs: []*Redirect{
				{Op: RdrOut, N: lit("2"), Word: litWord("file")},
			},
		},
	},
	{
		Strs: []string{"a >f1\nb >f2", "a >f1; b >f2"},
		common: []*Stmt{
			{
				Cmd:    litCall("a"),
				Redirs: []*Redirect{{Op: RdrOut, Word: litWord("f1")}},
			},
			{
				Cmd:    litCall("b"),
				Redirs: []*Redirect{{Op: RdrOut, Word: litWord("f2")}},
			},
		},
	},
	{
		Strs: []string{"foo >|bar"},
		common: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{
				{Op: ClbOut, Word: litWord("bar")},
			},
		},
	},
	{
		Strs: []string{
			"foo <<<input",
			"foo <<< input",
		},
		bash: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   WordHdoc,
				Word: litWord("input"),
			}},
		},
	},
	{
		Strs: []string{
			`foo <<<"spaced input"`,
			`foo <<< "spaced input"`,
		},
		bash: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   WordHdoc,
				Word: word(dblQuoted(lit("spaced input"))),
			}},
		},
	},
	{
		Strs: []string{"foo >(foo)"},
		bash: call(
			litWord("foo"),
			word(&ProcSubst{
				Op:    CmdOut,
				Stmts: litStmts("foo"),
			}),
		),
	},
	{
		Strs: []string{"foo < <(foo)"},
		bash: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op: RdrIn,
				Word: word(&ProcSubst{
					Op:    CmdIn,
					Stmts: litStmts("foo"),
				}),
			}},
		},
	},
	{
		Strs: []string{"a<(b) c>(d)"},
		bash: call(
			word(lit("a"), &ProcSubst{
				Op:    CmdIn,
				Stmts: litStmts("b"),
			}),
			word(lit("c"), &ProcSubst{
				Op:    CmdOut,
				Stmts: litStmts("d"),
			}),
		),
	},
	{
		Strs:   []string{"!"},
		common: &Stmt{Negated: true},
	},
	{
		Strs: []string{"! foo"},
		common: &Stmt{
			Negated: true,
			Cmd:     litCall("foo"),
		},
	},
	{
		Strs: []string{"foo &\nbar", "foo & bar", "foo&bar"},
		common: []*Stmt{
			{Cmd: litCall("foo"), Background: true},
			litStmt("bar"),
		},
	},
	{
		Strs: []string{"! if foo; then bar; fi >/dev/null &"},
		common: &Stmt{
			Negated: true,
			Cmd: &IfClause{
				CondStmts: litStmts("foo"),
				ThenStmts: litStmts("bar"),
			},
			Redirs: []*Redirect{
				{Op: RdrOut, Word: litWord("/dev/null")},
			},
			Background: true,
		},
	},
	{
		Strs:   []string{"foo#bar"},
		common: litWord("foo#bar"),
	},
	{
		Strs:   []string{"{ echo } }; }"},
		common: block(litStmt("echo", "}", "}")),
	},
	{
		Strs: []string{"$({ echo; })"},
		common: cmdSubst(stmt(
			block(litStmt("echo")),
		)),
	},
	{
		Strs: []string{
			"$( (echo foo bar))",
			"$( (echo foo bar) )",
			"`(echo foo bar)`",
		},
		common: cmdSubst(stmt(
			subshell(litStmt("echo", "foo", "bar")),
		)),
	},
	{
		Strs: []string{
			"$(\n\t(a)\n\tb\n)",
			"$( (a); b)",
			"`(a); b`",
		},
		common: cmdSubst(
			stmt(subshell(litStmt("a"))),
			litStmt("b"),
		),
	},
	{
		Strs: []string{"$( (a) | b)"},
		common: cmdSubst(
			stmt(&BinaryCmd{
				Op: Pipe,
				X:  stmt(subshell(litStmt("a"))),
				Y:  litStmt("b"),
			}),
		),
	},
	{
		Strs: []string{`"$( (foo))"`},
		common: dblQuoted(cmdSubst(stmt(
			subshell(litStmt("foo")),
		))),
	},
	{
		Strs: []string{"$({ echo; })", "`{ echo; }`"},
		common: cmdSubst(stmt(
			block(litStmt("echo")),
		)),
	},
	{
		Strs:   []string{`{foo}`},
		common: litWord(`{foo}`),
	},
	{
		Strs:   []string{`{"foo"`},
		common: word(lit("{"), dblQuoted(lit("foo"))),
	},
	{
		Strs:   []string{`foo"bar"`, "fo\\\no\"bar\""},
		common: word(lit("foo"), dblQuoted(lit("bar"))),
	},
	{
		Strs:   []string{`!foo`},
		common: litWord(`!foo`),
	},
	{
		Strs:   []string{"$(foo bar)", "`foo bar`"},
		common: cmdSubst(litStmt("foo", "bar")),
	},
	{
		Strs: []string{"$(foo | bar)", "`foo | bar`"},
		common: cmdSubst(
			stmt(&BinaryCmd{
				Op: Pipe,
				X:  litStmt("foo"),
				Y:  litStmt("bar"),
			}),
		),
	},
	{
		Strs: []string{"$(foo $(b1 b2))"},
		common: cmdSubst(stmt(call(
			litWord("foo"),
			word(cmdSubst(litStmt("b1", "b2"))),
		))),
	},
	{
		Strs: []string{`"$(foo "bar")"`},
		common: dblQuoted(cmdSubst(stmt(call(
			litWord("foo"),
			word(dblQuoted(lit("bar"))),
		)))),
	},
	{
		Strs:   []string{"$(foo)", "`fo\\\no`"},
		common: cmdSubst(litStmt("foo")),
	},
	{
		Strs: []string{"foo $(bar)", "foo `bar`"},
		common: call(
			litWord("foo"),
			word(cmdSubst(litStmt("bar"))),
		),
	},
	{
		Strs: []string{"$(foo 'bar')", "`foo 'bar'`"},
		common: cmdSubst(stmt(call(
			litWord("foo"),
			word(sglQuoted("bar")),
		))),
	},
	{
		Strs: []string{`$(foo "bar")`, "`foo \"bar\"`"},
		common: cmdSubst(stmt(call(
			litWord("foo"),
			word(dblQuoted(lit("bar"))),
		))),
	},
	{
		Strs:   []string{`"$foo"`},
		common: dblQuoted(litParamExp("foo")),
	},
	{
		Strs:   []string{`"#foo"`},
		common: dblQuoted(lit("#foo")),
	},
	{
		Strs: []string{`$@a $*a $#a $$a $?a $!a $0a $-a`},
		common: call(
			word(litParamExp("@"), lit("a")),
			word(litParamExp("*"), lit("a")),
			word(litParamExp("#"), lit("a")),
			word(litParamExp("$"), lit("a")),
			word(litParamExp("?"), lit("a")),
			word(litParamExp("!"), lit("a")),
			word(litParamExp("0"), lit("a")),
			word(litParamExp("-"), lit("a")),
		),
	},
	{
		Strs:   []string{`$`, `$ #`},
		common: litWord("$"),
	},
	{
		Strs: []string{`${@} ${*} ${#} ${$} ${?} ${!} ${0} ${-}`},
		common: call(
			word(&ParamExp{Param: lit("@")}),
			word(&ParamExp{Param: lit("*")}),
			word(&ParamExp{Param: lit("#")}),
			word(&ParamExp{Param: lit("$")}),
			word(&ParamExp{Param: lit("?")}),
			word(&ParamExp{Param: lit("!")}),
			word(&ParamExp{Param: lit("0")}),
			word(&ParamExp{Param: lit("-")}),
		),
	},
	{
		Strs: []string{`${#$} ${##} ${#:-a} ${?+b}`},
		common: call(
			word(&ParamExp{Length: true, Param: lit("$")}),
			word(&ParamExp{Length: true, Param: lit("#")}),
			word(&ParamExp{Length: true, Exp: &Expansion{
				Op:   SubstColMinus,
				Word: litWord("a"),
			}}),
			word(&ParamExp{
				Param: lit("?"),
				Exp: &Expansion{
					Op:   SubstPlus,
					Word: litWord("b"),
				},
			}),
		),
	},
	{
		Strs:   []string{`${foo}`},
		common: &ParamExp{Param: lit("foo")},
	},
	{
		Strs: []string{`${foo}"bar"`},
		common: word(
			&ParamExp{Param: lit("foo")},
			dblQuoted(lit("bar")),
		),
	},
	{
		Strs: []string{`${foo-bar}`},
		common: &ParamExp{
			Param: lit("foo"),
			Exp: &Expansion{
				Op:   SubstMinus,
				Word: litWord("bar"),
			},
		},
	},
	{
		Strs: []string{`${foo+}"bar"`},
		common: word(
			&ParamExp{
				Param: lit("foo"),
				Exp: &Expansion{
					Op:   SubstPlus,
					Word: litWord(""),
				},
			},
			dblQuoted(lit("bar")),
		),
	},
	{
		Strs: []string{`${foo:=<"bar"}`},
		common: &ParamExp{
			Param: lit("foo"),
			Exp: &Expansion{
				Op:   SubstColAssgn,
				Word: word(lit("<"), dblQuoted(lit("bar"))),
			},
		},
	},
	{
		Strs: []string{
			"${foo:=b${c}$(d)}",
			"${foo:=b${c}`d`}",
		},
		common: &ParamExp{
			Param: lit("foo"),
			Exp: &Expansion{
				Op: SubstColAssgn,
				Word: word(
					lit("b"),
					&ParamExp{Param: lit("c")},
					cmdSubst(litStmt("d")),
				),
			},
		},
	},
	{
		Strs: []string{`${foo?"${bar}"}`},
		common: &ParamExp{
			Param: lit("foo"),
			Exp: &Expansion{
				Op: SubstQuest,
				Word: word(dblQuoted(
					&ParamExp{Param: lit("bar")},
				)),
			},
		},
	},
	{
		Strs: []string{`${foo:?bar1 bar2}`},
		common: &ParamExp{
			Param: lit("foo"),
			Exp: &Expansion{
				Op:   SubstColQuest,
				Word: litWord("bar1 bar2"),
			},
		},
	},
	{
		Strs: []string{`${a:+b}${a:-b}${a=b}`},
		common: word(
			&ParamExp{
				Param: lit("a"),
				Exp: &Expansion{
					Op:   SubstColPlus,
					Word: litWord("b"),
				},
			},
			&ParamExp{
				Param: lit("a"),
				Exp: &Expansion{
					Op:   SubstColMinus,
					Word: litWord("b"),
				},
			},
			&ParamExp{
				Param: lit("a"),
				Exp: &Expansion{
					Op:   SubstAssgn,
					Word: litWord("b"),
				},
			},
		),
	},
	{
		Strs: []string{`${foo%bar}${foo%%bar*}`},
		common: word(
			&ParamExp{
				Param: lit("foo"),
				Exp: &Expansion{
					Op:   RemSmallSuffix,
					Word: litWord("bar"),
				},
			},
			&ParamExp{
				Param: lit("foo"),
				Exp: &Expansion{
					Op:   RemLargeSuffix,
					Word: litWord("bar*"),
				},
			},
		),
	},
	{
		Strs: []string{`${foo#bar}${foo##bar*}`},
		common: word(
			&ParamExp{
				Param: lit("foo"),
				Exp: &Expansion{
					Op:   RemSmallPrefix,
					Word: litWord("bar"),
				},
			},
			&ParamExp{
				Param: lit("foo"),
				Exp: &Expansion{
					Op:   RemLargePrefix,
					Word: litWord("bar*"),
				},
			},
		),
	},
	{
		Strs: []string{`${foo%?}`},
		common: &ParamExp{
			Param: lit("foo"),
			Exp: &Expansion{
				Op:   RemSmallSuffix,
				Word: litWord("?"),
			},
		},
	},
	{
		Strs: []string{
			`${foo[1]}`,
			`${foo[ 1 ]}`,
		},
		bash: &ParamExp{
			Param: lit("foo"),
			Ind:   &Index{Expr: litWord("1")},
		},
	},
	{
		Strs: []string{`${foo[-1]}`},
		bash: &ParamExp{
			Param: lit("foo"),
			Ind: &Index{Expr: &UnaryArithm{
				Op: Minus,
				X:  litWord("1"),
			}},
		},
	},
	{
		Strs: []string{`${foo[@]}`},
		bash: &ParamExp{
			Param: lit("foo"),
			Ind:   &Index{Expr: litWord("@")},
		},
	},
	{
		Strs: []string{`${foo[*]-etc}`},
		bash: &ParamExp{
			Param: lit("foo"),
			Ind: &Index{
				Expr: litWord("*"),
			},
			Exp: &Expansion{
				Op:   SubstMinus,
				Word: litWord("etc"),
			},
		},
	},
	{
		Strs: []string{`${foo[${bar}]}`},
		bash: &ParamExp{
			Param: lit("foo"),
			Ind: &Index{
				Expr: word(&ParamExp{Param: lit("bar")}),
			},
		},
	},
	{
		Strs: []string{`${foo:1}`, `${foo: 1 }`},
		bash: &ParamExp{
			Param: lit("foo"),
			Slice: &Slice{Offset: litWord("1")},
		},
	},
	{
		Strs: []string{`${foo:1:2}`, `${foo: 1 : 2 }`},
		bash: &ParamExp{
			Param: lit("foo"),
			Slice: &Slice{
				Offset: litWord("1"),
				Length: litWord("2"),
			},
		},
	},
	{
		Strs: []string{`${foo:$a:$b}`},
		bash: &ParamExp{
			Param: lit("foo"),
			Slice: &Slice{
				Offset: word(litParamExp("a")),
				Length: word(litParamExp("b")),
			},
		},
	},
	{
		Strs: []string{`${foo:1:-2}`},
		bash: &ParamExp{
			Param: lit("foo"),
			Slice: &Slice{
				Offset: litWord("1"),
				Length: &UnaryArithm{Op: Minus, X: litWord("2")},
			},
		},
	},
	{
		Strs: []string{`${foo::+3}`},
		bash: &ParamExp{
			Param: lit("foo"),
			Slice: &Slice{
				Length: &UnaryArithm{Op: Plus, X: litWord("3")},
			},
		},
	},
	{
		Strs: []string{`${foo: -1}`},
		bash: &ParamExp{
			Param: lit("foo"),
			Slice: &Slice{
				Offset: &UnaryArithm{Op: Minus, X: litWord("1")},
			},
		},
	},
	{
		Strs: []string{`${foo:a?1:2:3}`},
		bash: &ParamExp{
			Param: lit("foo"),
			Slice: &Slice{
				Offset: &BinaryArithm{
					Op: Quest,
					X:  litWord("a"),
					Y: &BinaryArithm{
						Op: Colon,
						X:  litWord("1"),
						Y:  litWord("2"),
					},
				},
				Length: litWord("3"),
			},
		},
	},
	{
		Strs: []string{`${foo/a/b}`},
		bash: &ParamExp{
			Param: lit("foo"),
			Repl:  &Replace{Orig: litWord("a"), With: litWord("b")},
		},
	},
	{
		Strs: []string{"${foo/ /\t}"},
		bash: &ParamExp{
			Param: lit("foo"),
			Repl:  &Replace{Orig: litWord(" "), With: litWord("\t")},
		},
	},
	{
		Strs: []string{`${foo/[/]-}`},
		bash: &ParamExp{
			Param: lit("foo"),
			Repl:  &Replace{Orig: litWord("["), With: litWord("]-")},
		},
	},
	{
		Strs: []string{`${foo/bar/b/a/r}`},
		bash: &ParamExp{
			Param: lit("foo"),
			Repl: &Replace{
				Orig: litWord("bar"),
				With: litWord("b/a/r"),
			},
		},
	},
	{
		Strs: []string{`${foo/$a/$b}`},
		bash: &ParamExp{
			Param: lit("foo"),
			Repl: &Replace{
				Orig: word(litParamExp("a")),
				With: word(litParamExp("b")),
			},
		},
	},
	{
		Strs: []string{`${foo//b1/b2}`},
		bash: &ParamExp{
			Param: lit("foo"),
			Repl: &Replace{
				All:  true,
				Orig: litWord("b1"),
				With: litWord("b2"),
			},
		},
	},
	{
		Strs: []string{`${foo///}`, `${foo//}`},
		bash: &ParamExp{
			Param: lit("foo"),
			Repl: &Replace{
				All:  true,
				Orig: litWord(""),
				With: litWord(""),
			},
		},
	},
	{
		Strs: []string{`${foo/-//}`},
		bash: &ParamExp{
			Param: lit("foo"),
			Repl:  &Replace{Orig: litWord("-"), With: litWord("/")},
		},
	},
	{
		Strs: []string{
			`${foo//#/}`,
			`${foo//#}`,
		},
		bash: &ParamExp{
			Param: lit("foo"),
			Repl: &Replace{
				All:  true,
				Orig: litWord("#"),
				With: litWord(""),
			},
		},
	},
	{
		Strs: []string{`${a^b} ${a^^b} ${a,b} ${a,,b}`},
		bash: call(
			word(&ParamExp{Param: lit("a"),
				Exp: &Expansion{
					Op:   UpperFirst,
					Word: litWord("b"),
				},
			}),
			word(&ParamExp{Param: lit("a"),
				Exp: &Expansion{
					Op:   UpperAll,
					Word: litWord("b"),
				},
			}),
			word(&ParamExp{Param: lit("a"),
				Exp: &Expansion{
					Op:   LowerFirst,
					Word: litWord("b"),
				},
			}),
			word(&ParamExp{Param: lit("a"),
				Exp: &Expansion{
					Op:   LowerAll,
					Word: litWord("b"),
				},
			}),
		),
	},
	{
		Strs: []string{`${a@E} ${b@a}`},
		bash: call(
			word(&ParamExp{Param: lit("a"),
				Exp: &Expansion{
					Op:   OtherParamOps,
					Word: litWord("E"),
				},
			}),
			word(&ParamExp{Param: lit("b"),
				Exp: &Expansion{
					Op:   OtherParamOps,
					Word: litWord("a"),
				},
			}),
		),
		minBash: 44,
	},
	{
		Strs: []string{`${#foo}`},
		common: &ParamExp{
			Length: true,
			Param:  lit("foo"),
		},
	},
	{
		Strs: []string{`${!foo}`},
		common: &ParamExp{
			Excl:  true,
			Param: lit("foo"),
		},
	},
	{
		Strs: []string{`${#?}`},
		common: call(
			word(&ParamExp{Length: true, Param: lit("?")}),
		),
	},
	{
		Strs:   []string{`"${foo}"`},
		common: dblQuoted(&ParamExp{Param: lit("foo")}),
	},
	{
		Strs:   []string{`"(foo)"`},
		common: dblQuoted(lit("(foo)")),
	},
	{
		Strs: []string{`"${foo}>"`},
		common: dblQuoted(
			&ParamExp{Param: lit("foo")},
			lit(">"),
		),
	},
	{
		Strs:   []string{`"$(foo)"`, "\"`foo`\""},
		common: dblQuoted(cmdSubst(litStmt("foo"))),
	},
	{
		Strs: []string{
			`"$(foo bar)"`,
			`"$(foo  bar)"`,
			"\"`foo bar`\"",
			"\"`foo  bar`\"",
		},
		common: dblQuoted(cmdSubst(litStmt("foo", "bar"))),
	},
	{
		Strs:   []string{`'${foo}'`},
		common: sglQuoted("${foo}"),
	},
	{
		Strs:   []string{"$(())"},
		common: arithmExp(nil),
	},
	{
		Strs:   []string{"$((1))"},
		common: arithmExp(litWord("1")),
	},
	{
		Strs: []string{"$((1 + 3))", "$((1+3))"},
		bash: arithmExp(&BinaryArithm{
			Op: Add,
			X:  litWord("1"),
			Y:  litWord("3"),
		}),
	},
	{
		Strs: []string{`"$((foo))"`},
		bash: dblQuoted(arithmExp(
			litWord("foo"),
		)),
	},
	{
		Strs: []string{"$(($(echo 1)))", "$((`echo 1`))"},
		common: arithmExp(word(
			cmdSubst(litStmt("echo", "1")),
		)),
	},
	{
		Strs: []string{`$(($a)) b`},
		bash: call(
			word(arithmExp(word(litParamExp("a")))),
			litWord("b"),
		),
	},
	{
		Strs: []string{`$((arr[0]++))`},
		common: arithmExp(
			&UnaryArithm{Op: Inc, Post: true, X: litWord("arr[0]")},
		),
	},
	{
		Strs: []string{"$((5 * 2 - 1))", "$((5*2-1))"},
		common: arithmExp(&BinaryArithm{
			Op: Sub,
			X: &BinaryArithm{
				Op: Mul,
				X:  litWord("5"),
				Y:  litWord("2"),
			},
			Y: litWord("1"),
		}),
	},
	{
		Strs: []string{"$(($i | 13))"},
		common: arithmExp(&BinaryArithm{
			Op: Or,
			X:  word(litParamExp("i")),
			Y:  litWord("13"),
		}),
	},
	{
		Strs: []string{"$((3 & $((4))))"},
		common: arithmExp(&BinaryArithm{
			Op: And,
			X:  litWord("3"),
			Y:  word(arithmExp(litWord("4"))),
		}),
	},
	{
		Strs: []string{"$(($(a) + $((b))))"},
		common: arithmExp(&BinaryArithm{
			Op: Add,
			X:  word(cmdSubst(litStmt("a"))),
			Y:  word(arithmExp(litWord("b"))),
		}),
	},
	{
		Strs: []string{
			"$(($(1) + 2))",
			"$(($(1)+2))",
		},
		common: arithmExp(&BinaryArithm{
			Op: Add,
			X:  word(cmdSubst(litStmt("1"))),
			Y:  litWord("2"),
		}),
	},
	{
		Strs: []string{"$(((a) + ((b))))"},
		common: arithmExp(&BinaryArithm{
			Op: Add,
			X:  parenArit(litWord("a")),
			Y:  parenArit(parenArit(litWord("b"))),
		}),
	},
	{
		Strs: []string{
			"$((3 % 7))",
			"$((3\n% 7))",
			"$((3\\\n % 7))",
		},
		common: arithmExp(&BinaryArithm{
			Op: Rem,
			X:  litWord("3"),
			Y:  litWord("7"),
		}),
	},
	{
		Strs: []string{`"$((1 / 3))"`},
		common: dblQuoted(arithmExp(&BinaryArithm{
			Op: Quo,
			X:  litWord("1"),
			Y:  litWord("3"),
		})),
	},
	{
		Strs: []string{"$((2 ** 10))"},
		common: arithmExp(&BinaryArithm{
			Op: Pow,
			X:  litWord("2"),
			Y:  litWord("10"),
		}),
	},
	{
		Strs: []string{`$(((1) ^ 3))`},
		common: arithmExp(&BinaryArithm{
			Op: Xor,
			X:  parenArit(litWord("1")),
			Y:  litWord("3"),
		}),
	},
	{
		Strs: []string{`$((1 >> (3 << 2)))`},
		common: arithmExp(&BinaryArithm{
			Op: Shr,
			X:  litWord("1"),
			Y: parenArit(&BinaryArithm{
				Op: Shl,
				X:  litWord("3"),
				Y:  litWord("2"),
			}),
		}),
	},
	{
		Strs: []string{`$((-(1)))`},
		common: arithmExp(&UnaryArithm{
			Op: Minus,
			X:  parenArit(litWord("1")),
		}),
	},
	{
		Strs: []string{`$((i++))`},
		common: arithmExp(&UnaryArithm{
			Op:   Inc,
			Post: true,
			X:    litWord("i"),
		}),
	},
	{
		Strs:   []string{`$((--i))`},
		common: arithmExp(&UnaryArithm{Op: Dec, X: litWord("i")}),
	},
	{
		Strs:   []string{`$((!i))`},
		common: arithmExp(&UnaryArithm{Op: Not, X: litWord("i")}),
	},
	{
		Strs: []string{`$((-!+i))`},
		common: arithmExp(&UnaryArithm{
			Op: Minus,
			X: &UnaryArithm{
				Op: Not,
				X:  &UnaryArithm{Op: Plus, X: litWord("i")},
			},
		}),
	},
	{
		Strs: []string{`$((!!i))`},
		common: arithmExp(&UnaryArithm{
			Op: Not,
			X:  &UnaryArithm{Op: Not, X: litWord("i")},
		}),
	},
	{
		Strs: []string{`$((1 < 3))`},
		common: arithmExp(&BinaryArithm{
			Op: Lss,
			X:  litWord("1"),
			Y:  litWord("3"),
		}),
	},
	{
		Strs: []string{`$((i = 2))`, `$((i=2))`},
		common: arithmExp(&BinaryArithm{
			Op: Assgn,
			X:  litWord("i"),
			Y:  litWord("2"),
		}),
	},
	{
		Strs: []string{"$((a += 2, b -= 3))"},
		common: arithmExp(&BinaryArithm{
			Op: Comma,
			X: &BinaryArithm{
				Op: AddAssgn,
				X:  litWord("a"),
				Y:  litWord("2"),
			},
			Y: &BinaryArithm{
				Op: SubAssgn,
				X:  litWord("b"),
				Y:  litWord("3"),
			},
		}),
	},
	{
		Strs: []string{"$((a >>= 2, b <<= 3))"},
		common: arithmExp(&BinaryArithm{
			Op: Comma,
			X: &BinaryArithm{
				Op: ShrAssgn,
				X:  litWord("a"),
				Y:  litWord("2"),
			},
			Y: &BinaryArithm{
				Op: ShlAssgn,
				X:  litWord("b"),
				Y:  litWord("3"),
			},
		}),
	},
	{
		Strs: []string{"$((a == b && c > d))"},
		common: arithmExp(&BinaryArithm{
			Op: AndArit,
			X: &BinaryArithm{
				Op: Eql,
				X:  litWord("a"),
				Y:  litWord("b"),
			},
			Y: &BinaryArithm{
				Op: Gtr,
				X:  litWord("c"),
				Y:  litWord("d"),
			},
		}),
	},
	{
		Strs: []string{"$((a != b))"},
		common: arithmExp(&BinaryArithm{
			Op: Neq,
			X:  litWord("a"),
			Y:  litWord("b"),
		}),
	},
	{
		Strs: []string{"$((a &= b))"},
		common: arithmExp(&BinaryArithm{
			Op: AndAssgn,
			X:  litWord("a"),
			Y:  litWord("b"),
		}),
	},
	{
		Strs: []string{"$((a |= b))"},
		common: arithmExp(&BinaryArithm{
			Op: OrAssgn,
			X:  litWord("a"),
			Y:  litWord("b"),
		}),
	},
	{
		Strs: []string{"$((a %= b))"},
		common: arithmExp(&BinaryArithm{
			Op: RemAssgn,
			X:  litWord("a"),
			Y:  litWord("b"),
		}),
	},
	{
		Strs: []string{"$((a /= b))"},
		common: arithmExp(&BinaryArithm{
			Op: QuoAssgn,
			X:  litWord("a"),
			Y:  litWord("b"),
		}),
	},
	{
		Strs: []string{"$((a ^= b))"},
		common: arithmExp(&BinaryArithm{
			Op: XorAssgn,
			X:  litWord("a"),
			Y:  litWord("b"),
		}),
	},
	{
		Strs: []string{"$((i *= 3))"},
		common: arithmExp(&BinaryArithm{
			Op: MulAssgn,
			X:  litWord("i"),
			Y:  litWord("3"),
		}),
	},
	{
		Strs: []string{"$((2 >= 10))"},
		common: arithmExp(&BinaryArithm{
			Op: Geq,
			X:  litWord("2"),
			Y:  litWord("10"),
		}),
	},
	{
		Strs: []string{"$((foo ? b1 : b2))"},
		common: arithmExp(&BinaryArithm{
			Op: Quest,
			X:  litWord("foo"),
			Y: &BinaryArithm{
				Op: Colon,
				X:  litWord("b1"),
				Y:  litWord("b2"),
			},
		}),
	},
	{
		Strs: []string{`$((a <= (1 || 2)))`},
		common: arithmExp(&BinaryArithm{
			Op: Leq,
			X:  litWord("a"),
			Y: parenArit(&BinaryArithm{
				Op: OrArit,
				X:  litWord("1"),
				Y:  litWord("2"),
			}),
		}),
	},
	{
		Strs:   []string{"foo$", "foo$\n"},
		common: word(lit("foo"), lit("$")),
	},
	{
		Strs:  []string{`$''`},
		bash:  sglDQuoted(""),
		posix: word(lit("$"), sglQuoted("")),
	},
	{
		Strs:  []string{`$""`},
		bash:  dblDQuoted(),
		posix: word(lit("$"), dblQuoted()),
	},
	{
		Strs:  []string{`$'foo'`},
		bash:  sglDQuoted("foo"),
		posix: word(lit("$"), sglQuoted("foo")),
	},
	{
		Strs: []string{`$'f+oo${'`},
		bash: sglDQuoted("f+oo${"),
	},
	{
		Strs: []string{"$'foo bar`'"},
		bash: sglDQuoted("foo bar`"),
	},
	{
		Strs: []string{"$'a ${b} c'"},
		bash: sglDQuoted("a ${b} c"),
	},
	{
		Strs: []string{`$"a ${b} c"`},
		bash: dblDQuoted(
			lit("a "),
			&ParamExp{Param: lit("b")},
			lit(" c"),
		),
	},
	{
		Strs:   []string{`"a $b c"`},
		common: dblQuoted(lit("a "), litParamExp("b"), lit(" c")),
	},
	{
		Strs: []string{`$"a $b c"`},
		bash: dblDQuoted(
			lit("a "),
			litParamExp("b"),
			lit(" c"),
		),
	},
	{
		Strs: []string{"$'f\\'oo\n'"},
		bash: sglDQuoted("f\\'oo\n"),
	},
	{
		Strs:  []string{`$"foo"`},
		bash:  dblDQuoted(lit("foo")),
		posix: word(lit("$"), dblQuoted(lit("foo"))),
	},
	{
		Strs: []string{`$"foo$"`},
		bash: dblDQuoted(lit("foo"), lit("$")),
	},
	{
		Strs: []string{`$"foo bar"`},
		bash: dblDQuoted(lit("foo bar")),
	},
	{
		Strs: []string{`$'f\'oo'`},
		bash: sglDQuoted(`f\'oo`),
	},
	{
		Strs: []string{`$"f\"oo"`},
		bash: dblDQuoted(lit(`f\"oo`)),
	},
	{
		Strs:   []string{`"foo$"`},
		common: dblQuoted(lit("foo"), lit("$")),
	},
	{
		Strs:   []string{`"foo$$"`},
		common: dblQuoted(lit("foo"), litParamExp("$")),
	},
	{
		Strs: []string{"$(foo$)", "`foo$`"},
		common: cmdSubst(
			stmt(call(word(lit("foo"), lit("$")))),
		),
	},
	{
		Strs:   []string{"foo$bar"},
		common: word(lit("foo"), litParamExp("bar")),
	},
	{
		Strs:   []string{"foo$(bar)"},
		common: word(lit("foo"), cmdSubst(litStmt("bar"))),
	},
	{
		Strs:   []string{"foo${bar}"},
		common: word(lit("foo"), &ParamExp{Param: lit("bar")}),
	},
	{
		Strs:   []string{"'foo${bar'"},
		common: sglQuoted("foo${bar"),
	},
	{
		Strs: []string{"(foo)\nbar", "(foo); bar"},
		common: []Command{
			subshell(litStmt("foo")),
			litCall("bar"),
		},
	},
	{
		Strs: []string{"foo\n(bar)", "foo; (bar)"},
		common: []Command{
			litCall("foo"),
			subshell(litStmt("bar")),
		},
	},
	{
		Strs: []string{"foo\n(bar)", "foo; (bar)"},
		common: []Command{
			litCall("foo"),
			subshell(litStmt("bar")),
		},
	},
	{
		Strs: []string{
			"case $i in 1) foo ;; 2 | 3*) bar ;; esac",
			"case $i in 1) foo;; 2 | 3*) bar; esac",
			"case $i in (1) foo;; 2 | 3*) bar;; esac",
			"case $i\nin\n#etc\n1)\nfoo\n;;\n2 | 3*)\nbar\n;;\nesac",
		},
		common: &CaseClause{
			Word: word(litParamExp("i")),
			List: []*PatternList{
				{
					Op:       DblSemicolon,
					Patterns: litWords("1"),
					Stmts:    litStmts("foo"),
				},
				{
					Op:       DblSemicolon,
					Patterns: litWords("2", "3*"),
					Stmts:    litStmts("bar"),
				},
			},
		},
	},
	{
		Strs: []string{"case $i in 1) a ;;& 2) b ;& 3) c ;; esac"},
		bash: &CaseClause{
			Word: word(litParamExp("i")),
			List: []*PatternList{
				{
					Op:       DblSemiFall,
					Patterns: litWords("1"),
					Stmts:    litStmts("a"),
				},
				{
					Op:       SemiFall,
					Patterns: litWords("2"),
					Stmts:    litStmts("b"),
				},
				{
					Op:       DblSemicolon,
					Patterns: litWords("3"),
					Stmts:    litStmts("c"),
				},
			},
		},
	},
	{
		Strs: []string{"case $i in 1) cat <<EOF ;;\nfoo\nEOF\nesac"},
		common: &CaseClause{
			Word: word(litParamExp("i")),
			List: []*PatternList{{
				Op:       DblSemicolon,
				Patterns: litWords("1"),
				Stmts: []*Stmt{{
					Cmd: litCall("cat"),
					Redirs: []*Redirect{{
						Op:   Hdoc,
						Word: litWord("EOF"),
						Hdoc: litWord("foo\n"),
					}},
				}},
			}},
		},
	},
	{
		Strs: []string{"foo | while read a; do b; done"},
		common: &BinaryCmd{
			Op: Pipe,
			X:  litStmt("foo"),
			Y: stmt(&WhileClause{
				CondStmts: []*Stmt{
					litStmt("read", "a"),
				},
				DoStmts: litStmts("b"),
			}),
		},
	},
	{
		Strs: []string{"while read l; do foo || bar; done"},
		common: &WhileClause{
			CondStmts: []*Stmt{litStmt("read", "l")},
			DoStmts: stmts(&BinaryCmd{
				Op: OrStmt,
				X:  litStmt("foo"),
				Y:  litStmt("bar"),
			}),
		},
	},
	{
		Strs:   []string{"echo if while"},
		common: litCall("echo", "if", "while"),
	},
	{
		Strs:   []string{"${foo}if"},
		common: word(&ParamExp{Param: lit("foo")}, lit("if")),
	},
	{
		Strs:   []string{"$if'|'"},
		common: word(litParamExp("if"), sglQuoted("|")),
	},
	{
		Strs: []string{"if a; then b=; fi", "if a; then b=\nfi"},
		common: &IfClause{
			CondStmts: litStmts("a"),
			ThenStmts: []*Stmt{
				{Assigns: []*Assign{
					{Name: lit("b")},
				}},
			},
		},
	},
	{
		Strs: []string{"if a; then >f; fi", "if a; then >f\nfi"},
		common: &IfClause{
			CondStmts: litStmts("a"),
			ThenStmts: []*Stmt{
				{Redirs: []*Redirect{
					{Op: RdrOut, Word: litWord("f")},
				}},
			},
		},
	},
	{
		Strs: []string{"if a; then (a); fi", "if a; then (a) fi"},
		common: &IfClause{
			CondStmts: litStmts("a"),
			ThenStmts: stmts(subshell(litStmt("a"))),
		},
	},
	{
		Strs: []string{"a=b\nc=d", "a=b; c=d"},
		common: []*Stmt{
			{Assigns: []*Assign{
				{Name: lit("a"), Value: litWord("b")},
			}},
			{Assigns: []*Assign{
				{Name: lit("c"), Value: litWord("d")},
			}},
		},
	},
	{
		Strs: []string{"foo && write | read"},
		common: &BinaryCmd{
			Op: AndStmt,
			X:  litStmt("foo"),
			Y: stmt(&BinaryCmd{
				Op: Pipe,
				X:  litStmt("write"),
				Y:  litStmt("read"),
			}),
		},
	},
	{
		Strs: []string{"write | read && bar"},
		common: &BinaryCmd{
			Op: AndStmt,
			X: stmt(&BinaryCmd{
				Op: Pipe,
				X:  litStmt("write"),
				Y:  litStmt("read"),
			}),
			Y: litStmt("bar"),
		},
	},
	{
		Strs: []string{"foo >f | bar"},
		common: &BinaryCmd{
			Op: Pipe,
			X: &Stmt{
				Cmd: litCall("foo"),
				Redirs: []*Redirect{
					{Op: RdrOut, Word: litWord("f")},
				},
			},
			Y: litStmt("bar"),
		},
	},
	{
		Strs: []string{"(foo) >f | bar"},
		common: &BinaryCmd{
			Op: Pipe,
			X: &Stmt{
				Cmd: subshell(litStmt("foo")),
				Redirs: []*Redirect{
					{Op: RdrOut, Word: litWord("f")},
				},
			},
			Y: litStmt("bar"),
		},
	},
	{
		Strs: []string{"foo | >f"},
		common: &BinaryCmd{
			Op: Pipe,
			X:  litStmt("foo"),
			Y: &Stmt{Redirs: []*Redirect{
				{Op: RdrOut, Word: litWord("f")},
			}},
		},
	},
	{
		Strs:  []string{"[[ a ]]"},
		bash:  &TestClause{X: litWord("a")},
		posix: litStmt("[[", "a", "]]"),
	},
	{
		Strs: []string{"[[ a ]]\nb"},
		bash: stmts(
			&TestClause{X: litWord("a")},
			litCall("b"),
		),
	},
	{
		Strs: []string{"[[ a > b ]]"},
		bash: &TestClause{X: &BinaryTest{
			Op: TsAfter,
			X:  litWord("a"),
			Y:  litWord("b"),
		}},
	},
	{
		Strs: []string{"[[ 1 -nt 2 ]]"},
		bash: &TestClause{X: &BinaryTest{
			Op: TsNewer,
			X:  litWord("1"),
			Y:  litWord("2"),
		}},
	},
	{
		Strs: []string{"[[ 1 -eq 2 ]]"},
		bash: &TestClause{X: &BinaryTest{
			Op: TsEql,
			X:  litWord("1"),
			Y:  litWord("2"),
		}},
	},
	{
		Strs: []string{"[[ -R a ]]"},
		bash: &TestClause{X: &UnaryTest{
			Op: TsRefVar,
			X:  litWord("a"),
		}},
		minBash: 43,
	},
	{
		Strs: []string{"[[ a =~ b ]]", "[[ a =~ b ]];"},
		bash: &TestClause{X: &BinaryTest{
			Op: TsReMatch,
			X:  litWord("a"),
			Y:  litWord("b"),
		}},
	},
	{
		Strs: []string{`[[ a =~ " foo "$bar ]]`},
		bash: &TestClause{X: &BinaryTest{
			Op: TsReMatch,
			X:  litWord("a"),
			Y: word(
				dblQuoted(lit(" foo ")),
				litParamExp("bar"),
			),
		}},
	},
	{
		Strs: []string{`[[ a =~ [ab](c |d) ]]`},
		bash: &TestClause{X: &BinaryTest{
			Op: TsReMatch,
			X:  litWord("a"),
			Y:  litWord("[ab](c |d)"),
		}},
	},
	{
		Strs: []string{`[[ a =~ ( ]]) ]]`},
		bash: &TestClause{X: &BinaryTest{
			Op: TsReMatch,
			X:  litWord("a"),
			Y:  litWord("( ]])"),
		}},
	},
	{
		Strs: []string{`[[ a =~ b\ c ]]`},
		bash: &TestClause{X: &BinaryTest{
			Op: TsReMatch,
			X:  litWord("a"),
			Y:  litWord(`b\ c`),
		}},
	},
	{
		Strs: []string{`[[ a == -n ]]`},
		bash: &TestClause{X: &BinaryTest{
			Op: TsEqual,
			X:  litWord("a"),
			Y:  litWord("-n"),
		}},
	},
	{
		Strs: []string{`[[ a =~ -n ]]`},
		bash: &TestClause{X: &BinaryTest{
			Op: TsReMatch,
			X:  litWord("a"),
			Y:  litWord("-n"),
		}},
	},
	{
		Strs: []string{"[[ -n $a ]]"},
		bash: &TestClause{
			X: &UnaryTest{Op: TsNempStr, X: word(litParamExp("a"))},
		},
	},
	{
		Strs: []string{"[[ ! $a < 'b' ]]"},
		bash: &TestClause{X: &UnaryTest{
			Op: TsNot,
			X: &BinaryTest{
				Op: TsBefore,
				X:  word(litParamExp("a")),
				Y:  word(sglQuoted("b")),
			},
		}},
	},
	{
		Strs: []string{
			"[[ ! -e $a ]]",
			"[[ ! -a $a ]]",
		},
		bash: &TestClause{X: &UnaryTest{
			Op: TsNot,
			X:  &UnaryTest{Op: TsExists, X: word(litParamExp("a"))},
		}},
	},
	{
		Strs: []string{"[[ (a && b) ]]"},
		bash: &TestClause{X: parenTest(&BinaryTest{
			Op: AndTest,
			X:  litWord("a"),
			Y:  litWord("b"),
		})},
	},
	{
		Strs: []string{"[[ (a && b) || -f c ]]"},
		bash: &TestClause{X: &BinaryTest{
			Op: OrTest,
			X: parenTest(&BinaryTest{
				Op: AndTest,
				X:  litWord("a"),
				Y:  litWord("b"),
			}),
			Y: &UnaryTest{Op: TsRegFile, X: litWord("c")},
		}},
	},
	{
		Strs: []string{
			"[[ -S a && -L b ]]",
			"[[ -S a && -h b ]]",
		},
		bash: &TestClause{X: &BinaryTest{
			Op: AndTest,
			X:  &UnaryTest{Op: TsSocket, X: litWord("a")},
			Y:  &UnaryTest{Op: TsSmbLink, X: litWord("b")},
		}},
	},
	{
		Strs: []string{"[[ -k a && -N b ]]"},
		bash: &TestClause{X: &BinaryTest{
			Op: AndTest,
			X:  &UnaryTest{Op: TsSticky, X: litWord("a")},
			Y:  &UnaryTest{Op: TsModif, X: litWord("b")},
		}},
	},
	{
		Strs: []string{"[[ -G a && -O b ]]"},
		bash: &TestClause{X: &BinaryTest{
			Op: AndTest,
			X:  &UnaryTest{Op: TsGrpOwn, X: litWord("a")},
			Y:  &UnaryTest{Op: TsUsrOwn, X: litWord("b")},
		}},
	},
	{
		Strs: []string{"[[ -d a && -c b ]]"},
		bash: &TestClause{X: &BinaryTest{
			Op: AndTest,
			X:  &UnaryTest{Op: TsDirect, X: litWord("a")},
			Y:  &UnaryTest{Op: TsCharSp, X: litWord("b")},
		}},
	},
	{
		Strs: []string{"[[ -b a && -p b ]]"},
		bash: &TestClause{X: &BinaryTest{
			Op: AndTest,
			X:  &UnaryTest{Op: TsBlckSp, X: litWord("a")},
			Y:  &UnaryTest{Op: TsNmPipe, X: litWord("b")},
		}},
	},
	{
		Strs: []string{"[[ -g a && -u b ]]"},
		bash: &TestClause{X: &BinaryTest{
			Op: AndTest,
			X:  &UnaryTest{Op: TsGIDSet, X: litWord("a")},
			Y:  &UnaryTest{Op: TsUIDSet, X: litWord("b")},
		}},
	},
	{
		Strs: []string{"[[ -r a && -w b ]]"},
		bash: &TestClause{X: &BinaryTest{
			Op: AndTest,
			X:  &UnaryTest{Op: TsRead, X: litWord("a")},
			Y:  &UnaryTest{Op: TsWrite, X: litWord("b")},
		}},
	},
	{
		Strs: []string{"[[ -x a && -s b ]]"},
		bash: &TestClause{X: &BinaryTest{
			Op: AndTest,
			X:  &UnaryTest{Op: TsExec, X: litWord("a")},
			Y:  &UnaryTest{Op: TsNoEmpty, X: litWord("b")},
		}},
	},
	{
		Strs: []string{"[[ -t a && -z b ]]"},
		bash: &TestClause{X: &BinaryTest{
			Op: AndTest,
			X:  &UnaryTest{Op: TsFdTerm, X: litWord("a")},
			Y:  &UnaryTest{Op: TsEmpStr, X: litWord("b")},
		}},
	},
	{
		Strs: []string{"[[ -o a && -v b ]]"},
		bash: &TestClause{X: &BinaryTest{
			Op: AndTest,
			X:  &UnaryTest{Op: TsOptSet, X: litWord("a")},
			Y:  &UnaryTest{Op: TsVarSet, X: litWord("b")},
		}},
	},
	{
		Strs: []string{"[[ a -ot b && c -ef d ]]"},
		bash: &TestClause{X: &BinaryTest{
			Op: AndTest,
			X: &BinaryTest{
				Op: TsOlder,
				X:  litWord("a"),
				Y:  litWord("b"),
			},
			Y: &BinaryTest{
				Op: TsDevIno,
				X:  litWord("c"),
				Y:  litWord("d"),
			},
		}},
	},
	{
		Strs: []string{
			"[[ a == b && c != d ]]",
			"[[ a = b && c != d ]]",
		},
		bash: &TestClause{X: &BinaryTest{
			Op: AndTest,
			X: &BinaryTest{
				Op: TsEqual,
				X:  litWord("a"),
				Y:  litWord("b"),
			},
			Y: &BinaryTest{
				Op: TsNequal,
				X:  litWord("c"),
				Y:  litWord("d"),
			},
		}},
	},
	{
		Strs: []string{"[[ a -ne b && c -le d ]]"},
		bash: &TestClause{X: &BinaryTest{
			Op: AndTest,
			X: &BinaryTest{
				Op: TsNeq,
				X:  litWord("a"),
				Y:  litWord("b"),
			},
			Y: &BinaryTest{
				Op: TsLeq,
				X:  litWord("c"),
				Y:  litWord("d"),
			},
		}},
	},
	{
		Strs: []string{"[[ c -ge d ]]"},
		bash: &TestClause{X: &BinaryTest{
			Op: TsGeq,
			X:  litWord("c"),
			Y:  litWord("d"),
		}},
	},
	{
		Strs: []string{"[[ a -lt b && c -gt d ]]"},
		bash: &TestClause{X: &BinaryTest{
			Op: AndTest,
			X: &BinaryTest{
				Op: TsLss,
				X:  litWord("a"),
				Y:  litWord("b"),
			},
			Y: &BinaryTest{
				Op: TsGtr,
				X:  litWord("c"),
				Y:  litWord("d"),
			},
		}},
	},
	{
		Strs: []string{
			"declare -f func",
			"typeset -f func",
		},
		bash: &DeclClause{
			Opts:    litWords("-f"),
			Assigns: []*Assign{{Value: litWord("func")}},
		},
	},
	{
		Strs: []string{"(local bar)"},
		bash: subshell(stmt(&DeclClause{
			Variant: "local",
			Assigns: []*Assign{{Value: litWord("bar")}},
		})),
		posix: subshell(litStmt("local", "bar")),
	},
	{
		Strs: []string{"export bar"},
		bash: &DeclClause{
			Variant: "export",
			Assigns: []*Assign{{Value: litWord("bar")}},
		},
		posix: litStmt("export", "bar"),
	},
	{
		Strs:  []string{"readonly -n"},
		bash:  &DeclClause{Variant: "readonly", Opts: litWords("-n")},
		posix: litStmt("readonly", "-n"),
	},
	{
		Strs: []string{"nameref bar"},
		bash: &DeclClause{
			Variant: "nameref",
			Assigns: []*Assign{{Value: litWord("bar")}},
		},
		posix: litStmt("nameref", "bar"),
	},
	{
		Strs: []string{"declare -a -bc foo=bar"},
		bash: &DeclClause{
			Opts: litWords("-a", "-bc"),
			Assigns: []*Assign{
				{Name: lit("foo"), Value: litWord("bar")},
			},
		},
	},
	{
		Strs: []string{
			"declare -a foo=(b1 $(b2))",
			"declare -a foo=(b1 `b2`)",
		},
		bash: &DeclClause{
			Opts: litWords("-a"),
			Assigns: []*Assign{{
				Name: lit("foo"),
				Value: word(&ArrayExpr{List: []*Word{
					litWord("b1"),
					word(cmdSubst(litStmt("b2"))),
				}}),
			}},
		},
	},
	{
		Strs: []string{"local -a foo=(b1)"},
		bash: &DeclClause{
			Variant: "local",
			Opts:    litWords("-a"),
			Assigns: []*Assign{{
				Name:  lit("foo"),
				Value: word(&ArrayExpr{List: litWords("b1")}),
			}},
		},
	},
	{
		Strs: []string{
			"a && b=(c)\nd",
			"a && b=(c); d",
		},
		bash: stmts(
			&BinaryCmd{
				Op: AndStmt,
				X:  litStmt("a"),
				Y: &Stmt{Assigns: []*Assign{{
					Name: lit("b"),
					Value: word(&ArrayExpr{
						List: litWords("c"),
					}),
				}}},
			},
			litCall("d"),
		),
	},
	{
		Strs: []string{"declare -f func >/dev/null"},
		bash: &Stmt{
			Cmd: &DeclClause{
				Opts:    litWords("-f"),
				Assigns: []*Assign{{Value: litWord("func")}},
			},
			Redirs: []*Redirect{
				{Op: RdrOut, Word: litWord("/dev/null")},
			},
		},
	},
	{
		Strs:  []string{"eval"},
		bash:  &EvalClause{},
		posix: litStmt("eval"),
	},
	{
		Strs: []string{"eval a=b foo"},
		bash: &EvalClause{Stmt: &Stmt{
			Cmd: litCall("foo"),
			Assigns: []*Assign{{
				Name:  lit("a"),
				Value: litWord("b"),
			}},
		}},
	},
	{
		Strs: []string{"coproc a { b; }"},
		bash: &CoprocClause{
			Name: lit("a"),
			Stmt: stmt(block(litStmt("b"))),
		},
	},
	{
		Strs: []string{"coproc a", "coproc a;"},
		bash: &CoprocClause{Stmt: litStmt("a")},
	},
	{
		Strs: []string{"coproc a b"},
		bash: &CoprocClause{Stmt: litStmt("a", "b")},
	},
	{
		Strs: []string{"coproc { a; }"},
		bash: &CoprocClause{
			Stmt: stmt(block(litStmt("a"))),
		},
	},
	{
		Strs: []string{"coproc (a)"},
		bash: &CoprocClause{
			Stmt: stmt(subshell(litStmt("a"))),
		},
	},
	{
		Strs: []string{"coproc $()", "coproc ``"},
		bash: &CoprocClause{Stmt: stmt(call(
			word(cmdSubst()),
		))},
	},
	{
		Strs: []string{`let i++`},
		bash: letClause(
			&UnaryArithm{Op: Inc, Post: true, X: litWord("i")},
		),
		posix: litStmt("let", "i++"),
	},
	{
		Strs: []string{`let a++ b++ c +d`},
		bash: letClause(
			&UnaryArithm{Op: Inc, Post: true, X: litWord("a")},
			&UnaryArithm{Op: Inc, Post: true, X: litWord("b")},
			litWord("c"),
			&UnaryArithm{Op: Plus, X: litWord("d")},
		),
	},
	{
		Strs: []string{`let "--i"`},
		bash: letClause(
			word(dblQuoted(lit("--i"))),
		),
	},
	{
		Strs: []string{`let ++i >/dev/null`},
		bash: &Stmt{
			Cmd:    letClause(&UnaryArithm{Op: Inc, X: litWord("i")}),
			Redirs: []*Redirect{{Op: RdrOut, Word: litWord("/dev/null")}},
		},
	},
	{
		Strs: []string{
			`let a=(1 + 2) b=3+4`,
			`let a=(1+2) b=3+4`,
		},
		bash: letClause(
			&BinaryArithm{
				Op: Assgn,
				X:  litWord("a"),
				Y: parenArit(&BinaryArithm{
					Op: Add,
					X:  litWord("1"),
					Y:  litWord("2"),
				}),
			},
			&BinaryArithm{
				Op: Assgn,
				X:  litWord("b"),
				Y: &BinaryArithm{
					Op: Add,
					X:  litWord("3"),
					Y:  litWord("4"),
				},
			},
		),
	},
	{
		Strs:   []string{"(foo-bar)"},
		common: subshell(litStmt("foo-bar")),
	},
	{
		Strs: []string{
			"let i++\nbar",
			"let i++ \nbar",
			"let i++; bar",
		},
		bash: []*Stmt{
			stmt(letClause(&UnaryArithm{
				Op:   Inc,
				Post: true,
				X:    litWord("i"),
			})),
			litStmt("bar"),
		},
	},
	{
		Strs: []string{
			"let i++\nfoo=(bar)",
			"let i++; foo=(bar)",
			"let i++; foo=(bar)\n",
		},
		bash: []*Stmt{
			stmt(letClause(&UnaryArithm{
				Op:   Inc,
				Post: true,
				X:    litWord("i"),
			})),
			{
				Assigns: []*Assign{{
					Name: lit("foo"),
					Value: word(
						&ArrayExpr{List: litWords("bar")},
					),
				}},
			},
		},
	},
	{
		Strs: []string{
			"case a in b) let i++ ;; esac",
			"case a in b) let i++;; esac",
		},
		bash: &CaseClause{
			Word: word(lit("a")),
			List: []*PatternList{{
				Op:       DblSemicolon,
				Patterns: litWords("b"),
				Stmts: stmts(letClause(&UnaryArithm{
					Op:   Inc,
					Post: true,
					X:    litWord("i"),
				})),
			}},
		},
	},
	{
		Strs: []string{"let $?"},
		bash: letClause(word(litParamExp("?"))),
	},
	{
		Strs: []string{"a=(b c) foo"},
		bash: &Stmt{
			Assigns: []*Assign{{
				Name: lit("a"),
				Value: word(
					&ArrayExpr{List: litWords("b", "c")},
				),
			}},
			Cmd: litCall("foo"),
		},
	},
	{
		Strs: []string{"a=(b c) foo", "a=(\nb\nc\n) foo"},
		bash: &Stmt{
			Assigns: []*Assign{{
				Name: lit("a"),
				Value: word(
					&ArrayExpr{List: litWords("b", "c")},
				),
			}},
			Cmd: litCall("foo"),
		},
	},
	{
		Strs: []string{"a+=1"},
		bash: &Stmt{
			Assigns: []*Assign{{
				Append: true,
				Name:   lit("a"),
				Value:  litWord("1"),
			}},
		},
		posix: litStmt("a+=1"),
	},
	{
		Strs: []string{"b+=(2 3)"},
		bash: &Stmt{Assigns: []*Assign{{
			Append: true,
			Name:   lit("b"),
			Value: word(
				&ArrayExpr{List: litWords("2", "3")},
			),
		}}},
	},
	{
		Strs:  []string{"a[2]=b"},
		posix: litStmt("a[2]=b"),
		bash: &Stmt{Assigns: []*Assign{{
			Name:  lit("a[2]"),
			Value: litWord("b"),
		}}},
	},
	{
		Strs: []string{"<<EOF | b\nfoo\nEOF", "<<EOF|b;\nfoo\n"},
		common: &BinaryCmd{
			Op: Pipe,
			X: &Stmt{Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("foo\n"),
			}}},
			Y: litStmt("b"),
		},
	},
	{
		Strs: []string{"<<EOF1 <<EOF2 | c && d\nEOF1\nEOF2"},
		common: &BinaryCmd{
			Op: AndStmt,
			X: stmt(&BinaryCmd{
				Op: Pipe,
				X: &Stmt{Redirs: []*Redirect{
					{
						Op:   Hdoc,
						Word: litWord("EOF1"),
						Hdoc: litWord(""),
					},
					{
						Op:   Hdoc,
						Word: litWord("EOF2"),
						Hdoc: litWord(""),
					},
				}},
				Y: litStmt("c"),
			}),
			Y: litStmt("d"),
		},
	},
	{
		Strs: []string{
			"<<EOF && { bar; }\nhdoc\nEOF",
			"<<EOF &&\nhdoc\nEOF\n{ bar; }",
		},
		common: &BinaryCmd{
			Op: AndStmt,
			X: &Stmt{Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("hdoc\n"),
			}}},
			Y: stmt(block(litStmt("bar"))),
		},
	},
	{
		Strs: []string{"foo() {\n\t<<EOF && { bar; }\nhdoc\nEOF\n}"},
		common: &FuncDecl{
			Name: lit("foo"),
			Body: stmt(block(stmt(&BinaryCmd{
				Op: AndStmt,
				X: &Stmt{Redirs: []*Redirect{{
					Op:   Hdoc,
					Word: litWord("EOF"),
					Hdoc: litWord("hdoc\n"),
				}}},
				Y: stmt(block(litStmt("bar"))),
			}))),
		},
	},
	{
		Strs: []string{`"a$("")"`, "\"a`\"\"`\""},
		common: dblQuoted(
			lit("a"),
			cmdSubst(stmt(call(
				word(dblQuoted()),
			))),
		),
	},
	{
		Strs: []string{"echo ?(b)*(c)+(d)@(e)!(f)"},
		bash: stmt(call(litWord("echo"), word(
			&ExtGlob{Op: GlobQuest, Pattern: lit("b")},
			&ExtGlob{Op: GlobStar, Pattern: lit("c")},
			&ExtGlob{Op: GlobPlus, Pattern: lit("d")},
			&ExtGlob{Op: GlobAt, Pattern: lit("e")},
			&ExtGlob{Op: GlobExcl, Pattern: lit("f")},
		))),
	},
	{
		Strs: []string{"echo foo@(b*(c|d))bar"},
		bash: stmt(call(litWord("echo"), word(
			lit("foo"),
			&ExtGlob{Op: GlobAt, Pattern: lit("b*(c|d)")},
			lit("bar"),
		))),
	},
	{
		Strs: []string{"echo $a@(b)$c?(d)$e*(f)$g+(h)$i!(j)$k"},
		bash: stmt(call(litWord("echo"), word(
			litParamExp("a"),
			&ExtGlob{Op: GlobAt, Pattern: lit("b")},
			litParamExp("c"),
			&ExtGlob{Op: GlobQuest, Pattern: lit("d")},
			litParamExp("e"),
			&ExtGlob{Op: GlobStar, Pattern: lit("f")},
			litParamExp("g"),
			&ExtGlob{Op: GlobPlus, Pattern: lit("h")},
			litParamExp("i"),
			&ExtGlob{Op: GlobExcl, Pattern: lit("j")},
			litParamExp("k"),
		))),
	},
}

// these don't have a canonical format with the same AST
var fileTestsNoPrint = []testCase{
	{
		Strs: []string{"<<EOF\n\\"},
		common: &Stmt{Redirs: []*Redirect{{
			Op:   Hdoc,
			Word: litWord("EOF"),
			Hdoc: litWord("\\"),
		}}},
	},
	{
		Strs:  []string{`$[foo]`},
		posix: word(lit("$"), lit("[foo]")),
	},
	{
		Strs:  []string{`"$[foo]"`},
		posix: dblQuoted(lit("$"), lit("[foo]")),
	},
	{
		Strs: []string{`"$[1 + 3]"`},
		bash: dblQuoted(arithmExpBr(&BinaryArithm{
			Op: Add,
			X:  litWord("1"),
			Y:  litWord("3"),
		})),
	},
}

func fullProg(v interface{}) *File {
	switch x := v.(type) {
	case *File:
		return x
	case []*Stmt:
		return &File{Stmts: x}
	case *Stmt:
		return &File{Stmts: []*Stmt{x}}
	case []Command:
		f := &File{}
		for _, cmd := range x {
			f.Stmts = append(f.Stmts, stmt(cmd))
		}
		return f
	case *Word:
		return fullProg(call(x))
	case WordPart:
		return fullProg(word(x))
	case Command:
		return fullProg(stmt(x))
	case nil:
	default:
		panic(reflect.TypeOf(v))
	}
	return nil
}

func clearPosRecurse(tb testing.TB, src string, v interface{}) {
	const zeroPos = 0
	checkSrc := func(pos Pos, strs ...string) {
		if src == "" {
			return
		}
		offs := int(pos - 1)
		if offs < 0 || offs > len(src) {
			tb.Fatalf("Pos %d in %T is out of bounds in %q",
				pos, v, string(src))
			return
		}
		if strs == nil {
			return
		}
		var gotErr string
		for i, want := range strs {
			got := string([]byte(src[offs:]))
			if i == 0 {
				gotErr = got
			}
			got = strings.Replace(got, "\\\n", "", -1)
			if len(got) > len(want) {
				got = got[:len(want)]
			}
			if got == want {
				return
			}
		}
		tb.Fatalf("Expected one of %q at %d in %q, found %q",
			strs, pos, src, gotErr)
	}
	setPos := func(p *Pos, strs ...string) {
		checkSrc(*p, strs...)
		if *p == zeroPos {
			tb.Fatalf("Pos in %T is already %v", v, zeroPos)
		}
		*p = zeroPos
	}
	checkPos := func(n Node) {
		if n == nil {
			return
		}
		if n.Pos() != zeroPos {
			tb.Fatalf("Found unexpected Pos() in %T: want %d, got %d",
				n, zeroPos, n.Pos())
		}
		if n.Pos() > n.End() {
			tb.Fatalf("Found End() before Pos() in %T", n)
		}
	}
	recurse := func(v interface{}) {
		clearPosRecurse(tb, src, v)
		if n, ok := v.(Node); ok {
			checkPos(n)
		}
	}
	switch x := v.(type) {
	case *File:
		for _, c := range x.Comments {
			recurse(c)
		}
		recurse(x.Stmts)
		checkPos(x)
	case *Comment:
		setPos(&x.Hash, "#"+x.Text)
	case []*Stmt:
		for _, s := range x {
			recurse(s)
		}
	case *Stmt:
		endOff := int(x.End() - 1)
		switch {
		case src == "":
		case endOff >= len(src):
			// ended by EOF
		case wordBreak(rune(src[endOff])), regOps(rune(src[endOff])):
			// ended by end character
		case endOff > 0 && src[endOff-1] == ';':
			// ended by semicolon
		default:
			tb.Fatalf("Unexpected Stmt.End() %d %q in %q",
				endOff, src[endOff], string(src))
		}
		setPos(&x.Position)
		if x.Semicolon.IsValid() {
			setPos(&x.Semicolon, ";")
		}
		if x.Cmd != nil {
			recurse(x.Cmd)
		}
		recurse(x.Assigns)
		for _, r := range x.Redirs {
			setPos(&r.OpPos, r.Op.String())
			if r.N != nil {
				recurse(r.N)
			}
			recurse(r.Word)
			if r.Hdoc != nil {
				recurse(r.Hdoc)
			}
		}
	case []*Assign:
		for _, a := range x {
			if a.Name != nil {
				recurse(a.Name)
			}
			if a.Value != nil {
				recurse(a.Value)
			}
			checkPos(a)
		}
	case *CallExpr:
		recurse(x.Args)
	case []*Word:
		for _, w := range x {
			recurse(w)
		}
	case *Word:
		recurse(x.Parts)
	case []WordPart:
		for _, wp := range x {
			recurse(wp)
		}
	case *Lit:
		pos, end := int(x.Pos()), int(x.End())
		want := pos + len(x.Value)
		switch {
		case src == "":
		case strings.Contains(src, "\\\n"):
		case end-1 < len(src) && src[end-1] == '\n':
			// heredoc literals that end with the
			// stop word and a newline
		case end-1 == len(src):
			// same as above, but with word and EOF
		case end != want:
			tb.Fatalf("Unexpected Lit.End() %d (wanted %d) in %q",
				end, want, string(src))
		}
		setPos(&x.ValuePos, x.Value)
		setPos(&x.ValueEnd)
	case *Subshell:
		setPos(&x.Lparen, "(")
		setPos(&x.Rparen, ")")
		recurse(x.Stmts)
	case *Block:
		setPos(&x.Lbrace, "{")
		setPos(&x.Rbrace, "}")
		recurse(x.Stmts)
	case *IfClause:
		setPos(&x.If, "if")
		setPos(&x.Then, "then")
		setPos(&x.Fi, "fi")
		recurse(x.CondStmts)
		recurse(x.ThenStmts)
		for _, e := range x.Elifs {
			setPos(&e.Elif, "elif")
			setPos(&e.Then, "then")
			recurse(e.CondStmts)
			recurse(e.ThenStmts)
		}
		if len(x.ElseStmts) > 0 {
			setPos(&x.Else, "else")
			recurse(x.ElseStmts)
		}
	case *WhileClause:
		setPos(&x.While, "while")
		setPos(&x.Do, "do")
		setPos(&x.Done, "done")
		recurse(x.CondStmts)
		recurse(x.DoStmts)
	case *UntilClause:
		setPos(&x.Until, "until")
		setPos(&x.Do, "do")
		setPos(&x.Done, "done")
		recurse(x.CondStmts)
		recurse(x.DoStmts)
	case *ForClause:
		setPos(&x.For, "for")
		setPos(&x.Do, "do")
		setPos(&x.Done, "done")
		recurse(x.Loop)
		recurse(x.DoStmts)
	case *WordIter:
		recurse(x.Name)
		recurse(x.List)
	case *CStyleLoop:
		setPos(&x.Lparen, "((")
		setPos(&x.Rparen, "))")
		recurse(x.Init)
		recurse(x.Cond)
		recurse(x.Post)
	case *SglQuoted:
		checkSrc(x.End()-1, "'")
		valuePos := x.Position + 1
		if x.Dollar {
			valuePos++
		}
		checkSrc(valuePos, x.Value)
		if x.Dollar {
			setPos(&x.Position, "$'")
		} else {
			setPos(&x.Position, "'")
		}
	case *DblQuoted:
		checkSrc(x.End()-1, `"`)
		if x.Dollar {
			setPos(&x.Position, `$"`)
		} else {
			setPos(&x.Position, `"`)
		}
		recurse(x.Parts)
	case *UnaryArithm:
		setPos(&x.OpPos, x.Op.String())
		recurse(x.X)
	case *UnaryTest:
		strs := []string{x.Op.String()}
		switch x.Op {
		case TsExists:
			strs = append(strs, "-a")
		case TsSmbLink:
			strs = append(strs, "-h")
		}
		setPos(&x.OpPos, strs...)
		recurse(x.X)
	case *BinaryCmd:
		setPos(&x.OpPos, x.Op.String())
		recurse(x.X)
		recurse(x.Y)
	case *BinaryArithm:
		setPos(&x.OpPos, x.Op.String())
		recurse(x.X)
		recurse(x.Y)
	case *BinaryTest:
		strs := []string{x.Op.String()}
		switch x.Op {
		case TsEqual:
			strs = append(strs, "=")
		}
		setPos(&x.OpPos, strs...)
		recurse(x.X)
		recurse(x.Y)
	case *ParenArithm:
		setPos(&x.Lparen, "(")
		setPos(&x.Rparen, ")")
		recurse(x.X)
	case *ParenTest:
		setPos(&x.Lparen, "(")
		setPos(&x.Rparen, ")")
		recurse(x.X)
	case *FuncDecl:
		if x.BashStyle {
			setPos(&x.Position, "function")
		} else {
			setPos(&x.Position)
		}
		recurse(x.Name)
		recurse(x.Body)
	case *ParamExp:
		setPos(&x.Dollar, "$")
		if !x.Short {
			setPos(&x.Rbrace, "}")
		}
		if x.Param != nil {
			recurse(x.Param)
		}
		if x.Ind != nil {
			recurse(x.Ind.Expr)
		}
		if x.Slice != nil {
			recurse(x.Slice.Offset)
			if x.Slice.Length != nil {
				recurse(x.Slice.Length)
			}
		}
		if x.Repl != nil {
			recurse(x.Repl.Orig)
			recurse(x.Repl.With)
		}
		if x.Exp != nil {
			recurse(x.Exp.Word)
		}
	case *ArithmExp:
		if x.Bracket {
			// deprecated $(( form
			setPos(&x.Left, "$[")
			setPos(&x.Right, "]")
		} else {
			setPos(&x.Left, "$((")
			setPos(&x.Right, "))")
		}
		recurse(x.X)
	case *ArithmCmd:
		setPos(&x.Left, "((")
		setPos(&x.Right, "))")
		recurse(x.X)
	case *CmdSubst:
		setPos(&x.Left, "$(", "`")
		setPos(&x.Right, ")", "`")
		recurse(x.Stmts)
	case *CaseClause:
		setPos(&x.Case, "case")
		setPos(&x.Esac, "esac")
		recurse(x.Word)
		for _, pl := range x.List {
			setPos(&pl.OpPos, pl.Op.String(), "esac")
			recurse(pl.Patterns)
			recurse(pl.Stmts)
		}
	case *TestClause:
		setPos(&x.Left, "[[")
		setPos(&x.Right, "]]")
		recurse(x.X)
	case *DeclClause:
		if x.Variant == "" {
			setPos(&x.Position, "declare", "typeset")
		} else {
			setPos(&x.Position, x.Variant)
		}
		recurse(x.Opts)
		recurse(x.Assigns)
	case *EvalClause:
		setPos(&x.Eval, "eval")
		if x.Stmt != nil {
			recurse(x.Stmt)
		}
	case *CoprocClause:
		setPos(&x.Coproc, "coproc")
		if x.Name != nil {
			recurse(x.Name)
		}
		recurse(x.Stmt)
	case *LetClause:
		setPos(&x.Let, "let")
		for _, expr := range x.Exprs {
			recurse(expr)
		}
	case *ArrayExpr:
		setPos(&x.Lparen, "(")
		setPos(&x.Rparen, ")")
		recurse(x.List)
	case *ExtGlob:
		setPos(&x.OpPos, x.Op.String())
		checkSrc(x.Pattern.End(), ")")
		recurse(x.Pattern)
	case *ProcSubst:
		setPos(&x.OpPos, x.Op.String())
		setPos(&x.Rparen, ")")
		recurse(x.Stmts)
	case nil:
	default:
		panic(reflect.TypeOf(v))
	}
}

func checkNewlines(tb testing.TB, src string, got []Pos) {
	want := []Pos{0}
	for i, b := range src {
		if b == '\n' {
			want = append(want, Pos(i+1))
		}
	}
	if !reflect.DeepEqual(got, want) {
		tb.Fatalf("Unexpected newline offsets at %q:\ngot:  %v\nwant: %v",
			src, got, want)
	}
}
