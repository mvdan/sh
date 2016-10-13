// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package tests

import (
	"reflect"
	"strings"
	"testing"

	. "github.com/mvdan/sh/ast"
	. "github.com/mvdan/sh/token"
)

func prepareTest(c *TestCase) {
	c.common = fullProg(c.common)
	c.bash = fullProg(c.bash)
	c.posix = fullProg(c.posix)
	if f, ok := c.common.(*File); ok && f != nil {
		c.All = append(c.All, f)
		c.Bash = f
		c.Posix = f
	}
	if f, ok := c.bash.(*File); ok && f != nil {
		c.Bash = f
	}
	if f, ok := c.posix.(*File); ok && f != nil {
		c.Posix = f
	}
}

func init() {
	for i := range FileTests {
		prepareTest(&FileTests[i])
	}
	for i := range FileTestsNoPrint {
		prepareTest(&FileTestsNoPrint[i])
	}
}

func lit(s string) *Lit { return &Lit{Value: s} }
func lits(strs ...string) []WordPart {
	l := make([]WordPart, len(strs))
	for i, s := range strs {
		l[i] = lit(s)
	}
	return l
}

func word(ps ...WordPart) *Word { return &Word{Parts: ps} }
func litWord(s string) *Word    { return word(lits(s)...) }
func litWords(strs ...string) []Word {
	l := make([]Word, 0, len(strs))
	for _, s := range strs {
		l = append(l, *litWord(s))
	}
	return l
}

func call(words ...Word) *CallExpr     { return &CallExpr{Args: words} }
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

func sglQuoted(s string) *SglQuoted     { return &SglQuoted{Value: s} }
func dblQuoted(ps ...WordPart) *Quoted  { return &Quoted{Quote: DQUOTE, Parts: ps} }
func block(sts ...*Stmt) *Block         { return &Block{Stmts: sts} }
func subshell(sts ...*Stmt) *Subshell   { return &Subshell{Stmts: sts} }
func arithmExp(e ArithmExpr) *ArithmExp { return &ArithmExp{Token: DOLLDP, X: e} }
func parenExpr(e ArithmExpr) *ParenExpr { return &ParenExpr{X: e} }

func cmdSubst(sts ...*Stmt) *CmdSubst { return &CmdSubst{Stmts: sts} }
func bckQuoted(sts ...*Stmt) *CmdSubst {
	return &CmdSubst{Backquotes: true, Stmts: sts}
}
func litParamExp(s string) *ParamExp {
	return &ParamExp{Short: true, Param: *lit(s)}
}
func letClause(exps ...ArithmExpr) *LetClause {
	return &LetClause{Exprs: exps}
}

type TestCase struct {
	Strs                []string
	common, bash, posix interface{}
	All                 []*File
	Bash, Posix         *File
}

var FileTests = []TestCase{
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
		bash: stmt(&ArithmExp{Token: DLPAREN, X: &BinaryExpr{
			Op: EQL,
			X:  litWord("a"),
			Y:  litWord("2"),
		}}),
		posix: subshell(stmt(subshell(litStmt("a", "==", "2")))),
	},
	{
		Strs: []string{"if ((1 > 2)); then b; fi"},
		bash: &IfClause{
			CondStmts: stmts(&ArithmExp{
				Token: DLPAREN,
				X: &BinaryExpr{
					Op: GTR,
					X:  litWord("1"),
					Y:  litWord("2"),
				},
			}),
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
			CondStmts: stmts(&ArithmExp{
				Token: DLPAREN,
				X: &BinaryExpr{
					Op: GTR,
					X:  litWord("1"),
					Y:  litWord("2"),
				},
			}),
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
			Loop: &WordIter{
				Name: *lit("i"),
			},
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
				Name: *lit("i"),
				List: litWords("1", "2", "3"),
			},
			DoStmts: stmts(call(
				*litWord("echo"),
				*word(litParamExp("i")),
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
				Init: &BinaryExpr{
					Op: ASSIGN,
					X:  litWord("i"),
					Y:  litWord("0"),
				},
				Cond: &BinaryExpr{
					Op: LSS,
					X:  litWord("i"),
					Y:  litWord("10"),
				},
				Post: &UnaryExpr{
					Op:   INC,
					Post: true,
					X:    litWord("i"),
				},
			},
			DoStmts: stmts(call(
				*litWord("echo"),
				*word(litParamExp("i")),
			)),
		},
	},
	{
		Strs: []string{`' ' "foo bar"`},
		common: call(
			*word(sglQuoted(" ")),
			*word(dblQuoted(lits("foo bar")...)),
		),
	},
	{
		Strs:   []string{`"foo \" bar"`},
		common: word(dblQuoted(lits(`foo \" bar`)...)),
	},
	{
		Strs: []string{"\">foo\" \"\nbar\""},
		common: call(
			*word(dblQuoted(lit(">foo"))),
			*word(dblQuoted(lit("\nbar"))),
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
			Op: LAND,
			X:  litStmt("foo"),
			Y:  litStmt("bar"),
		},
	},
	{
		Strs: []string{"foo \\\n\t&& bar"},
		common: &BinaryCmd{
			Op: LAND,
			X:  litStmt("foo"),
			Y:  litStmt("bar"),
		},
	},
	{
		Strs: []string{"foo || bar", "foo||bar", "foo ||\nbar"},
		common: &BinaryCmd{
			Op: LOR,
			X:  litStmt("foo"),
			Y:  litStmt("bar"),
		},
	},
	{
		Strs: []string{"if a; then b; fi || while a; do b; done"},
		common: &BinaryCmd{
			Op: LOR,
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
			Op: LAND,
			X:  litStmt("foo"),
			Y: stmt(&BinaryCmd{
				Op: LOR,
				X:  litStmt("bar1"),
				Y:  litStmt("bar2"),
			}),
		},
	},
	{
		Strs: []string{"foo | bar", "foo|bar", "foo |\n#etc\nbar"},
		common: &BinaryCmd{
			Op: OR,
			X:  litStmt("foo"),
			Y:  litStmt("bar"),
		},
	},
	{
		Strs: []string{"foo | bar | extra"},
		common: &BinaryCmd{
			Op: OR,
			X:  litStmt("foo"),
			Y: stmt(&BinaryCmd{
				Op: OR,
				X:  litStmt("bar"),
				Y:  litStmt("extra"),
			}),
		},
	},
	{
		Strs: []string{"foo |& bar", "foo|&bar"},
		bash: &BinaryCmd{
			Op: PIPEALL,
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
			Name: *lit("foo"),
			Body: stmt(block(litStmts("a", "b")...)),
		},
	},
	{
		Strs: []string{"foo() { a; }\nbar", "foo() {\na\n}; bar"},
		common: []Command{
			&FuncDecl{
				Name: *lit("foo"),
				Body: stmt(block(litStmts("a")...)),
			},
			litCall("bar"),
		},
	},
	{
		Strs: []string{"-foo_.,+-bar() { a; }"},
		common: &FuncDecl{
			Name: *lit("-foo_.,+-bar"),
			Body: stmt(block(litStmts("a")...)),
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
			Name:      *lit("foo"),
			Body:      stmt(block(litStmts("a", "b")...)),
		},
	},
	{
		Strs: []string{"function foo() (a)"},
		bash: &FuncDecl{
			BashStyle: true,
			Name:      *lit("foo"),
			Body:      stmt(subshell(litStmt("a"))),
		},
	},
	{
		Strs: []string{"a=b foo=$bar foo=start$bar"},
		common: &Stmt{
			Assigns: []*Assign{
				{Name: lit("a"), Value: *litWord("b")},
				{Name: lit("foo"), Value: *word(litParamExp("bar"))},
				{Name: lit("foo"), Value: *word(
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
				Value: *word(dblQuoted(lit("\nbar"))),
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
				{Op: GTR, Word: *litWord("a")},
				{Op: SHR, Word: *litWord("b")},
				{Op: LSS, Word: *litWord("c")},
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
				{Op: GTR, Word: *litWord("a")},
			},
		},
	},
	{
		Strs: []string{`>a >\b`},
		common: &Stmt{
			Redirs: []*Redirect{
				{Op: GTR, Word: *litWord("a")},
				{Op: GTR, Word: *litWord(`\b`)},
			},
		},
	},
	{
		Strs: []string{">a\n>b", ">a; >b"},
		common: []*Stmt{
			{Redirs: []*Redirect{
				{Op: GTR, Word: *litWord("a")},
			}},
			{Redirs: []*Redirect{
				{Op: GTR, Word: *litWord("b")},
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
					{Op: GTR, Word: *litWord("r2")},
				},
			},
		},
	},
	{
		Strs: []string{"foo >bar`etc`", "foo >b\\\nar`etc`"},
		common: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{
				{Op: GTR, Word: *word(
					lit("bar"),
					bckQuoted(litStmt("etc")),
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
				Op:   SHL,
				Word: *litWord("EOF"),
				Hdoc: *litWord("bar\n"),
			}},
		},
	},
	{
		Strs: []string{"foo <<EOF\n\nbar\nEOF"},
		common: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   SHL,
				Word: *litWord("EOF"),
				Hdoc: *litWord("\nbar\n"),
			}},
		},
	},
	{
		Strs: []string{"foo <<EOF\n1\n2\n3\nEOF"},
		common: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   SHL,
				Word: *litWord("EOF"),
				Hdoc: *litWord("1\n2\n3\n"),
			}},
		},
	},
	{
		Strs: []string{"a <<EOF\nfoo$bar\nEOF"},
		common: &Stmt{
			Cmd: litCall("a"),
			Redirs: []*Redirect{{
				Op:   SHL,
				Word: *litWord("EOF"),
				Hdoc: *word(
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
				Op:   SHL,
				Word: *litWord("EOF"),
				Hdoc: *word(
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
				Op:   SHL,
				Word: *litWord("EOF"),
				Hdoc: *word(
					lit("$"),
					lit("''"),
					litParamExp("bar"),
					lit("\n"),
				),
			}},
		},
	},
	{
		Strs: []string{"a <<EOF\n`b`\nc\nEOF"},
		common: &Stmt{
			Cmd: litCall("a"),
			Redirs: []*Redirect{{
				Op:   SHL,
				Word: *litWord("EOF"),
				Hdoc: *word(
					bckQuoted(litStmt("b")),
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
				Op:   SHL,
				Word: *litWord("EOF"),
				Hdoc: *litWord("\\${\n"),
			}},
		},
	},
	{
		Strs: []string{"{ foo <<EOF\nbar\nEOF\n}"},
		common: block(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   SHL,
				Word: *litWord("EOF"),
				Hdoc: *litWord("bar\n"),
			}},
		}),
	},
	{
		Strs: []string{"$(foo <<EOF\nbar\nEOF\n)"},
		common: cmdSubst(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   SHL,
				Word: *litWord("EOF"),
				Hdoc: *litWord("bar\n"),
			}},
		}),
	},
	{
		Strs: []string{"foo >f <<EOF\nbar\nEOF"},
		common: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{
				{Op: GTR, Word: *litWord("f")},
				{
					Op:   SHL,
					Word: *litWord("EOF"),
					Hdoc: *litWord("bar\n"),
				},
			},
		},
	},
	{
		Strs: []string{"foo <<EOF >f\nbar\nEOF"},
		common: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{
				{
					Op:   SHL,
					Word: *litWord("EOF"),
					Hdoc: *litWord("bar\n"),
				},
				{Op: GTR, Word: *litWord("f")},
			},
		},
	},
	{
		Strs: []string{"foo <<EOF && {\nbar\nEOF\n\tetc\n}"},
		common: &BinaryCmd{
			Op: LAND,
			X: &Stmt{
				Cmd: litCall("foo"),
				Redirs: []*Redirect{{
					Op:   SHL,
					Word: *litWord("EOF"),
					Hdoc: *litWord("bar\n"),
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
			Cmd: call(*word(cmdSubst(litStmt("foo")))),
			Redirs: []*Redirect{{
				Op:   SHL,
				Word: *litWord("EOF"),
				Hdoc: *litWord("bar\n"),
			}},
		},
	},
	{
		Strs: []string{
			"`\n\tfoo\n` <<EOF\nbar\nEOF",
			"<<EOF `\n\tfoo\n`\nbar\nEOF",
		},
		common: &Stmt{
			Cmd: call(*word(bckQuoted(litStmt("foo")))),
			Redirs: []*Redirect{{
				Op:   SHL,
				Word: *litWord("EOF"),
				Hdoc: *litWord("bar\n"),
			}},
		},
	},
	{
		Strs: []string{
			"$((foo)) <<EOF\nbar\nEOF",
			"<<EOF $((\n\tfoo\n))\nbar\nEOF",
		},
		common: &Stmt{
			Cmd: call(*word(arithmExp(litWord("foo")))),
			Redirs: []*Redirect{{
				Op:   SHL,
				Word: *litWord("EOF"),
				Hdoc: *litWord("bar\n"),
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
					Op:   DHEREDOC,
					Word: *litWord("EOF"),
					Hdoc: *litWord("\tbar\n\t"),
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
					Op:   DHEREDOC,
					Word: *litWord("EOF"),
					Hdoc: *litWord("\t"),
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
					Op:   SHL,
					Word: *litWord("EOF"),
					Hdoc: *litWord("bar\n"),
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
				Op:   SHL,
				Word: *litWord("FOOBAR"),
				Hdoc: *litWord("bar\n"),
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
				Op:   SHL,
				Word: *word(dblQuoted(lit("EOF"))),
				Hdoc: *litWord("bar\n"),
			}},
		},
	},
	{
		Strs: []string{"foo <<'EOF'\n${\nEOF"},
		common: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{
				{
					Op:   SHL,
					Word: *word(sglQuoted("EOF")),
					Hdoc: *litWord("${\n"),
				},
			},
		},
	},
	{
		Strs: []string{"foo <<\"EOF\"2\nbar\nEOF2"},
		common: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   SHL,
				Word: *word(dblQuoted(lit("EOF")), lit("2")),
				Hdoc: *litWord("bar\n"),
			}},
		},
	},
	{
		Strs: []string{"foo <<\\EOF\nbar\nEOF"},
		common: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   SHL,
				Word: *litWord("\\EOF"),
				Hdoc: *litWord("bar\n"),
			}},
		},
	},
	{
		Strs: []string{"foo <<$EOF\nbar\n$EOF"},
		common: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   SHL,
				Word: *word(litParamExp("EOF")),
				Hdoc: *litWord("bar\n"),
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
				Op:   DHEREDOC,
				Word: *litWord("EOF"),
				Hdoc: *litWord("bar\n"),
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
				Op:   DHEREDOC,
				Word: *litWord("EOF"),
				Hdoc: *litWord("\t"),
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
				Op:   DHEREDOC,
				Word: *litWord("EOF"),
				Hdoc: *litWord("\tbar\n\t"),
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
				Op:   DHEREDOC,
				Word: *word(sglQuoted("EOF")),
				Hdoc: *litWord("\tbar\n\t"),
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
					Op:   SHL,
					Word: *litWord("EOF1"),
					Hdoc: *litWord("h1\n"),
				}},
			},
			{
				Cmd: litCall("f2"),
				Redirs: []*Redirect{{
					Op:   SHL,
					Word: *litWord("EOF2"),
					Hdoc: *litWord("h2\n"),
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
					Op:   SHL,
					Word: *litWord("EOF"),
					Hdoc: *litWord("foo\n"),
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
				*litWord("foo"),
				*word(dblQuoted(lit("\narg"))),
			),
			Redirs: []*Redirect{{
				Op:   SHL,
				Word: *litWord("EOF"),
				Hdoc: *litWord("bar\n"),
			}},
		},
	},
	{
		Strs: []string{"foo >&2 <&0 2>file <>f2 &>f3 &>>f4"},
		bash: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{
				{Op: DPLOUT, Word: *litWord("2")},
				{Op: DPLIN, Word: *litWord("0")},
				{Op: GTR, N: lit("2"), Word: *litWord("file")},
				{Op: RDRINOUT, Word: *litWord("f2")},
				{Op: RDRALL, Word: *litWord("f3")},
				{Op: APPALL, Word: *litWord("f4")},
			},
		},
	},
	{
		Strs: []string{"foo 2>file bar", "2>file foo bar"},
		common: &Stmt{
			Cmd: litCall("foo", "bar"),
			Redirs: []*Redirect{
				{Op: GTR, N: lit("2"), Word: *litWord("file")},
			},
		},
	},
	{
		Strs: []string{"a >f1\nb >f2", "a >f1; b >f2"},
		common: []*Stmt{
			{
				Cmd:    litCall("a"),
				Redirs: []*Redirect{{Op: GTR, Word: *litWord("f1")}},
			},
			{
				Cmd:    litCall("b"),
				Redirs: []*Redirect{{Op: GTR, Word: *litWord("f2")}},
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
			Redirs: []*Redirect{
				{Op: WHEREDOC, Word: *litWord("input")},
			},
		},
	},
	{
		Strs: []string{
			`foo <<<"spaced input"`,
			`foo <<< "spaced input"`,
		},
		bash: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{
				{
					Op:   WHEREDOC,
					Word: *word(dblQuoted(lit("spaced input"))),
				},
			},
		},
	},
	{
		Strs: []string{"foo >(foo)"},
		bash: &Stmt{
			Cmd: call(
				*litWord("foo"),
				*word(&ProcSubst{
					Op:    CMDOUT,
					Stmts: litStmts("foo"),
				}),
			),
		},
	},
	{
		Strs: []string{"foo < <(foo)"},
		bash: &Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op: LSS,
				Word: *word(&ProcSubst{
					Op:    CMDIN,
					Stmts: litStmts("foo"),
				}),
			}},
		},
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
			{
				Cmd:        litCall("foo"),
				Background: true,
			},
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
				{Op: GTR, Word: *litWord("/dev/null")},
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
			"$((echo foo bar) )",
			"$( (echo foo bar) )",
		},
		common: cmdSubst(stmt(
			subshell(litStmt("echo", "foo", "bar")),
		)),
	},
	{
		Strs: []string{"`(foo)`"},
		common: bckQuoted(stmt(
			subshell(litStmt("foo")),
		)),
	},
	{
		Strs: []string{
			"$(\n\t(a)\n\tb\n)",
			"$( (a); b)",
			"$((a); b)",
		},
		common: cmdSubst(
			stmt(subshell(litStmt("a"))),
			litStmt("b"),
		),
	},
	{
		Strs: []string{
			"$( (a) | b)",
			"$((a) | b)",
		},
		common: cmdSubst(
			stmt(&BinaryCmd{
				Op: OR,
				X:  stmt(subshell(litStmt("a"))),
				Y:  litStmt("b"),
			}),
		),
	},
	{
		Strs: []string{
			`"$( (foo))"`,
			`"$((foo) )"`,
		},
		common: dblQuoted(
			cmdSubst(stmt(
				subshell(litStmt("foo")),
			)),
		),
	},
	{
		Strs: []string{
			"\"$( (\n\tfoo\n\tbar\n))\"",
			"\"$((\n\tfoo\n\tbar\n) )\"",
		},
		common: dblQuoted(
			cmdSubst(stmt(
				subshell(litStmts("foo", "bar")...),
			)),
		),
	},
	{
		Strs: []string{"`{ echo; }`"},
		common: bckQuoted(stmt(
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
		Strs:   []string{"$(foo bar)"},
		common: cmdSubst(litStmt("foo", "bar")),
	},
	{
		Strs: []string{"$(foo | bar)"},
		common: cmdSubst(
			stmt(&BinaryCmd{
				Op: OR,
				X:  litStmt("foo"),
				Y:  litStmt("bar"),
			}),
		),
	},
	{
		Strs: []string{"$(foo $(b1 b2))"},
		common: cmdSubst(
			stmt(call(
				*litWord("foo"),
				*word(cmdSubst(litStmt("b1", "b2"))),
			)),
		),
	},
	{
		Strs: []string{`"$(foo "bar")"`},
		common: dblQuoted(cmdSubst(
			stmt(call(
				*litWord("foo"),
				*word(dblQuoted(lit("bar"))),
			)),
		)),
	},
	{
		Strs:   []string{"`foo`"},
		common: bckQuoted(litStmt("foo")),
	},
	{
		Strs: []string{"`foo | bar`"},
		common: bckQuoted(
			stmt(&BinaryCmd{
				Op: OR,
				X:  litStmt("foo"),
				Y:  litStmt("bar"),
			}),
		),
	},
	{
		Strs: []string{"`foo 'bar'`"},
		common: bckQuoted(stmt(call(
			*litWord("foo"),
			*word(sglQuoted("bar")),
		))),
	},
	{
		Strs: []string{"`foo \"bar\"`"},
		common: bckQuoted(
			stmt(call(
				*litWord("foo"),
				*word(dblQuoted(lit("bar"))),
			)),
		),
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
		Strs: []string{`$@ $# $$ $?`},
		common: call(
			*word(litParamExp("@")),
			*word(litParamExp("#")),
			*word(litParamExp("$")),
			*word(litParamExp("?")),
		),
	},
	{
		Strs:   []string{`$`, `$ #`},
		common: litWord("$"),
	},
	{
		Strs: []string{`${@} ${$} ${?}`},
		common: call(
			*word(&ParamExp{Param: *lit("@")}),
			*word(&ParamExp{Param: *lit("$")}),
			*word(&ParamExp{Param: *lit("?")}),
		),
	},
	{
		Strs:   []string{`${foo}`},
		common: &ParamExp{Param: *lit("foo")},
	},
	{
		Strs: []string{`${foo}"bar"`},
		common: word(
			&ParamExp{Param: *lit("foo")},
			dblQuoted(lit("bar")),
		),
	},
	{
		Strs: []string{`${foo-bar}`},
		common: &ParamExp{
			Param: *lit("foo"),
			Exp: &Expansion{
				Op:   SUB,
				Word: *litWord("bar"),
			},
		},
	},
	{
		Strs: []string{`${foo+bar}"bar"`},
		common: word(
			&ParamExp{
				Param: *lit("foo"),
				Exp: &Expansion{
					Op:   ADD,
					Word: *litWord("bar"),
				},
			},
			dblQuoted(lit("bar")),
		),
	},
	{
		Strs: []string{`${foo:=<"bar"}`},
		common: &ParamExp{
			Param: *lit("foo"),
			Exp: &Expansion{
				Op:   CASSIGN,
				Word: *word(lit("<"), dblQuoted(lit("bar"))),
			},
		},
	},
	{
		Strs: []string{"${foo:=b${c}`d`}"},
		common: &ParamExp{
			Param: *lit("foo"),
			Exp: &Expansion{
				Op: CASSIGN,
				Word: *word(
					lit("b"),
					&ParamExp{Param: *lit("c")},
					bckQuoted(litStmt("d")),
				),
			},
		},
	},
	{
		Strs: []string{`${foo?"${bar}"}`},
		common: &ParamExp{
			Param: *lit("foo"),
			Exp: &Expansion{
				Op: QUEST,
				Word: *word(dblQuoted(
					&ParamExp{Param: *lit("bar")},
				)),
			},
		},
	},
	{
		Strs: []string{`${foo:?bar1 bar2}`},
		common: &ParamExp{
			Param: *lit("foo"),
			Exp: &Expansion{
				Op:   CQUEST,
				Word: *litWord("bar1 bar2"),
			},
		},
	},
	{
		Strs: []string{`${a:+b}${a:-b}${a=b}`},
		common: word(
			&ParamExp{
				Param: *lit("a"),
				Exp: &Expansion{
					Op:   CADD,
					Word: *litWord("b"),
				},
			},
			&ParamExp{
				Param: *lit("a"),
				Exp: &Expansion{
					Op:   CSUB,
					Word: *litWord("b"),
				},
			},
			&ParamExp{
				Param: *lit("a"),
				Exp: &Expansion{
					Op:   ASSIGN,
					Word: *litWord("b"),
				},
			},
		),
	},
	{
		Strs: []string{`${foo%bar}${foo%%bar*}`},
		common: word(
			&ParamExp{
				Param: *lit("foo"),
				Exp: &Expansion{
					Op:   REM,
					Word: *litWord("bar"),
				},
			},
			&ParamExp{
				Param: *lit("foo"),
				Exp: &Expansion{
					Op:   DREM,
					Word: *litWord("bar*"),
				},
			},
		),
	},
	{
		Strs: []string{`${foo#bar}${foo##bar*}`},
		common: word(
			&ParamExp{
				Param: *lit("foo"),
				Exp: &Expansion{
					Op:   HASH,
					Word: *litWord("bar"),
				},
			},
			&ParamExp{
				Param: *lit("foo"),
				Exp: &Expansion{
					Op:   DHASH,
					Word: *litWord("bar*"),
				},
			},
		),
	},
	{
		Strs: []string{`${foo%?}`},
		common: &ParamExp{
			Param: *lit("foo"),
			Exp: &Expansion{
				Op:   REM,
				Word: *litWord("?"),
			},
		},
	},
	{
		Strs: []string{`${foo::}`},
		common: &ParamExp{
			Param: *lit("foo"),
			Exp: &Expansion{
				Op:   COLON,
				Word: *litWord(":"),
			},
		},
	},
	{
		Strs: []string{`${foo[bar]}`},
		common: &ParamExp{
			Param: *lit("foo"),
			Ind: &Index{
				Word: *litWord("bar"),
			},
		},
	},
	{
		Strs: []string{`${foo[bar]-etc}`},
		common: &ParamExp{
			Param: *lit("foo"),
			Ind: &Index{
				Word: *litWord("bar"),
			},
			Exp: &Expansion{
				Op:   SUB,
				Word: *litWord("etc"),
			},
		},
	},
	{
		Strs: []string{`${foo[${bar}]}`},
		common: &ParamExp{
			Param: *lit("foo"),
			Ind: &Index{
				Word: *word(&ParamExp{Param: *lit("bar")}),
			},
		},
	},
	{
		Strs: []string{`${foo/b1/b2}`},
		common: &ParamExp{
			Param: *lit("foo"),
			Repl: &Replace{
				Orig: *litWord("b1"),
				With: *litWord("b2"),
			},
		},
	},
	{
		Strs: []string{`${foo/a b/c d}`},
		common: &ParamExp{
			Param: *lit("foo"),
			Repl: &Replace{
				Orig: *litWord("a b"),
				With: *litWord("c d"),
			},
		},
	},
	{
		Strs: []string{`${foo/[/]}`},
		common: &ParamExp{
			Param: *lit("foo"),
			Repl: &Replace{
				Orig: *litWord("["),
				With: *litWord("]"),
			},
		},
	},
	{
		Strs: []string{`${foo/bar/b/a/r}`},
		common: &ParamExp{
			Param: *lit("foo"),
			Repl: &Replace{
				Orig: *litWord("bar"),
				With: *litWord("b/a/r"),
			},
		},
	},
	{
		Strs: []string{`${foo/$a/$b}`},
		common: &ParamExp{
			Param: *lit("foo"),
			Repl: &Replace{
				Orig: *word(litParamExp("a")),
				With: *word(litParamExp("b")),
			},
		},
	},
	{
		Strs: []string{`${foo//b1/b2}`},
		common: &ParamExp{
			Param: *lit("foo"),
			Repl: &Replace{
				All:  true,
				Orig: *litWord("b1"),
				With: *litWord("b2"),
			},
		},
	},
	{
		Strs: []string{
			`${foo//#/}`,
			`${foo//#}`,
		},
		common: &ParamExp{
			Param: *lit("foo"),
			Repl: &Replace{
				All:  true,
				Orig: *litWord("#"),
			},
		},
	},
	{
		Strs: []string{`${#foo}`},
		common: &ParamExp{
			Length: true,
			Param:  *lit("foo"),
		},
	},
	{
		Strs: []string{`${#} ${#?}`},
		common: call(
			*word(&ParamExp{Length: true}),
			*word(&ParamExp{Length: true, Param: *lit("?")}),
		),
	},
	{
		Strs:   []string{`"${foo}"`},
		common: dblQuoted(&ParamExp{Param: *lit("foo")}),
	},
	{
		Strs:   []string{`"(foo)"`},
		common: dblQuoted(lit("(foo)")),
	},
	{
		Strs: []string{`"${foo}>"`},
		common: dblQuoted(
			&ParamExp{Param: *lit("foo")},
			lit(">"),
		),
	},
	{
		Strs:   []string{`"$(foo)"`},
		common: dblQuoted(cmdSubst(litStmt("foo"))),
	},
	{
		Strs:   []string{`"$(foo bar)"`, `"$(foo  bar)"`},
		common: dblQuoted(cmdSubst(litStmt("foo", "bar"))),
	},
	{
		Strs:   []string{"\"`foo`\""},
		common: dblQuoted(bckQuoted(litStmt("foo"))),
	},
	{
		Strs:   []string{"\"`foo bar`\"", "\"`foo  bar`\""},
		common: dblQuoted(bckQuoted(litStmt("foo", "bar"))),
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
		Strs: []string{"$((1 + 3))", "$((1+3))", "$[1+3]"},
		bash: arithmExp(&BinaryExpr{
			Op: ADD,
			X:  litWord("1"),
			Y:  litWord("3"),
		}),
	},
	{
		Strs: []string{`"$((foo))"`, `"$[foo]"`},
		bash: dblQuoted(arithmExp(
			litWord("foo"),
		)),
	},
	{
		Strs: []string{`$((arr[0]++))`},
		common: arithmExp(
			&UnaryExpr{
				Op:   INC,
				Post: true,
				X:    litWord("arr[0]"),
			},
		),
	},
	{
		Strs: []string{"$((5 * 2 - 1))", "$((5*2-1))"},
		common: arithmExp(&BinaryExpr{
			Op: SUB,
			X: &BinaryExpr{
				Op: MUL,
				X:  litWord("5"),
				Y:  litWord("2"),
			},
			Y: litWord("1"),
		}),
	},
	{
		Strs: []string{"$(($i | 13))"},
		common: arithmExp(&BinaryExpr{
			Op: OR,
			X:  word(litParamExp("i")),
			Y:  litWord("13"),
		}),
	},
	{
		Strs: []string{"$((3 & $((4))))"},
		common: arithmExp(&BinaryExpr{
			Op: AND,
			X:  litWord("3"),
			Y:  word(arithmExp(litWord("4"))),
		}),
	},
	{
		Strs: []string{
			"$((3 % 7))",
			"$((3\n% 7))",
			"$((3\\\n % 7))",
		},
		common: arithmExp(&BinaryExpr{
			Op: REM,
			X:  litWord("3"),
			Y:  litWord("7"),
		}),
	},
	{
		Strs: []string{`"$((1 / 3))"`},
		common: dblQuoted(arithmExp(&BinaryExpr{
			Op: QUO,
			X:  litWord("1"),
			Y:  litWord("3"),
		})),
	},
	{
		Strs: []string{"$((2 ** 10))"},
		common: arithmExp(&BinaryExpr{
			Op: POW,
			X:  litWord("2"),
			Y:  litWord("10"),
		}),
	},
	{
		Strs: []string{`$(((1) ^ 3))`},
		common: arithmExp(&BinaryExpr{
			Op: XOR,
			X:  parenExpr(litWord("1")),
			Y:  litWord("3"),
		}),
	},
	{
		Strs: []string{`$((1 >> (3 << 2)))`},
		common: arithmExp(&BinaryExpr{
			Op: SHR,
			X:  litWord("1"),
			Y: parenExpr(&BinaryExpr{
				Op: SHL,
				X:  litWord("3"),
				Y:  litWord("2"),
			}),
		}),
	},
	{
		Strs: []string{`$((-(1)))`},
		common: arithmExp(&UnaryExpr{
			Op: SUB,
			X:  parenExpr(litWord("1")),
		}),
	},
	{
		Strs: []string{`$((i++))`},
		common: arithmExp(&UnaryExpr{
			Op:   INC,
			Post: true,
			X:    litWord("i"),
		}),
	},
	{
		Strs: []string{`$((--i))`},
		common: arithmExp(&UnaryExpr{
			Op: DEC,
			X:  litWord("i"),
		}),
	},
	{
		Strs: []string{`$((!i))`},
		common: arithmExp(&UnaryExpr{
			Op: NOT,
			X:  litWord("i"),
		}),
	},
	{
		Strs: []string{`$((-!+i))`},
		common: arithmExp(&UnaryExpr{
			Op: SUB,
			X: &UnaryExpr{
				Op: NOT,
				X: &UnaryExpr{
					Op: ADD,
					X:  litWord("i"),
				},
			},
		}),
	},
	{
		Strs: []string{`$((!!i))`},
		common: arithmExp(&UnaryExpr{
			Op: NOT,
			X: &UnaryExpr{
				Op: NOT,
				X:  litWord("i"),
			},
		}),
	},
	{
		Strs: []string{`$((1 < 3))`},
		common: arithmExp(&BinaryExpr{
			Op: LSS,
			X:  litWord("1"),
			Y:  litWord("3"),
		}),
	},
	{
		Strs: []string{`$((i = 2))`, `$((i=2))`},
		common: arithmExp(&BinaryExpr{
			Op: ASSIGN,
			X:  litWord("i"),
			Y:  litWord("2"),
		}),
	},
	{
		Strs: []string{"$((a += 2, b -= 3))"},
		common: arithmExp(&BinaryExpr{
			Op: COMMA,
			X: &BinaryExpr{
				Op: ADDASSGN,
				X:  litWord("a"),
				Y:  litWord("2"),
			},
			Y: &BinaryExpr{
				Op: SUBASSGN,
				X:  litWord("b"),
				Y:  litWord("3"),
			},
		}),
	},
	{
		Strs: []string{"$((a >>= 2, b <<= 3))"},
		common: arithmExp(&BinaryExpr{
			Op: COMMA,
			X: &BinaryExpr{
				Op: SHRASSGN,
				X:  litWord("a"),
				Y:  litWord("2"),
			},
			Y: &BinaryExpr{
				Op: SHLASSGN,
				X:  litWord("b"),
				Y:  litWord("3"),
			},
		}),
	},
	{
		Strs: []string{"$((a == b && c > d))"},
		common: arithmExp(&BinaryExpr{
			Op: LAND,
			X: &BinaryExpr{
				Op: EQL,
				X:  litWord("a"),
				Y:  litWord("b"),
			},
			Y: &BinaryExpr{
				Op: GTR,
				X:  litWord("c"),
				Y:  litWord("d"),
			},
		}),
	},
	{
		Strs: []string{"$((a != b))"},
		common: arithmExp(&BinaryExpr{
			Op: NEQ,
			X:  litWord("a"),
			Y:  litWord("b"),
		}),
	},
	{
		Strs: []string{"$((a &= b))"},
		common: arithmExp(&BinaryExpr{
			Op: ANDASSGN,
			X:  litWord("a"),
			Y:  litWord("b"),
		}),
	},
	{
		Strs: []string{"$((a |= b))"},
		common: arithmExp(&BinaryExpr{
			Op: ORASSGN,
			X:  litWord("a"),
			Y:  litWord("b"),
		}),
	},
	{
		Strs: []string{"$((a %= b))"},
		common: arithmExp(&BinaryExpr{
			Op: REMASSGN,
			X:  litWord("a"),
			Y:  litWord("b"),
		}),
	},
	{
		Strs: []string{"$((a /= b))"},
		common: arithmExp(&BinaryExpr{
			Op: QUOASSGN,
			X:  litWord("a"),
			Y:  litWord("b"),
		}),
	},
	{
		Strs: []string{"$((a ^= b))"},
		common: arithmExp(&BinaryExpr{
			Op: XORASSGN,
			X:  litWord("a"),
			Y:  litWord("b"),
		}),
	},
	{
		Strs: []string{"$((i *= 3))"},
		common: arithmExp(&BinaryExpr{
			Op: MULASSGN,
			X:  litWord("i"),
			Y:  litWord("3"),
		}),
	},
	{
		Strs: []string{"$((2 >= 10))"},
		common: arithmExp(&BinaryExpr{
			Op: GEQ,
			X:  litWord("2"),
			Y:  litWord("10"),
		}),
	},
	{
		Strs: []string{"$((foo ? b1 : b2))"},
		common: arithmExp(&BinaryExpr{
			Op: QUEST,
			X:  litWord("foo"),
			Y: &BinaryExpr{
				Op: COLON,
				X:  litWord("b1"),
				Y:  litWord("b2"),
			},
		}),
	},
	{
		Strs: []string{`$((a <= (1 || 2)))`},
		common: arithmExp(&BinaryExpr{
			Op: LEQ,
			X:  litWord("a"),
			Y: parenExpr(&BinaryExpr{
				Op: LOR,
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
		Strs:  []string{`$'foo'`},
		bash:  &Quoted{Quote: DOLLSQ, Parts: lits("foo")},
		posix: word(lit("$"), sglQuoted("foo")),
	},
	{
		Strs: []string{`$'foo${'`},
		bash: &Quoted{Quote: DOLLSQ, Parts: lits("foo${")},
	},
	{
		Strs: []string{"$'foo bar`'"},
		bash: &Quoted{Quote: DOLLSQ, Parts: lits("foo bar`")},
	},
	{
		Strs: []string{"$'f\\'oo\n'"},
		bash: &Quoted{Quote: DOLLSQ, Parts: lits("f\\'oo\n")},
	},
	{
		Strs:  []string{`$"foo"`},
		bash:  &Quoted{Quote: DOLLDQ, Parts: lits("foo")},
		posix: word(lit("$"), dblQuoted(lit("foo"))),
	},
	{
		Strs: []string{`$"foo$"`},
		bash: &Quoted{Quote: DOLLDQ, Parts: lits("foo", "$")},
	},
	{
		Strs: []string{`$"foo bar"`},
		bash: &Quoted{Quote: DOLLDQ, Parts: lits(`foo bar`)},
	},
	{
		Strs: []string{`$"f\"oo"`},
		bash: &Quoted{Quote: DOLLDQ, Parts: lits(`f\"oo`)},
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
		Strs: []string{"`foo$`"},
		common: bckQuoted(
			stmt(call(*word(lit("foo"), lit("$")))),
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
		common: word(lit("foo"), &ParamExp{Param: *lit("bar")}),
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
			Word: *word(litParamExp("i")),
			List: []*PatternList{
				{
					Op:       DSEMICOLON,
					Patterns: litWords("1"),
					Stmts:    litStmts("foo"),
				},
				{
					Op:       DSEMICOLON,
					Patterns: litWords("2", "3*"),
					Stmts:    litStmts("bar"),
				},
			},
		},
	},
	{
		Strs: []string{"case $i in 1) a ;;& 2) b ;& 3) c ;; esac"},
		bash: &CaseClause{
			Word: *word(litParamExp("i")),
			List: []*PatternList{
				{
					Op:       DSEMIFALL,
					Patterns: litWords("1"),
					Stmts:    litStmts("a"),
				},
				{
					Op:       SEMIFALL,
					Patterns: litWords("2"),
					Stmts:    litStmts("b"),
				},
				{
					Op:       DSEMICOLON,
					Patterns: litWords("3"),
					Stmts:    litStmts("c"),
				},
			},
		},
	},
	{
		Strs: []string{"case $i in 1) cat <<EOF ;;\nfoo\nEOF\nesac"},
		common: &CaseClause{
			Word: *word(litParamExp("i")),
			List: []*PatternList{{
				Op:       DSEMICOLON,
				Patterns: litWords("1"),
				Stmts: []*Stmt{{
					Cmd: litCall("cat"),
					Redirs: []*Redirect{{
						Op:   SHL,
						Word: *litWord("EOF"),
						Hdoc: *litWord("foo\n"),
					}},
				}},
			}},
		},
	},
	{
		Strs: []string{"foo | while read a; do b; done"},
		common: &BinaryCmd{
			Op: OR,
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
				Op: LOR,
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
		common: word(&ParamExp{Param: *lit("foo")}, lit("if")),
	},
	{
		Strs:   []string{"$if"},
		common: litParamExp("if"),
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
					{Op: GTR, Word: *litWord("f")},
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
				{Name: lit("a"), Value: *litWord("b")},
			}},
			{Assigns: []*Assign{
				{Name: lit("c"), Value: *litWord("d")},
			}},
		},
	},
	{
		Strs: []string{"foo && write | read"},
		common: &BinaryCmd{
			Op: LAND,
			X:  litStmt("foo"),
			Y: stmt(&BinaryCmd{
				Op: OR,
				X:  litStmt("write"),
				Y:  litStmt("read"),
			}),
		},
	},
	{
		Strs: []string{"write | read && bar"},
		common: &BinaryCmd{
			Op: LAND,
			X: stmt(&BinaryCmd{
				Op: OR,
				X:  litStmt("write"),
				Y:  litStmt("read"),
			}),
			Y: litStmt("bar"),
		},
	},
	{
		Strs: []string{"foo >f | bar"},
		common: &BinaryCmd{
			Op: OR,
			X: &Stmt{
				Cmd: litCall("foo"),
				Redirs: []*Redirect{
					{Op: GTR, Word: *litWord("f")},
				},
			},
			Y: litStmt("bar"),
		},
	},
	{
		Strs: []string{"(foo) >f | bar"},
		common: &BinaryCmd{
			Op: OR,
			X: &Stmt{
				Cmd: subshell(litStmt("foo")),
				Redirs: []*Redirect{
					{Op: GTR, Word: *litWord("f")},
				},
			},
			Y: litStmt("bar"),
		},
	},
	{
		Strs: []string{"foo | >f"},
		common: &BinaryCmd{
			Op: OR,
			X:  litStmt("foo"),
			Y: &Stmt{
				Redirs: []*Redirect{
					{Op: GTR, Word: *litWord("f")},
				},
			},
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
			&TestClause{
				X: litWord("a"),
			},
			litCall("b"),
		),
	},
	{
		Strs: []string{"[[ a > b ]]"},
		bash: &TestClause{
			X: &BinaryExpr{
				Op: GTR,
				X:  litWord("a"),
				Y:  litWord("b"),
			},
		},
	},
	{
		Strs: []string{"[[ 1 -eq 2 ]]"},
		bash: &TestClause{
			X: &BinaryExpr{
				Op: TEQL,
				X:  litWord("1"),
				Y:  litWord("2"),
			},
		},
	},
	{
		Strs: []string{"[[ a =~ b ]]"},
		bash: &TestClause{
			X: &BinaryExpr{
				Op: TREMATCH,
				X:  litWord("a"),
				Y:  litWord("b"),
			},
		},
	},
	{
		Strs: []string{`[[ a =~ " foo "$bar ]]`},
		bash: &TestClause{
			X: &BinaryExpr{
				Op: TREMATCH,
				X:  litWord("a"),
				Y:  litWord(`" foo "$bar`),
			},
		},
	},
	{
		Strs: []string{`[[ a =~ [ab](c |d) ]]`},
		bash: &TestClause{
			X: &BinaryExpr{
				Op: TREMATCH,
				X:  litWord("a"),
				Y:  litWord("[ab](c |d)"),
			},
		},
	},
	{
		Strs: []string{"[[ -n $a ]]"},
		bash: &TestClause{
			X: &UnaryExpr{
				Op: TNEMPSTR,
				X:  word(litParamExp("a")),
			},
		},
	},
	{
		Strs: []string{"[[ ! $a < 'b' ]]"},
		bash: &TestClause{
			X: &UnaryExpr{
				Op: NOT,
				X: &BinaryExpr{
					Op: LSS,
					X:  word(litParamExp("a")),
					Y:  word(sglQuoted("b")),
				},
			},
		},
	},
	{
		Strs: []string{
			"[[ ! -e $a ]]",
			"[[ ! -a $a ]]",
		},
		bash: &TestClause{
			X: &UnaryExpr{
				Op: NOT,
				X: &UnaryExpr{
					Op: TEXISTS,
					X:  word(litParamExp("a")),
				},
			},
		},
	},
	{
		Strs: []string{"[[ (a && b) ]]"},
		bash: &TestClause{
			X: parenExpr(&BinaryExpr{
				Op: LAND,
				X:  litWord("a"),
				Y:  litWord("b"),
			}),
		},
	},
	{
		Strs: []string{"[[ (a && b) || c ]]"},
		bash: &TestClause{
			X: &BinaryExpr{
				Op: LOR,
				X: parenExpr(&BinaryExpr{
					Op: LAND,
					X:  litWord("a"),
					Y:  litWord("b"),
				}),
				Y: litWord("c"),
			},
		},
	},
	{
		Strs: []string{
			"[[ -S a && -L b ]]",
			"[[ -S a && -h b ]]",
		},
		bash: &TestClause{
			X: &BinaryExpr{
				Op: LAND,
				X: &UnaryExpr{
					Op: TSOCKET,
					X:  litWord("a"),
				},
				Y: &UnaryExpr{
					Op: TSMBLINK,
					X:  litWord("b"),
				},
			},
		},
	},
	{
		Strs: []string{"[[ a > b && c > d ]]"},
		bash: &TestClause{
			X: &BinaryExpr{
				Op: GTR,
				X:  litWord("a"),
				Y: &BinaryExpr{
					Op: LAND,
					X:  litWord("b"),
					Y: &BinaryExpr{
						Op: GTR,
						X:  litWord("c"),
						Y:  litWord("d"),
					},
				},
			},
		},
	},
	{
		Strs: []string{"[[ a == b && c != d ]]"},
		bash: &TestClause{
			X: &BinaryExpr{
				Op: EQL,
				X:  litWord("a"),
				Y: &BinaryExpr{
					Op: LAND,
					X:  litWord("b"),
					Y: &BinaryExpr{
						Op: NEQ,
						X:  litWord("c"),
						Y:  litWord("d"),
					},
				},
			},
		},
	},
	{
		Strs: []string{"declare -f func"},
		bash: &DeclClause{
			Opts: litWords("-f"),
			Assigns: []*Assign{
				{Value: *litWord("func")},
			},
		},
		posix: litStmt("declare", "-f", "func"),
	},
	{
		Strs: []string{"local bar"},
		bash: &DeclClause{
			Local:   true,
			Assigns: []*Assign{{Value: *litWord("bar")}},
		},
		posix: litStmt("local", "bar"),
	},
	{
		Strs: []string{"declare -a -bc foo=bar"},
		bash: &DeclClause{
			Opts: litWords("-a", "-bc"),
			Assigns: []*Assign{
				{Name: lit("foo"), Value: *litWord("bar")},
			},
		},
	},
	{
		Strs: []string{"declare -a foo=(b1 `b2`)"},
		bash: &DeclClause{
			Opts: litWords("-a"),
			Assigns: []*Assign{{
				Name: lit("foo"),
				Value: *word(
					&ArrayExpr{List: []Word{
						*litWord("b1"),
						*word(bckQuoted(litStmt("b2"))),
					}},
				),
			}},
		},
	},
	{
		Strs: []string{"local -a foo=(b1 `b2`)"},
		bash: &DeclClause{
			Local: true,
			Opts:  litWords("-a"),
			Assigns: []*Assign{{
				Name: lit("foo"),
				Value: *word(
					&ArrayExpr{List: []Word{
						*litWord("b1"),
						*word(bckQuoted(litStmt("b2"))),
					}},
				),
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
				Op: LAND,
				X:  litStmt("a"),
				Y: &Stmt{Assigns: []*Assign{{
					Name: lit("b"),
					Value: *word(&ArrayExpr{
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
				Opts: litWords("-f"),
				Assigns: []*Assign{
					{Value: *litWord("func")},
				},
			},
			Redirs: []*Redirect{
				{Op: GTR, Word: *litWord("/dev/null")},
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
				Value: *litWord("b"),
			}},
		}},
	},
	{
		Strs: []string{`let i++`},
		bash: letClause(
			&UnaryExpr{
				Op:   INC,
				Post: true,
				X:    litWord("i"),
			},
		),
		posix: litStmt("let", "i++"),
	},
	{
		Strs: []string{`let a++ b++ c +d`},
		bash: letClause(
			&UnaryExpr{
				Op:   INC,
				Post: true,
				X:    litWord("a"),
			},
			&UnaryExpr{
				Op:   INC,
				Post: true,
				X:    litWord("b"),
			},
			litWord("c"),
			&UnaryExpr{
				Op: ADD,
				X:  litWord("d"),
			},
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
			Cmd: letClause(
				&UnaryExpr{
					Op: INC,
					X:  litWord("i"),
				},
			),
			Redirs: []*Redirect{
				{Op: GTR, Word: *litWord("/dev/null")},
			},
		},
	},
	{
		Strs: []string{
			`let a=(1 + 2) b=3+4`,
			`let a=(1+2) b=3+4`,
		},
		bash: letClause(
			&BinaryExpr{
				Op: ASSIGN,
				X:  litWord("a"),
				Y: parenExpr(&BinaryExpr{
					Op: ADD,
					X:  litWord("1"),
					Y:  litWord("2"),
				}),
			},
			&BinaryExpr{
				Op: ASSIGN,
				X:  litWord("b"),
				Y: &BinaryExpr{
					Op: ADD,
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
			"let i++; bar",
		},
		bash: []*Stmt{
			stmt(letClause(
				&UnaryExpr{
					Op:   INC,
					Post: true,
					X:    litWord("i"),
				},
			)),
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
			stmt(letClause(
				&UnaryExpr{
					Op:   INC,
					Post: true,
					X:    litWord("i"),
				},
			)),
			{
				Assigns: []*Assign{{
					Name: lit("foo"),
					Value: *word(
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
			Word: *word(lit("a")),
			List: []*PatternList{{
				Op:       DSEMICOLON,
				Patterns: litWords("b"),
				Stmts: stmts(letClause(
					&UnaryExpr{
						Op:   INC,
						Post: true,
						X:    litWord("i"),
					},
				)),
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
				Value: *word(
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
				Value: *word(
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
				Value:  *litWord("1"),
			}},
		},
		posix: litStmt("a+=1"),
	},
	{
		Strs: []string{"b+=(2 3)"},
		bash: &Stmt{
			Assigns: []*Assign{{
				Append: true,
				Name:   lit("b"),
				Value: *word(
					&ArrayExpr{List: litWords("2", "3")},
				),
			}},
		},
	},
	{
		Strs: []string{"<<EOF | b\nfoo\nEOF", "<<EOF|b;\nfoo\n"},
		common: &BinaryCmd{
			Op: OR,
			X: &Stmt{Redirs: []*Redirect{{
				Op:   SHL,
				Word: *litWord("EOF"),
				Hdoc: *litWord("foo\n"),
			}}},
			Y: litStmt("b"),
		},
	},
	{
		Strs: []string{"<<EOF1 <<EOF2 | c && d\nEOF1\nEOF2"},
		common: &BinaryCmd{
			Op: LAND,
			X: stmt(&BinaryCmd{
				Op: OR,
				X: &Stmt{Redirs: []*Redirect{
					{
						Op:   SHL,
						Word: *litWord("EOF1"),
						Hdoc: *word(),
					},
					{
						Op:   SHL,
						Word: *litWord("EOF2"),
						Hdoc: *word(),
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
			Op: LAND,
			X: &Stmt{Redirs: []*Redirect{{
				Op:   SHL,
				Word: *litWord("EOF"),
				Hdoc: *litWord("hdoc\n"),
			}}},
			Y: stmt(block(litStmt("bar"))),
		},
	},
	{
		Strs: []string{"foo() {\n\t<<EOF && { bar; }\nhdoc\nEOF\n}"},
		common: &FuncDecl{
			Name: *lit("foo"),
			Body: stmt(block(stmt(&BinaryCmd{
				Op: LAND,
				X: &Stmt{Redirs: []*Redirect{{
					Op:   SHL,
					Word: *litWord("EOF"),
					Hdoc: *litWord("hdoc\n"),
				}}},
				Y: stmt(block(litStmt("bar"))),
			}))),
		},
	},
	{
		Strs: []string{"\"a`\"\"`\""},
		common: dblQuoted(
			lit("a"),
			bckQuoted(
				stmt(call(
					*word(dblQuoted()),
				)),
			),
		),
	},
}

// these don't have a canonical format with the same AST
var FileTestsNoPrint = []TestCase{
	{
		Strs: []string{"<<EOF\n\\"},
		common: &Stmt{Redirs: []*Redirect{{
			Op:   SHL,
			Word: *litWord("EOF"),
			Hdoc: *litWord("\\"),
		}}},
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
		return fullProg(call(*x))
	case WordPart:
		return fullProg(word(x))
	case Command:
		return fullProg(stmt(x))
	}
	return nil
}

func SetPosRecurse(tb testing.TB, src string, v interface{}, to Pos, diff bool) {
	checkSrc := func(pos Pos, strs []string) {
		if src == "" || strs == nil {
			return
		}
		offs := int(pos - 1)
		if offs < 0 || offs > len(src) {
			tb.Fatalf("Pos() in %T is out of bounds", v)
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
			strs, offs, src, gotErr)
	}
	setPos := func(p *Pos, strs ...string) {
		checkSrc(*p, strs)
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
		if to == 0 {
			if n.End() != to {
				tb.Fatalf("Found unexpected End() in %T", n)
			}
			return
		}
		if n.Pos() > n.End() {
			tb.Fatalf("Found End() before Pos() in %T", n)
		}
	}
	recurse := func(v interface{}) {
		SetPosRecurse(tb, src, v, to, diff)
		if n, ok := v.(Node); ok {
			checkPos(n)
		}
	}
	switch x := v.(type) {
	case *File:
		recurse(x.Stmts)
		checkPos(x)
	case []*Stmt:
		for _, s := range x {
			recurse(s)
		}
	case *Stmt:
		setPos(&x.Position)
		if x.Cmd != nil {
			recurse(x.Cmd)
		}
		recurse(x.Assigns)
		for _, r := range x.Redirs {
			setPos(&r.OpPos, r.Op.String())
			if r.N != nil {
				recurse(r.N)
			}
			recurse(&r.Word)
			if len(r.Hdoc.Parts) > 0 {
				recurse(&r.Hdoc)
			}
		}
	case []*Assign:
		for _, a := range x {
			if a.Name != nil {
				recurse(a.Name)
			}
			recurse(&a.Value)
			checkPos(a)
		}
	case *CallExpr:
		recurse(x.Args)
	case []Word:
		for i := range x {
			recurse(&x[i])
		}
	case *Word:
		recurse(x.Parts)
	case []WordPart:
		for i := range x {
			recurse(&x[i])
		}
	case *WordPart:
		recurse(*x)
	case *Loop:
		recurse(*x)
	case *ArithmExpr:
		recurse(*x)
	case *Lit:
		setPos(&x.ValuePos, x.Value)
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
		recurse(&x.Loop)
		recurse(x.DoStmts)
	case *WordIter:
		recurse(&x.Name)
		recurse(x.List)
	case *CStyleLoop:
		setPos(&x.Lparen, "((")
		setPos(&x.Rparen, "))")
		recurse(&x.Init)
		recurse(&x.Cond)
		recurse(&x.Post)
	case *SglQuoted:
		setPos(&x.Quote, "'")
	case *Quoted:
		setPos(&x.QuotePos, x.Quote.String())
		recurse(x.Parts)
	case *UnaryExpr:
		strs := []string{x.Op.String()}
		switch x.Op {
		case TEXISTS:
			strs = append(strs, "-a")
		case TSMBLINK:
			strs = append(strs, "-h")
		}
		setPos(&x.OpPos, strs...)
		recurse(&x.X)
	case *BinaryCmd:
		setPos(&x.OpPos, x.Op.String())
		recurse(x.X)
		recurse(x.Y)
	case *BinaryExpr:
		setPos(&x.OpPos, x.Op.String())
		recurse(&x.X)
		recurse(&x.Y)
	case *FuncDecl:
		if x.BashStyle {
			setPos(&x.Position, "function")
		} else {
			setPos(&x.Position)
		}
		recurse(&x.Name)
		recurse(x.Body)
	case *ParamExp:
		setPos(&x.Dollar, "$")
		recurse(&x.Param)
		if x.Ind != nil {
			recurse(&x.Ind.Word)
		}
		if x.Repl != nil {
			recurse(&x.Repl.Orig)
			recurse(&x.Repl.With)
		}
		if x.Exp != nil {
			recurse(&x.Exp.Word)
		}
	case *ArithmExp:
		if src != "" && src[x.Left] == '[' {
			// deprecated $(( form
			setPos(&x.Left, "$[")
			setPos(&x.Right, "]")
		} else {
			setPos(&x.Left, x.Token.String())
			setPos(&x.Right, "))")
		}
		recurse(&x.X)
	case *ParenExpr:
		setPos(&x.Lparen, "(")
		setPos(&x.Rparen, ")")
		recurse(&x.X)
	case *CmdSubst:
		if x.Backquotes {
			setPos(&x.Left, "`")
			setPos(&x.Right, "`")
		} else {
			setPos(&x.Left, "$(")
			setPos(&x.Right, ")")
		}
		recurse(x.Stmts)
	case *CaseClause:
		setPos(&x.Case, "case")
		setPos(&x.Esac, "esac")
		recurse(&x.Word)
		for _, pl := range x.List {
			setPos(&pl.OpPos)
			recurse(pl.Patterns)
			recurse(pl.Stmts)
		}
	case *TestClause:
		setPos(&x.Left, "[[")
		setPos(&x.Right, "]]")
		recurse(x.X)
	case *DeclClause:
		if x.Local {
			setPos(&x.Declare, "local")
		} else {
			setPos(&x.Declare, "declare")
		}
		recurse(x.Opts)
		recurse(x.Assigns)
	case *EvalClause:
		setPos(&x.Eval, "eval")
		if x.Stmt != nil {
			recurse(x.Stmt)
		}
	case *LetClause:
		setPos(&x.Let, "let")
		for i := range x.Exprs {
			recurse(&x.Exprs[i])
		}
	case *ArrayExpr:
		setPos(&x.Lparen, "(")
		setPos(&x.Rparen, ")")
		recurse(x.List)
	case *ProcSubst:
		setPos(&x.OpPos, x.Op.String())
		setPos(&x.Rparen, ")")
		recurse(x.Stmts)
	case nil:
	default:
		panic(reflect.TypeOf(v))
	}
}

func CheckNewlines(tb testing.TB, src string, got []int) {
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
