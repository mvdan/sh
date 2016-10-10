// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package tests

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mvdan/sh/ast"
	"github.com/mvdan/sh/token"
)

func init() {
	for i := range FileTests {
		FileTests[i].All = fullProg(FileTests[i].All)
	}
}

func lit(s string) *ast.Lit { return &ast.Lit{Value: s} }
func lits(strs ...string) []ast.WordPart {
	l := make([]ast.WordPart, len(strs))
	for i, s := range strs {
		l[i] = lit(s)
	}
	return l
}

func word(ps ...ast.WordPart) *ast.Word { return &ast.Word{Parts: ps} }
func litWord(s string) *ast.Word        { return word(lits(s)...) }
func litWords(strs ...string) []ast.Word {
	l := make([]ast.Word, 0, len(strs))
	for _, s := range strs {
		l = append(l, *litWord(s))
	}
	return l
}

func call(words ...ast.Word) *ast.CallExpr { return &ast.CallExpr{Args: words} }
func litCall(strs ...string) *ast.CallExpr { return call(litWords(strs...)...) }

func stmt(cmd ast.Command) *ast.Stmt { return &ast.Stmt{Cmd: cmd} }
func stmts(cmds ...ast.Command) []*ast.Stmt {
	l := make([]*ast.Stmt, len(cmds))
	for i, cmd := range cmds {
		l[i] = stmt(cmd)
	}
	return l
}

func litStmt(strs ...string) *ast.Stmt { return stmt(litCall(strs...)) }
func litStmts(strs ...string) []*ast.Stmt {
	l := make([]*ast.Stmt, len(strs))
	for i, s := range strs {
		l[i] = litStmt(s)
	}
	return l
}

func sglQuoted(s string) *ast.SglQuoted         { return &ast.SglQuoted{Value: s} }
func dblQuoted(ps ...ast.WordPart) *ast.Quoted  { return &ast.Quoted{Quote: token.DQUOTE, Parts: ps} }
func block(sts ...*ast.Stmt) *ast.Block         { return &ast.Block{Stmts: sts} }
func subshell(sts ...*ast.Stmt) *ast.Subshell   { return &ast.Subshell{Stmts: sts} }
func arithmExp(e ast.ArithmExpr) *ast.ArithmExp { return &ast.ArithmExp{Token: token.DOLLDP, X: e} }
func parenExpr(e ast.ArithmExpr) *ast.ParenExpr { return &ast.ParenExpr{X: e} }

func cmdSubst(sts ...*ast.Stmt) *ast.CmdSubst { return &ast.CmdSubst{Stmts: sts} }
func bckQuoted(sts ...*ast.Stmt) *ast.CmdSubst {
	return &ast.CmdSubst{Backquotes: true, Stmts: sts}
}
func litParamExp(s string) *ast.ParamExp {
	return &ast.ParamExp{Short: true, Param: *lit(s)}
}
func letClause(exps ...ast.ArithmExpr) *ast.LetClause {
	return &ast.LetClause{Exprs: exps}
}

type TestCase struct {
	Strs []string
	All  interface{}
}

var FileTests = []TestCase{
	{
		Strs: []string{"", " ", "\t", "\n \n", "\r \r\n"},
		All:  &ast.File{},
	},
	{
		Strs: []string{"", "# foo", "# foo ( bar", "# foo'bar"},
		All:  &ast.File{},
	},
	{
		Strs: []string{"foo", "foo ", " foo", "foo # bar"},
		All:  litWord("foo"),
	},
	{
		Strs: []string{`\`},
		All:  litWord(`\`),
	},
	{
		Strs: []string{`foo\`, "f\\\noo\\"},
		All:  litWord(`foo\`),
	},
	{
		Strs: []string{`foo\a`, "f\\\noo\\a"},
		All:  litWord(`foo\a`),
	},
	{
		Strs: []string{
			"foo\nbar",
			"foo; bar;",
			"foo;bar;",
			"\nfoo\nbar\n",
			"foo\r\nbar\r\n",
		},
		All: litStmts("foo", "bar"),
	},
	{
		Strs: []string{"foo a b", " foo  a  b ", "foo \\\n a b"},
		All:  litCall("foo", "a", "b"),
	},
	{
		Strs: []string{"foobar", "foo\\\nbar", "foo\\\nba\\\nr"},
		All:  litWord("foobar"),
	},
	{
		Strs: []string{"foo", "foo \\\n"},
		All:  litWord("foo"),
	},
	{
		Strs: []string{"foo'bar'"},
		All:  word(lit("foo"), sglQuoted("bar")),
	},
	{
		Strs: []string{"(foo)", "(foo;)", "(\nfoo\n)"},
		All:  subshell(litStmt("foo")),
	},
	{
		Strs: []string{"(\n\tfoo\n\tbar\n)", "(foo; bar)"},
		All:  subshell(litStmt("foo"), litStmt("bar")),
	},
	{
		Strs: []string{"{ foo; }", "{\nfoo\n}"},
		All:  block(litStmt("foo")),
	},
	{
		Strs: []string{
			"if a; then b; fi",
			"if a\nthen\nb\nfi",
			"if a \nthen\nb\nfi",
		},
		All: &ast.IfClause{
			CondStmts: litStmts("a"),
			ThenStmts: litStmts("b"),
		},
	},
	{
		Strs: []string{
			"if a; then b; else c; fi",
			"if a\nthen b\nelse\nc\nfi",
		},
		All: &ast.IfClause{
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
		All: &ast.IfClause{
			CondStmts: litStmts("a"),
			ThenStmts: litStmts("a"),
			Elifs: []*ast.Elif{
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
		All: &ast.IfClause{
			CondStmts: []*ast.Stmt{
				litStmt("a1"),
				litStmt("a2", "foo"),
				litStmt("a3", "bar"),
			},
			ThenStmts: litStmts("b"),
		},
	},
	{
		Strs: []string{`((a <= 2))`},
		All: &ast.ArithmExp{Token: token.DLPAREN, X: &ast.BinaryExpr{
			Op: token.LEQ,
			X:  litWord("a"),
			Y:  litWord("2"),
		}},
	},
	{
		Strs: []string{"if ((1 > 2)); then b; fi"},
		All: &ast.IfClause{
			CondStmts: stmts(&ast.ArithmExp{
				Token: token.DLPAREN,
				X: &ast.BinaryExpr{
					Op: token.GTR,
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
		All: &ast.WhileClause{
			CondStmts: litStmts("a"),
			DoStmts:   litStmts("b"),
		},
	},
	{
		Strs: []string{"while { a; }; do b; done", "while { a; } do b; done"},
		All: &ast.WhileClause{
			CondStmts: stmts(block(litStmt("a"))),
			DoStmts:   litStmts("b"),
		},
	},
	{
		Strs: []string{"while (a); do b; done", "while (a) do b; done"},
		All: &ast.WhileClause{
			CondStmts: stmts(subshell(litStmt("a"))),
			DoStmts:   litStmts("b"),
		},
	},
	{
		Strs: []string{"while ((1 > 2)); do b; done"},
		All: &ast.WhileClause{
			CondStmts: stmts(&ast.ArithmExp{
				Token: token.DLPAREN,
				X: &ast.BinaryExpr{
					Op: token.GTR,
					X:  litWord("1"),
					Y:  litWord("2"),
				},
			}),
			DoStmts: litStmts("b"),
		},
	},
	{
		Strs: []string{"until a; do b; done", "until a\ndo\nb\ndone"},
		All: &ast.UntilClause{
			CondStmts: litStmts("a"),
			DoStmts:   litStmts("b"),
		},
	},
	{
		Strs: []string{
			"for i; do foo; done",
			"for i in; do foo; done",
		},
		All: &ast.ForClause{
			Loop: &ast.WordIter{
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
		All: &ast.ForClause{
			Loop: &ast.WordIter{
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
		All: &ast.ForClause{
			Loop: &ast.CStyleLoop{
				Init: &ast.BinaryExpr{
					Op: token.ASSIGN,
					X:  litWord("i"),
					Y:  litWord("0"),
				},
				Cond: &ast.BinaryExpr{
					Op: token.LSS,
					X:  litWord("i"),
					Y:  litWord("10"),
				},
				Post: &ast.UnaryExpr{
					Op:   token.INC,
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
		All: call(
			*word(sglQuoted(" ")),
			*word(dblQuoted(lits("foo bar")...)),
		),
	},
	{
		Strs: []string{`"foo \" bar"`},
		All:  word(dblQuoted(lits(`foo \" bar`)...)),
	},
	{
		Strs: []string{"\">foo\" \"\nbar\""},
		All: call(
			*word(dblQuoted(lit(">foo"))),
			*word(dblQuoted(lit("\nbar"))),
		),
	},
	{
		Strs: []string{`foo \" bar`},
		All:  litCall(`foo`, `\"`, `bar`),
	},
	{
		Strs: []string{`'"'`},
		All:  word(sglQuoted(`"`)),
	},
	{
		Strs: []string{"'`'"},
		All:  word(sglQuoted("`")),
	},
	{
		Strs: []string{`"'"`},
		All:  word(dblQuoted(lit("'"))),
	},
	{
		Strs: []string{`""`},
		All:  word(dblQuoted()),
	},
	{
		Strs: []string{"=a s{s s=s"},
		All:  litCall("=a", "s{s", "s=s"),
	},
	{
		Strs: []string{"foo && bar", "foo&&bar", "foo &&\nbar"},
		All: &ast.BinaryCmd{
			Op: token.LAND,
			X:  litStmt("foo"),
			Y:  litStmt("bar"),
		},
	},
	{
		Strs: []string{"foo \\\n\t&& bar"},
		All: &ast.BinaryCmd{
			Op: token.LAND,
			X:  litStmt("foo"),
			Y:  litStmt("bar"),
		},
	},
	{
		Strs: []string{"foo || bar", "foo||bar", "foo ||\nbar"},
		All: &ast.BinaryCmd{
			Op: token.LOR,
			X:  litStmt("foo"),
			Y:  litStmt("bar"),
		},
	},
	{
		Strs: []string{"if a; then b; fi || while a; do b; done"},
		All: &ast.BinaryCmd{
			Op: token.LOR,
			X: stmt(&ast.IfClause{
				CondStmts: litStmts("a"),
				ThenStmts: litStmts("b"),
			}),
			Y: stmt(&ast.WhileClause{
				CondStmts: litStmts("a"),
				DoStmts:   litStmts("b"),
			}),
		},
	},
	{
		Strs: []string{"foo && bar1 || bar2"},
		All: &ast.BinaryCmd{
			Op: token.LAND,
			X:  litStmt("foo"),
			Y: stmt(&ast.BinaryCmd{
				Op: token.LOR,
				X:  litStmt("bar1"),
				Y:  litStmt("bar2"),
			}),
		},
	},
	{
		Strs: []string{"foo | bar", "foo|bar", "foo |\n#etc\nbar"},
		All: &ast.BinaryCmd{
			Op: token.OR,
			X:  litStmt("foo"),
			Y:  litStmt("bar"),
		},
	},
	{
		Strs: []string{"foo | bar | extra"},
		All: &ast.BinaryCmd{
			Op: token.OR,
			X:  litStmt("foo"),
			Y: stmt(&ast.BinaryCmd{
				Op: token.OR,
				X:  litStmt("bar"),
				Y:  litStmt("extra"),
			}),
		},
	},
	{
		Strs: []string{"foo |& bar", "foo|&bar"},
		All: &ast.BinaryCmd{
			Op: token.PIPEALL,
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
		All: &ast.FuncDecl{
			Name: *lit("foo"),
			Body: stmt(block(litStmts("a", "b")...)),
		},
	},
	{
		Strs: []string{"foo() { a; }\nbar", "foo() {\na\n}; bar"},
		All: []ast.Command{
			&ast.FuncDecl{
				Name: *lit("foo"),
				Body: stmt(block(litStmts("a")...)),
			},
			litCall("bar"),
		},
	},
	{
		Strs: []string{"-foo_.,+-bar() { a; }"},
		All: &ast.FuncDecl{
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
		All: &ast.FuncDecl{
			BashStyle: true,
			Name:      *lit("foo"),
			Body:      stmt(block(litStmts("a", "b")...)),
		},
	},
	{
		Strs: []string{"function foo() (a)"},
		All: &ast.FuncDecl{
			BashStyle: true,
			Name:      *lit("foo"),
			Body:      stmt(subshell(litStmt("a"))),
		},
	},
	{
		Strs: []string{"a=b foo=$bar foo=start$bar"},
		All: &ast.Stmt{
			Assigns: []*ast.Assign{
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
		All: &ast.Stmt{
			Assigns: []*ast.Assign{{
				Name:  lit("a"),
				Value: *word(dblQuoted(lit("\nbar"))),
			}},
		},
	},
	{
		Strs: []string{"A_3a= foo"},
		All: &ast.Stmt{
			Cmd:     litCall("foo"),
			Assigns: []*ast.Assign{{Name: lit("A_3a")}},
		},
	},
	{
		Strs: []string{"à=b foo"},
		All:  litStmt("à=b", "foo"),
	},
	{
		Strs: []string{
			"foo >a >>b <c",
			"foo > a >> b < c",
			">a >>b <c foo",
		},
		All: &ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{
				{Op: token.GTR, Word: *litWord("a")},
				{Op: token.SHR, Word: *litWord("b")},
				{Op: token.LSS, Word: *litWord("c")},
			},
		},
	},
	{
		Strs: []string{
			"foo bar >a",
			"foo >a bar",
		},
		All: &ast.Stmt{
			Cmd: litCall("foo", "bar"),
			Redirs: []*ast.Redirect{
				{Op: token.GTR, Word: *litWord("a")},
			},
		},
	},
	{
		Strs: []string{`>a >\b`},
		All: &ast.Stmt{
			Redirs: []*ast.Redirect{
				{Op: token.GTR, Word: *litWord("a")},
				{Op: token.GTR, Word: *litWord(`\b`)},
			},
		},
	},
	{
		Strs: []string{">a\n>b", ">a; >b"},
		All: []*ast.Stmt{
			{Redirs: []*ast.Redirect{
				{Op: token.GTR, Word: *litWord("a")},
			}},
			{Redirs: []*ast.Redirect{
				{Op: token.GTR, Word: *litWord("b")},
			}},
		},
	},
	{
		Strs: []string{"foo1\nfoo2 >r2", "foo1; >r2 foo2"},
		All: []*ast.Stmt{
			litStmt("foo1"),
			{
				Cmd: litCall("foo2"),
				Redirs: []*ast.Redirect{
					{Op: token.GTR, Word: *litWord("r2")},
				},
			},
		},
	},
	{
		Strs: []string{"foo >bar`etc`", "foo >b\\\nar`etc`"},
		All: &ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{
				{Op: token.GTR, Word: *word(
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
		},
		All: &ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{{
				Op:   token.SHL,
				Word: *litWord("EOF"),
				Hdoc: *litWord("bar\n"),
			}},
		},
	},
	{
		Strs: []string{"foo <<EOF\n\nbar\nEOF"},
		All: &ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{{
				Op:   token.SHL,
				Word: *litWord("EOF"),
				Hdoc: *litWord("\nbar\n"),
			}},
		},
	},
	{
		Strs: []string{"foo <<EOF\n1\n2\n3\nEOF"},
		All: &ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{{
				Op:   token.SHL,
				Word: *litWord("EOF"),
				Hdoc: *litWord("1\n2\n3\n"),
			}},
		},
	},
	{
		Strs: []string{"a <<EOF\nfoo$bar\nEOF"},
		All: &ast.Stmt{
			Cmd: litCall("a"),
			Redirs: []*ast.Redirect{{
				Op:   token.SHL,
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
		All: &ast.Stmt{
			Cmd: litCall("a"),
			Redirs: []*ast.Redirect{{
				Op:   token.SHL,
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
		All: &ast.Stmt{
			Cmd: litCall("a"),
			Redirs: []*ast.Redirect{{
				Op:   token.SHL,
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
		All: &ast.Stmt{
			Cmd: litCall("a"),
			Redirs: []*ast.Redirect{{
				Op:   token.SHL,
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
		All: &ast.Stmt{
			Cmd: litCall("a"),
			Redirs: []*ast.Redirect{{
				Op:   token.SHL,
				Word: *litWord("EOF"),
				Hdoc: *litWord("\\${\n"),
			}},
		},
	},
	{
		Strs: []string{"{ foo <<EOF\nbar\nEOF\n}"},
		All: block(&ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{{
				Op:   token.SHL,
				Word: *litWord("EOF"),
				Hdoc: *litWord("bar\n"),
			}},
		}),
	},
	{
		Strs: []string{"$(foo <<EOF\nbar\nEOF\n)"},
		All: word(cmdSubst(&ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{{
				Op:   token.SHL,
				Word: *litWord("EOF"),
				Hdoc: *litWord("bar\n"),
			}},
		})),
	},
	{
		Strs: []string{"foo >f <<EOF\nbar\nEOF"},
		All: &ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{
				{Op: token.GTR, Word: *litWord("f")},
				{
					Op:   token.SHL,
					Word: *litWord("EOF"),
					Hdoc: *litWord("bar\n"),
				},
			},
		},
	},
	{
		Strs: []string{"foo <<EOF >f\nbar\nEOF"},
		All: &ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{
				{
					Op:   token.SHL,
					Word: *litWord("EOF"),
					Hdoc: *litWord("bar\n"),
				},
				{Op: token.GTR, Word: *litWord("f")},
			},
		},
	},
	{
		Strs: []string{"foo <<EOF && {\nbar\nEOF\n\tetc\n}"},
		All: &ast.BinaryCmd{
			Op: token.LAND,
			X: &ast.Stmt{
				Cmd: litCall("foo"),
				Redirs: []*ast.Redirect{{
					Op:   token.SHL,
					Word: *litWord("EOF"),
					Hdoc: *litWord("bar\n"),
				}},
			},
			Y: stmt(block(litStmt("etc"))),
		},
	},
	{
		Strs: []string{"if true; then foo <<-EOF\n\tbar\n\tEOF\nfi"},
		All: &ast.IfClause{
			CondStmts: litStmts("true"),
			ThenStmts: []*ast.Stmt{{
				Cmd: litCall("foo"),
				Redirs: []*ast.Redirect{{
					Op:   token.DHEREDOC,
					Word: *litWord("EOF"),
					Hdoc: *litWord("\tbar\n\t"),
				}},
			}},
		},
	},
	{
		Strs: []string{"if true; then foo <<-EOF\n\tEOF\nfi"},
		All: &ast.IfClause{
			CondStmts: litStmts("true"),
			ThenStmts: []*ast.Stmt{{
				Cmd: litCall("foo"),
				Redirs: []*ast.Redirect{{
					Op:   token.DHEREDOC,
					Word: *litWord("EOF"),
					Hdoc: *litWord("\t"),
				}},
			}},
		},
	},
	{
		Strs: []string{"foo <<EOF\nbar\nEOF\nfoo2"},
		All: []*ast.Stmt{
			{
				Cmd: litCall("foo"),
				Redirs: []*ast.Redirect{{
					Op:   token.SHL,
					Word: *litWord("EOF"),
					Hdoc: *litWord("bar\n"),
				}},
			},
			litStmt("foo2"),
		},
	},
	{
		Strs: []string{"foo <<FOOBAR\nbar\nFOOBAR"},
		All: &ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{{
				Op:   token.SHL,
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
		All: &ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{{
				Op:   token.SHL,
				Word: *word(dblQuoted(lit("EOF"))),
				Hdoc: *litWord("bar\n"),
			}},
		},
	},
	{
		Strs: []string{"foo <<'EOF'\n${\nEOF"},
		All: &ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{
				{
					Op:   token.SHL,
					Word: *word(sglQuoted("EOF")),
					Hdoc: *litWord("${\n"),
				},
			},
		},
	},
	{
		Strs: []string{"foo <<\"EOF\"2\nbar\nEOF2"},
		All: &ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{{
				Op:   token.SHL,
				Word: *word(dblQuoted(lit("EOF")), lit("2")),
				Hdoc: *litWord("bar\n"),
			}},
		},
	},
	{
		Strs: []string{"foo <<\\EOF\nbar\nEOF"},
		All: &ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{{
				Op:   token.SHL,
				Word: *litWord("\\EOF"),
				Hdoc: *litWord("bar\n"),
			}},
		},
	},
	{
		Strs: []string{"foo <<$EOF\nbar\n$EOF"},
		All: &ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{{
				Op:   token.SHL,
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
		All: &ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{{
				Op:   token.DHEREDOC,
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
		All: &ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{{
				Op:   token.DHEREDOC,
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
		All: &ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{{
				Op:   token.DHEREDOC,
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
		All: &ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{{
				Op:   token.DHEREDOC,
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
		All: []*ast.Stmt{
			{
				Cmd: litCall("f1"),
				Redirs: []*ast.Redirect{{
					Op:   token.SHL,
					Word: *litWord("EOF1"),
					Hdoc: *litWord("h1\n"),
				}},
			},
			{
				Cmd: litCall("f2"),
				Redirs: []*ast.Redirect{{
					Op:   token.SHL,
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
		All: []*ast.Stmt{
			{
				Cmd: litCall("a"),
				Redirs: []*ast.Redirect{{
					Op:   token.SHL,
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
		All: &ast.Stmt{
			Cmd: call(
				*litWord("foo"),
				*word(dblQuoted(lit("\narg"))),
			),
			Redirs: []*ast.Redirect{{
				Op:   token.SHL,
				Word: *litWord("EOF"),
				Hdoc: *litWord("bar\n"),
			}},
		},
	},
	{
		Strs: []string{"foo >&2 <&0 2>file <>f2 &>f3 &>>f4"},
		All: &ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{
				{Op: token.DPLOUT, Word: *litWord("2")},
				{Op: token.DPLIN, Word: *litWord("0")},
				{Op: token.GTR, N: lit("2"), Word: *litWord("file")},
				{Op: token.RDRINOUT, Word: *litWord("f2")},
				{Op: token.RDRALL, Word: *litWord("f3")},
				{Op: token.APPALL, Word: *litWord("f4")},
			},
		},
	},
	{
		Strs: []string{"foo 2>file bar", "2>file foo bar"},
		All: &ast.Stmt{
			Cmd: litCall("foo", "bar"),
			Redirs: []*ast.Redirect{
				{Op: token.GTR, N: lit("2"), Word: *litWord("file")},
			},
		},
	},
	{
		Strs: []string{"a >f1\nb >f2", "a >f1; b >f2"},
		All: []*ast.Stmt{
			{
				Cmd:    litCall("a"),
				Redirs: []*ast.Redirect{{Op: token.GTR, Word: *litWord("f1")}},
			},
			{
				Cmd:    litCall("b"),
				Redirs: []*ast.Redirect{{Op: token.GTR, Word: *litWord("f2")}},
			},
		},
	},
	{
		Strs: []string{
			"foo <<<input",
			"foo <<< input",
		},
		All: &ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{
				{Op: token.WHEREDOC, Word: *litWord("input")},
			},
		},
	},
	{
		Strs: []string{
			`foo <<<"spaced input"`,
			`foo <<< "spaced input"`,
		},
		All: &ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{
				{
					Op:   token.WHEREDOC,
					Word: *word(dblQuoted(lit("spaced input"))),
				},
			},
		},
	},
	{
		Strs: []string{"foo >(foo)"},
		All: &ast.Stmt{
			Cmd: call(
				*litWord("foo"),
				*word(&ast.ProcSubst{
					Op:    token.CMDOUT,
					Stmts: litStmts("foo"),
				}),
			),
		},
	},
	{
		Strs: []string{"foo < <(foo)"},
		All: &ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{{
				Op: token.LSS,
				Word: *word(&ast.ProcSubst{
					Op:    token.CMDIN,
					Stmts: litStmts("foo"),
				}),
			}},
		},
	},
	{
		Strs: []string{"!"},
		All:  &ast.Stmt{Negated: true},
	},
	{
		Strs: []string{"! foo"},
		All: &ast.Stmt{
			Negated: true,
			Cmd:     litCall("foo"),
		},
	},
	{
		Strs: []string{"foo &\nbar", "foo & bar", "foo&bar"},
		All: []*ast.Stmt{
			{
				Cmd:        litCall("foo"),
				Background: true,
			},
			litStmt("bar"),
		},
	},
	{
		Strs: []string{"! if foo; then bar; fi >/dev/null &"},
		All: &ast.Stmt{
			Negated: true,
			Cmd: &ast.IfClause{
				CondStmts: litStmts("foo"),
				ThenStmts: litStmts("bar"),
			},
			Redirs: []*ast.Redirect{
				{Op: token.GTR, Word: *litWord("/dev/null")},
			},
			Background: true,
		},
	},
	{
		Strs: []string{"foo#bar"},
		All:  litWord("foo#bar"),
	},
	{
		Strs: []string{"{ echo } }; }"},
		All:  block(litStmt("echo", "}", "}")),
	},
	{
		Strs: []string{"$({ echo; })"},
		All: word(cmdSubst(stmt(
			block(litStmt("echo")),
		))),
	},
	{
		Strs: []string{
			"$( (echo foo bar))",
			"$((echo foo bar) )",
			"$( (echo foo bar) )",
		},
		All: word(cmdSubst(stmt(
			subshell(litStmt("echo", "foo", "bar")),
		))),
	},
	{
		Strs: []string{"`(foo)`"},
		All: word(bckQuoted(stmt(
			subshell(litStmt("foo")),
		))),
	},
	{
		Strs: []string{
			"$(\n\t(a)\n\tb\n)",
			"$( (a); b)",
			"$((a); b)",
		},
		All: word(cmdSubst(
			stmt(subshell(litStmt("a"))),
			litStmt("b"),
		)),
	},
	{
		Strs: []string{
			"$( (a) | b)",
			"$((a) | b)",
		},
		All: word(cmdSubst(
			stmt(&ast.BinaryCmd{
				Op: token.OR,
				X:  stmt(subshell(litStmt("a"))),
				Y:  litStmt("b"),
			}),
		)),
	},
	{
		Strs: []string{
			`"$( (foo))"`,
			`"$((foo) )"`,
		},
		All: word(dblQuoted(
			cmdSubst(stmt(
				subshell(litStmt("foo")),
			)),
		)),
	},
	{
		Strs: []string{
			"\"$( (\n\tfoo\n\tbar\n))\"",
			"\"$((\n\tfoo\n\tbar\n) )\"",
		},
		All: word(dblQuoted(
			cmdSubst(stmt(
				subshell(litStmts("foo", "bar")...),
			)),
		)),
	},
	{
		Strs: []string{"`{ echo; }`"},
		All: word(bckQuoted(stmt(
			block(litStmt("echo")),
		))),
	},
	{
		Strs: []string{`{foo}`},
		All:  litWord(`{foo}`),
	},
	{
		Strs: []string{`{"foo"`},
		All:  word(lit("{"), dblQuoted(lit("foo"))),
	},
	{
		Strs: []string{`foo"bar"`, "fo\\\no\"bar\""},
		All:  word(lit("foo"), dblQuoted(lit("bar"))),
	},
	{
		Strs: []string{`!foo`},
		All:  litWord(`!foo`),
	},
	{
		Strs: []string{"$(foo bar)"},
		All:  word(cmdSubst(litStmt("foo", "bar"))),
	},
	{
		Strs: []string{"$(foo | bar)"},
		All: word(cmdSubst(
			stmt(&ast.BinaryCmd{
				Op: token.OR,
				X:  litStmt("foo"),
				Y:  litStmt("bar"),
			}),
		)),
	},
	{
		Strs: []string{"$(foo $(b1 b2))"},
		All: word(cmdSubst(
			stmt(call(
				*litWord("foo"),
				*word(cmdSubst(litStmt("b1", "b2"))),
			)),
		)),
	},
	{
		Strs: []string{`"$(foo "bar")"`},
		All: word(dblQuoted(cmdSubst(
			stmt(call(
				*litWord("foo"),
				*word(dblQuoted(lit("bar"))),
			)),
		))),
	},
	{
		Strs: []string{"`foo`"},
		All:  word(bckQuoted(litStmt("foo"))),
	},
	{
		Strs: []string{"`foo | bar`"},
		All: word(bckQuoted(
			stmt(&ast.BinaryCmd{
				Op: token.OR,
				X:  litStmt("foo"),
				Y:  litStmt("bar"),
			}),
		)),
	},
	{
		Strs: []string{"`foo 'bar'`"},
		All: word(bckQuoted(stmt(call(
			*litWord("foo"),
			*word(sglQuoted("bar")),
		)))),
	},
	{
		Strs: []string{"`foo \"bar\"`"},
		All: word(bckQuoted(
			stmt(call(
				*litWord("foo"),
				*word(dblQuoted(lit("bar"))),
			)),
		)),
	},
	{
		Strs: []string{`"$foo"`},
		All:  word(dblQuoted(litParamExp("foo"))),
	},
	{
		Strs: []string{`"#foo"`},
		All:  word(dblQuoted(lit("#foo"))),
	},
	{
		Strs: []string{`$@ $# $$ $?`},
		All: call(
			*word(litParamExp("@")),
			*word(litParamExp("#")),
			*word(litParamExp("$")),
			*word(litParamExp("?")),
		),
	},
	{
		Strs: []string{`$`, `$ #`},
		All:  litWord("$"),
	},
	{
		Strs: []string{`${@} ${$} ${?}`},
		All: call(
			*word(&ast.ParamExp{Param: *lit("@")}),
			*word(&ast.ParamExp{Param: *lit("$")}),
			*word(&ast.ParamExp{Param: *lit("?")}),
		),
	},
	{
		Strs: []string{`${foo}`},
		All:  word(&ast.ParamExp{Param: *lit("foo")}),
	},
	{
		Strs: []string{`${foo}"bar"`},
		All: word(
			&ast.ParamExp{Param: *lit("foo")},
			dblQuoted(lit("bar")),
		),
	},
	{
		Strs: []string{`${foo-bar}`},
		All: word(&ast.ParamExp{
			Param: *lit("foo"),
			Exp: &ast.Expansion{
				Op:   token.SUB,
				Word: *litWord("bar"),
			},
		}),
	},
	{
		Strs: []string{`${foo+bar}"bar"`},
		All: word(
			&ast.ParamExp{
				Param: *lit("foo"),
				Exp: &ast.Expansion{
					Op:   token.ADD,
					Word: *litWord("bar"),
				},
			},
			dblQuoted(lit("bar")),
		),
	},
	{
		Strs: []string{`${foo:=<"bar"}`},
		All: word(&ast.ParamExp{
			Param: *lit("foo"),
			Exp: &ast.Expansion{
				Op:   token.CASSIGN,
				Word: *word(lit("<"), dblQuoted(lit("bar"))),
			},
		}),
	},
	{
		Strs: []string{"${foo:=b${c}`d`}"},
		All: word(&ast.ParamExp{
			Param: *lit("foo"),
			Exp: &ast.Expansion{
				Op: token.CASSIGN,
				Word: *word(
					lit("b"),
					&ast.ParamExp{Param: *lit("c")},
					bckQuoted(litStmt("d")),
				),
			},
		}),
	},
	{
		Strs: []string{`${foo?"${bar}"}`},
		All: word(&ast.ParamExp{
			Param: *lit("foo"),
			Exp: &ast.Expansion{
				Op: token.QUEST,
				Word: *word(dblQuoted(
					&ast.ParamExp{Param: *lit("bar")},
				)),
			},
		}),
	},
	{
		Strs: []string{`${foo:?bar1 bar2}`},
		All: word(&ast.ParamExp{
			Param: *lit("foo"),
			Exp: &ast.Expansion{
				Op:   token.CQUEST,
				Word: *litWord("bar1 bar2"),
			},
		}),
	},
	{
		Strs: []string{`${a:+b}${a:-b}${a=b}`},
		All: word(
			&ast.ParamExp{
				Param: *lit("a"),
				Exp: &ast.Expansion{
					Op:   token.CADD,
					Word: *litWord("b"),
				},
			},
			&ast.ParamExp{
				Param: *lit("a"),
				Exp: &ast.Expansion{
					Op:   token.CSUB,
					Word: *litWord("b"),
				},
			},
			&ast.ParamExp{
				Param: *lit("a"),
				Exp: &ast.Expansion{
					Op:   token.ASSIGN,
					Word: *litWord("b"),
				},
			},
		),
	},
	{
		Strs: []string{`${foo%bar}${foo%%bar*}`},
		All: word(
			&ast.ParamExp{
				Param: *lit("foo"),
				Exp: &ast.Expansion{
					Op:   token.REM,
					Word: *litWord("bar"),
				},
			},
			&ast.ParamExp{
				Param: *lit("foo"),
				Exp: &ast.Expansion{
					Op:   token.DREM,
					Word: *litWord("bar*"),
				},
			},
		),
	},
	{
		Strs: []string{`${foo#bar}${foo##bar*}`},
		All: word(
			&ast.ParamExp{
				Param: *lit("foo"),
				Exp: &ast.Expansion{
					Op:   token.HASH,
					Word: *litWord("bar"),
				},
			},
			&ast.ParamExp{
				Param: *lit("foo"),
				Exp: &ast.Expansion{
					Op:   token.DHASH,
					Word: *litWord("bar*"),
				},
			},
		),
	},
	{
		Strs: []string{`${foo%?}`},
		All: word(&ast.ParamExp{
			Param: *lit("foo"),
			Exp: &ast.Expansion{
				Op:   token.REM,
				Word: *litWord("?"),
			},
		}),
	},
	{
		Strs: []string{`${foo::}`},
		All: word(&ast.ParamExp{
			Param: *lit("foo"),
			Exp: &ast.Expansion{
				Op:   token.COLON,
				Word: *litWord(":"),
			},
		}),
	},
	{
		Strs: []string{`${foo[bar]}`},
		All: word(&ast.ParamExp{
			Param: *lit("foo"),
			Ind: &ast.Index{
				Word: *litWord("bar"),
			},
		}),
	},
	{
		Strs: []string{`${foo[bar]-etc}`},
		All: word(&ast.ParamExp{
			Param: *lit("foo"),
			Ind: &ast.Index{
				Word: *litWord("bar"),
			},
			Exp: &ast.Expansion{
				Op:   token.SUB,
				Word: *litWord("etc"),
			},
		}),
	},
	{
		Strs: []string{`${foo[${bar}]}`},
		All: word(&ast.ParamExp{
			Param: *lit("foo"),
			Ind: &ast.Index{
				Word: *word(&ast.ParamExp{Param: *lit("bar")}),
			},
		}),
	},
	{
		Strs: []string{`${foo/b1/b2}`},
		All: word(&ast.ParamExp{
			Param: *lit("foo"),
			Repl: &ast.Replace{
				Orig: *litWord("b1"),
				With: *litWord("b2"),
			},
		}),
	},
	{
		Strs: []string{`${foo/a b/c d}`},
		All: word(&ast.ParamExp{
			Param: *lit("foo"),
			Repl: &ast.Replace{
				Orig: *litWord("a b"),
				With: *litWord("c d"),
			},
		}),
	},
	{
		Strs: []string{`${foo/[/]}`},
		All: word(&ast.ParamExp{
			Param: *lit("foo"),
			Repl: &ast.Replace{
				Orig: *litWord("["),
				With: *litWord("]"),
			},
		}),
	},
	{
		Strs: []string{`${foo/bar/b/a/r}`},
		All: word(&ast.ParamExp{
			Param: *lit("foo"),
			Repl: &ast.Replace{
				Orig: *litWord("bar"),
				With: *litWord("b/a/r"),
			},
		}),
	},
	{
		Strs: []string{`${foo/$a/$b}`},
		All: word(&ast.ParamExp{
			Param: *lit("foo"),
			Repl: &ast.Replace{
				Orig: *word(litParamExp("a")),
				With: *word(litParamExp("b")),
			},
		}),
	},
	{
		Strs: []string{`${foo//b1/b2}`},
		All: word(&ast.ParamExp{
			Param: *lit("foo"),
			Repl: &ast.Replace{
				All:  true,
				Orig: *litWord("b1"),
				With: *litWord("b2"),
			},
		}),
	},
	{
		Strs: []string{
			`${foo//#/}`,
			`${foo//#}`,
		},
		All: word(&ast.ParamExp{
			Param: *lit("foo"),
			Repl: &ast.Replace{
				All:  true,
				Orig: *litWord("#"),
			},
		}),
	},
	{
		Strs: []string{`${#foo}`},
		All: word(&ast.ParamExp{
			Length: true,
			Param:  *lit("foo"),
		}),
	},
	{
		Strs: []string{`${#} ${#?}`},
		All: call(
			*word(&ast.ParamExp{Length: true}),
			*word(&ast.ParamExp{Length: true, Param: *lit("?")}),
		),
	},
	{
		Strs: []string{`"${foo}"`},
		All:  word(dblQuoted(&ast.ParamExp{Param: *lit("foo")})),
	},
	{
		Strs: []string{`"(foo)"`},
		All:  word(dblQuoted(lit("(foo)"))),
	},
	{
		Strs: []string{`"${foo}>"`},
		All: word(dblQuoted(
			&ast.ParamExp{Param: *lit("foo")},
			lit(">"),
		)),
	},
	{
		Strs: []string{`"$(foo)"`},
		All:  word(dblQuoted(cmdSubst(litStmt("foo")))),
	},
	{
		Strs: []string{`"$(foo bar)"`, `"$(foo  bar)"`},
		All:  word(dblQuoted(cmdSubst(litStmt("foo", "bar")))),
	},
	{
		Strs: []string{"\"`foo`\""},
		All:  word(dblQuoted(bckQuoted(litStmt("foo")))),
	},
	{
		Strs: []string{"\"`foo bar`\"", "\"`foo  bar`\""},
		All:  word(dblQuoted(bckQuoted(litStmt("foo", "bar")))),
	},
	{
		Strs: []string{`'${foo}'`},
		All:  word(sglQuoted("${foo}")),
	},
	{
		Strs: []string{"$(())"},
		All:  word(arithmExp(nil)),
	},
	{
		Strs: []string{"$((1))"},
		All:  word(arithmExp(litWord("1"))),
	},
	{
		Strs: []string{"$((1 + 3))", "$((1+3))", "$[1+3]"},
		All: word(arithmExp(&ast.BinaryExpr{
			Op: token.ADD,
			X:  litWord("1"),
			Y:  litWord("3"),
		})),
	},
	{
		Strs: []string{`"$((foo))"`, `"$[foo]"`},
		All: word(dblQuoted(arithmExp(
			litWord("foo"),
		))),
	},
	{
		Strs: []string{`$((arr[0]++))`},
		All: word(arithmExp(
			&ast.UnaryExpr{
				Op:   token.INC,
				Post: true,
				X:    litWord("arr[0]"),
			},
		)),
	},
	{
		Strs: []string{"$((5 * 2 - 1))", "$((5*2-1))"},
		All: word(arithmExp(&ast.BinaryExpr{
			Op: token.SUB,
			X: &ast.BinaryExpr{
				Op: token.MUL,
				X:  litWord("5"),
				Y:  litWord("2"),
			},
			Y: litWord("1"),
		})),
	},
	{
		Strs: []string{"$(($i | 13))"},
		All: word(arithmExp(&ast.BinaryExpr{
			Op: token.OR,
			X:  word(litParamExp("i")),
			Y:  litWord("13"),
		})),
	},
	{
		Strs: []string{"$((3 & $((4))))"},
		All: word(arithmExp(&ast.BinaryExpr{
			Op: token.AND,
			X:  litWord("3"),
			Y:  word(arithmExp(litWord("4"))),
		})),
	},
	{
		Strs: []string{
			"$((3 % 7))",
			"$((3\n% 7))",
			"$((3\\\n % 7))",
		},
		All: word(arithmExp(&ast.BinaryExpr{
			Op: token.REM,
			X:  litWord("3"),
			Y:  litWord("7"),
		})),
	},
	{
		Strs: []string{`"$((1 / 3))"`},
		All: word(dblQuoted(arithmExp(&ast.BinaryExpr{
			Op: token.QUO,
			X:  litWord("1"),
			Y:  litWord("3"),
		}))),
	},
	{
		Strs: []string{"$((2 ** 10))"},
		All: word(arithmExp(&ast.BinaryExpr{
			Op: token.POW,
			X:  litWord("2"),
			Y:  litWord("10"),
		})),
	},
	{
		Strs: []string{`$(((1) ^ 3))`},
		All: word(arithmExp(&ast.BinaryExpr{
			Op: token.XOR,
			X:  parenExpr(litWord("1")),
			Y:  litWord("3"),
		})),
	},
	{
		Strs: []string{`$((1 >> (3 << 2)))`},
		All: word(arithmExp(&ast.BinaryExpr{
			Op: token.SHR,
			X:  litWord("1"),
			Y: parenExpr(&ast.BinaryExpr{
				Op: token.SHL,
				X:  litWord("3"),
				Y:  litWord("2"),
			}),
		})),
	},
	{
		Strs: []string{`$((-(1)))`},
		All: word(arithmExp(&ast.UnaryExpr{
			Op: token.SUB,
			X:  parenExpr(litWord("1")),
		})),
	},
	{
		Strs: []string{`$((i++))`},
		All: word(arithmExp(&ast.UnaryExpr{
			Op:   token.INC,
			Post: true,
			X:    litWord("i"),
		})),
	},
	{
		Strs: []string{`$((--i))`},
		All: word(arithmExp(&ast.UnaryExpr{
			Op: token.DEC,
			X:  litWord("i"),
		})),
	},
	{
		Strs: []string{`$((!i))`},
		All: word(arithmExp(&ast.UnaryExpr{
			Op: token.NOT,
			X:  litWord("i"),
		})),
	},
	{
		Strs: []string{`$((-!+i))`},
		All: word(arithmExp(&ast.UnaryExpr{
			Op: token.SUB,
			X: &ast.UnaryExpr{
				Op: token.NOT,
				X: &ast.UnaryExpr{
					Op: token.ADD,
					X:  litWord("i"),
				},
			},
		})),
	},
	{
		Strs: []string{`$((!!i))`},
		All: word(arithmExp(&ast.UnaryExpr{
			Op: token.NOT,
			X: &ast.UnaryExpr{
				Op: token.NOT,
				X:  litWord("i"),
			},
		})),
	},
	{
		Strs: []string{`$((1 < 3))`},
		All: word(arithmExp(&ast.BinaryExpr{
			Op: token.LSS,
			X:  litWord("1"),
			Y:  litWord("3"),
		})),
	},
	{
		Strs: []string{`$((i = 2))`, `$((i=2))`},
		All: word(arithmExp(&ast.BinaryExpr{
			Op: token.ASSIGN,
			X:  litWord("i"),
			Y:  litWord("2"),
		})),
	},
	{
		Strs: []string{"$((a += 2, b -= 3))"},
		All: word(arithmExp(&ast.BinaryExpr{
			Op: token.COMMA,
			X: &ast.BinaryExpr{
				Op: token.ADDASSGN,
				X:  litWord("a"),
				Y:  litWord("2"),
			},
			Y: &ast.BinaryExpr{
				Op: token.SUBASSGN,
				X:  litWord("b"),
				Y:  litWord("3"),
			},
		})),
	},
	{
		Strs: []string{"$((a >>= 2, b <<= 3))"},
		All: word(arithmExp(&ast.BinaryExpr{
			Op: token.COMMA,
			X: &ast.BinaryExpr{
				Op: token.SHRASSGN,
				X:  litWord("a"),
				Y:  litWord("2"),
			},
			Y: &ast.BinaryExpr{
				Op: token.SHLASSGN,
				X:  litWord("b"),
				Y:  litWord("3"),
			},
		})),
	},
	{
		Strs: []string{"$((a == b && c > d))"},
		All: word(arithmExp(&ast.BinaryExpr{
			Op: token.LAND,
			X: &ast.BinaryExpr{
				Op: token.EQL,
				X:  litWord("a"),
				Y:  litWord("b"),
			},
			Y: &ast.BinaryExpr{
				Op: token.GTR,
				X:  litWord("c"),
				Y:  litWord("d"),
			},
		})),
	},
	{
		Strs: []string{"$((a != b))"},
		All: word(arithmExp(&ast.BinaryExpr{
			Op: token.NEQ,
			X:  litWord("a"),
			Y:  litWord("b"),
		})),
	},
	{
		Strs: []string{"$((a &= b))"},
		All: word(arithmExp(&ast.BinaryExpr{
			Op: token.ANDASSGN,
			X:  litWord("a"),
			Y:  litWord("b"),
		})),
	},
	{
		Strs: []string{"$((a |= b))"},
		All: word(arithmExp(&ast.BinaryExpr{
			Op: token.ORASSGN,
			X:  litWord("a"),
			Y:  litWord("b"),
		})),
	},
	{
		Strs: []string{"$((a %= b))"},
		All: word(arithmExp(&ast.BinaryExpr{
			Op: token.REMASSGN,
			X:  litWord("a"),
			Y:  litWord("b"),
		})),
	},
	{
		Strs: []string{"$((a /= b))"},
		All: word(arithmExp(&ast.BinaryExpr{
			Op: token.QUOASSGN,
			X:  litWord("a"),
			Y:  litWord("b"),
		})),
	},
	{
		Strs: []string{"$((a ^= b))"},
		All: word(arithmExp(&ast.BinaryExpr{
			Op: token.XORASSGN,
			X:  litWord("a"),
			Y:  litWord("b"),
		})),
	},
	{
		Strs: []string{"$((i *= 3))"},
		All: word(arithmExp(&ast.BinaryExpr{
			Op: token.MULASSGN,
			X:  litWord("i"),
			Y:  litWord("3"),
		})),
	},
	{
		Strs: []string{"$((2 >= 10))"},
		All: word(arithmExp(&ast.BinaryExpr{
			Op: token.GEQ,
			X:  litWord("2"),
			Y:  litWord("10"),
		})),
	},
	{
		Strs: []string{"$((foo ? b1 : b2))"},
		All: word(arithmExp(&ast.BinaryExpr{
			Op: token.QUEST,
			X:  litWord("foo"),
			Y: &ast.BinaryExpr{
				Op: token.COLON,
				X:  litWord("b1"),
				Y:  litWord("b2"),
			},
		})),
	},
	{
		Strs: []string{`$((a <= (1 || 2)))`},
		All: word(arithmExp(&ast.BinaryExpr{
			Op: token.LEQ,
			X:  litWord("a"),
			Y: parenExpr(&ast.BinaryExpr{
				Op: token.LOR,
				X:  litWord("1"),
				Y:  litWord("2"),
			}),
		})),
	},
	{
		Strs: []string{"foo$", "foo$\n"},
		All:  word(lit("foo"), lit("$")),
	},
	{
		Strs: []string{`$'foo'`},
		All:  word(&ast.Quoted{Quote: token.DOLLSQ, Parts: lits("foo")}),
	},
	{
		Strs: []string{`$'foo${'`},
		All:  word(&ast.Quoted{Quote: token.DOLLSQ, Parts: lits("foo${")}),
	},
	{
		Strs: []string{"$'foo bar`'"},
		All:  word(&ast.Quoted{Quote: token.DOLLSQ, Parts: lits("foo bar`")}),
	},
	{
		Strs: []string{"$'f\\'oo\n'"},
		All:  word(&ast.Quoted{Quote: token.DOLLSQ, Parts: lits("f\\'oo\n")}),
	},
	{
		Strs: []string{`$"foo"`},
		All:  word(&ast.Quoted{Quote: token.DOLLDQ, Parts: lits("foo")}),
	},
	{
		Strs: []string{`$"foo$"`},
		All:  word(&ast.Quoted{Quote: token.DOLLDQ, Parts: lits("foo", "$")}),
	},
	{
		Strs: []string{`$"foo bar"`},
		All:  word(&ast.Quoted{Quote: token.DOLLDQ, Parts: lits(`foo bar`)}),
	},
	{
		Strs: []string{`$"f\"oo"`},
		All:  word(&ast.Quoted{Quote: token.DOLLDQ, Parts: lits(`f\"oo`)}),
	},
	{
		Strs: []string{`"foo$"`},
		All:  word(dblQuoted(lit("foo"), lit("$"))),
	},
	{
		Strs: []string{`"foo$$"`},
		All:  word(dblQuoted(lit("foo"), litParamExp("$"))),
	},
	{
		Strs: []string{"`foo$`"},
		All: word(bckQuoted(
			stmt(call(*word(lit("foo"), lit("$")))),
		)),
	},
	{
		Strs: []string{"foo$bar"},
		All:  word(lit("foo"), litParamExp("bar")),
	},
	{
		Strs: []string{"foo$(bar)"},
		All:  word(lit("foo"), cmdSubst(litStmt("bar"))),
	},
	{
		Strs: []string{"foo${bar}"},
		All:  word(lit("foo"), &ast.ParamExp{Param: *lit("bar")}),
	},
	{
		Strs: []string{"'foo${bar'"},
		All:  word(sglQuoted("foo${bar")),
	},
	{
		Strs: []string{"(foo)\nbar", "(foo); bar"},
		All: []ast.Command{
			subshell(litStmt("foo")),
			litCall("bar"),
		},
	},
	{
		Strs: []string{"foo\n(bar)", "foo; (bar)"},
		All: []ast.Command{
			litCall("foo"),
			subshell(litStmt("bar")),
		},
	},
	{
		Strs: []string{"foo\n(bar)", "foo; (bar)"},
		All: []ast.Command{
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
		All: &ast.CaseClause{
			Word: *word(litParamExp("i")),
			List: []*ast.PatternList{
				{
					Op:       token.DSEMICOLON,
					Patterns: litWords("1"),
					Stmts:    litStmts("foo"),
				},
				{
					Op:       token.DSEMICOLON,
					Patterns: litWords("2", "3*"),
					Stmts:    litStmts("bar"),
				},
			},
		},
	},
	{
		Strs: []string{"case $i in 1) a ;;& 2) b ;& 3) c ;; esac"},
		All: &ast.CaseClause{
			Word: *word(litParamExp("i")),
			List: []*ast.PatternList{
				{
					Op:       token.DSEMIFALL,
					Patterns: litWords("1"),
					Stmts:    litStmts("a"),
				},
				{
					Op:       token.SEMIFALL,
					Patterns: litWords("2"),
					Stmts:    litStmts("b"),
				},
				{
					Op:       token.DSEMICOLON,
					Patterns: litWords("3"),
					Stmts:    litStmts("c"),
				},
			},
		},
	},
	{
		Strs: []string{"case $i in 1) cat <<EOF ;;\nfoo\nEOF\nesac"},
		All: &ast.CaseClause{
			Word: *word(litParamExp("i")),
			List: []*ast.PatternList{{
				Op:       token.DSEMICOLON,
				Patterns: litWords("1"),
				Stmts: []*ast.Stmt{{
					Cmd: litCall("cat"),
					Redirs: []*ast.Redirect{{
						Op:   token.SHL,
						Word: *litWord("EOF"),
						Hdoc: *litWord("foo\n"),
					}},
				}},
			}},
		},
	},
	{
		Strs: []string{"foo | while read a; do b; done"},
		All: &ast.BinaryCmd{
			Op: token.OR,
			X:  litStmt("foo"),
			Y: stmt(&ast.WhileClause{
				CondStmts: []*ast.Stmt{
					litStmt("read", "a"),
				},
				DoStmts: litStmts("b"),
			}),
		},
	},
	{
		Strs: []string{"while read l; do foo || bar; done"},
		All: &ast.WhileClause{
			CondStmts: []*ast.Stmt{litStmt("read", "l")},
			DoStmts: stmts(&ast.BinaryCmd{
				Op: token.LOR,
				X:  litStmt("foo"),
				Y:  litStmt("bar"),
			}),
		},
	},
	{
		Strs: []string{"echo if while"},
		All:  litCall("echo", "if", "while"),
	},
	{
		Strs: []string{"${foo}if"},
		All:  word(&ast.ParamExp{Param: *lit("foo")}, lit("if")),
	},
	{
		Strs: []string{"$if"},
		All:  word(litParamExp("if")),
	},
	{
		Strs: []string{"if a; then b=; fi", "if a; then b=\nfi"},
		All: &ast.IfClause{
			CondStmts: litStmts("a"),
			ThenStmts: []*ast.Stmt{
				{Assigns: []*ast.Assign{
					{Name: lit("b")},
				}},
			},
		},
	},
	{
		Strs: []string{"if a; then >f; fi", "if a; then >f\nfi"},
		All: &ast.IfClause{
			CondStmts: litStmts("a"),
			ThenStmts: []*ast.Stmt{
				{Redirs: []*ast.Redirect{
					{Op: token.GTR, Word: *litWord("f")},
				}},
			},
		},
	},
	{
		Strs: []string{"if a; then (a); fi", "if a; then (a) fi"},
		All: &ast.IfClause{
			CondStmts: litStmts("a"),
			ThenStmts: stmts(subshell(litStmt("a"))),
		},
	},
	{
		Strs: []string{"a=b\nc=d", "a=b; c=d"},
		All: []*ast.Stmt{
			{Assigns: []*ast.Assign{
				{Name: lit("a"), Value: *litWord("b")},
			}},
			{Assigns: []*ast.Assign{
				{Name: lit("c"), Value: *litWord("d")},
			}},
		},
	},
	{
		Strs: []string{"foo && write | read"},
		All: &ast.BinaryCmd{
			Op: token.LAND,
			X:  litStmt("foo"),
			Y: stmt(&ast.BinaryCmd{
				Op: token.OR,
				X:  litStmt("write"),
				Y:  litStmt("read"),
			}),
		},
	},
	{
		Strs: []string{"write | read && bar"},
		All: &ast.BinaryCmd{
			Op: token.LAND,
			X: stmt(&ast.BinaryCmd{
				Op: token.OR,
				X:  litStmt("write"),
				Y:  litStmt("read"),
			}),
			Y: litStmt("bar"),
		},
	},
	{
		Strs: []string{"foo >f | bar"},
		All: &ast.BinaryCmd{
			Op: token.OR,
			X: &ast.Stmt{
				Cmd: litCall("foo"),
				Redirs: []*ast.Redirect{
					{Op: token.GTR, Word: *litWord("f")},
				},
			},
			Y: litStmt("bar"),
		},
	},
	{
		Strs: []string{"(foo) >f | bar"},
		All: &ast.BinaryCmd{
			Op: token.OR,
			X: &ast.Stmt{
				Cmd: subshell(litStmt("foo")),
				Redirs: []*ast.Redirect{
					{Op: token.GTR, Word: *litWord("f")},
				},
			},
			Y: litStmt("bar"),
		},
	},
	{
		Strs: []string{"foo | >f"},
		All: &ast.BinaryCmd{
			Op: token.OR,
			X:  litStmt("foo"),
			Y: &ast.Stmt{
				Redirs: []*ast.Redirect{
					{Op: token.GTR, Word: *litWord("f")},
				},
			},
		},
	},
	{
		Strs: []string{"[[ a ]]"},
		All: &ast.TestClause{
			X: litWord("a"),
		},
	},
	{
		Strs: []string{"[[ a ]]\nb"},
		All: stmts(
			&ast.TestClause{
				X: litWord("a"),
			},
			litCall("b"),
		),
	},
	{
		Strs: []string{"[[ a > b ]]"},
		All: &ast.TestClause{
			X: &ast.BinaryExpr{
				Op: token.GTR,
				X:  litWord("a"),
				Y:  litWord("b"),
			},
		},
	},
	{
		Strs: []string{"[[ 1 -eq 2 ]]"},
		All: &ast.TestClause{
			X: &ast.BinaryExpr{
				Op: token.TEQL,
				X:  litWord("1"),
				Y:  litWord("2"),
			},
		},
	},
	{
		Strs: []string{"[[ a =~ b ]]"},
		All: &ast.TestClause{
			X: &ast.BinaryExpr{
				Op: token.TREMATCH,
				X:  litWord("a"),
				Y:  litWord("b"),
			},
		},
	},
	{
		Strs: []string{`[[ a =~ " foo "$bar ]]`},
		All: &ast.TestClause{
			X: &ast.BinaryExpr{
				Op: token.TREMATCH,
				X:  litWord("a"),
				Y:  litWord(`" foo "$bar`),
			},
		},
	},
	{
		Strs: []string{`[[ a =~ [ab](c |d) ]]`},
		All: &ast.TestClause{
			X: &ast.BinaryExpr{
				Op: token.TREMATCH,
				X:  litWord("a"),
				Y:  litWord("[ab](c |d)"),
			},
		},
	},
	{
		Strs: []string{"[[ -n $a ]]"},
		All: &ast.TestClause{
			X: &ast.UnaryExpr{
				Op: token.TNEMPSTR,
				X:  word(litParamExp("a")),
			},
		},
	},
	{
		Strs: []string{"[[ ! $a < 'b' ]]"},
		All: &ast.TestClause{
			X: &ast.UnaryExpr{
				Op: token.NOT,
				X: &ast.BinaryExpr{
					Op: token.LSS,
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
		All: &ast.TestClause{
			X: &ast.UnaryExpr{
				Op: token.NOT,
				X: &ast.UnaryExpr{
					Op: token.TEXISTS,
					X:  word(litParamExp("a")),
				},
			},
		},
	},
	{
		Strs: []string{"[[ (a && b) ]]"},
		All: &ast.TestClause{
			X: parenExpr(&ast.BinaryExpr{
				Op: token.LAND,
				X:  litWord("a"),
				Y:  litWord("b"),
			}),
		},
	},
	{
		Strs: []string{"[[ (a && b) || c ]]"},
		All: &ast.TestClause{
			X: &ast.BinaryExpr{
				Op: token.LOR,
				X: parenExpr(&ast.BinaryExpr{
					Op: token.LAND,
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
		All: &ast.TestClause{
			X: &ast.BinaryExpr{
				Op: token.LAND,
				X: &ast.UnaryExpr{
					Op: token.TSOCKET,
					X:  litWord("a"),
				},
				Y: &ast.UnaryExpr{
					Op: token.TSMBLINK,
					X:  litWord("b"),
				},
			},
		},
	},
	{
		Strs: []string{"[[ a > b && c > d ]]"},
		All: &ast.TestClause{
			X: &ast.BinaryExpr{
				Op: token.GTR,
				X:  litWord("a"),
				Y: &ast.BinaryExpr{
					Op: token.LAND,
					X:  litWord("b"),
					Y: &ast.BinaryExpr{
						Op: token.GTR,
						X:  litWord("c"),
						Y:  litWord("d"),
					},
				},
			},
		},
	},
	{
		Strs: []string{"[[ a == b && c != d ]]"},
		All: &ast.TestClause{
			X: &ast.BinaryExpr{
				Op: token.EQL,
				X:  litWord("a"),
				Y: &ast.BinaryExpr{
					Op: token.LAND,
					X:  litWord("b"),
					Y: &ast.BinaryExpr{
						Op: token.NEQ,
						X:  litWord("c"),
						Y:  litWord("d"),
					},
				},
			},
		},
	},
	{
		Strs: []string{"declare -f func"},
		All: &ast.DeclClause{
			Opts: litWords("-f"),
			Assigns: []*ast.Assign{
				{Value: *litWord("func")},
			},
		},
	},
	{
		Strs: []string{"declare -a -bc foo=bar"},
		All: &ast.DeclClause{
			Opts: litWords("-a", "-bc"),
			Assigns: []*ast.Assign{
				{Name: lit("foo"), Value: *litWord("bar")},
			},
		},
	},
	{
		Strs: []string{"declare -a foo=(b1 `b2`)"},
		All: &ast.DeclClause{
			Opts: litWords("-a"),
			Assigns: []*ast.Assign{{
				Name: lit("foo"),
				Value: *word(
					&ast.ArrayExpr{List: []ast.Word{
						*litWord("b1"),
						*word(bckQuoted(litStmt("b2"))),
					}},
				),
			}},
		},
	},
	{
		Strs: []string{"local -a foo=(b1 `b2`)"},
		All: &ast.DeclClause{
			Local: true,
			Opts:  litWords("-a"),
			Assigns: []*ast.Assign{{
				Name: lit("foo"),
				Value: *word(
					&ast.ArrayExpr{List: []ast.Word{
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
		All: stmts(
			&ast.BinaryCmd{
				Op: token.LAND,
				X:  litStmt("a"),
				Y: &ast.Stmt{Assigns: []*ast.Assign{{
					Name: lit("b"),
					Value: *word(&ast.ArrayExpr{
						List: litWords("c"),
					}),
				}}},
			},
			litCall("d"),
		),
	},
	{
		Strs: []string{"declare -f func >/dev/null"},
		All: &ast.Stmt{
			Cmd: &ast.DeclClause{
				Opts: litWords("-f"),
				Assigns: []*ast.Assign{
					{Value: *litWord("func")},
				},
			},
			Redirs: []*ast.Redirect{
				{Op: token.GTR, Word: *litWord("/dev/null")},
			},
		},
	},
	{
		Strs: []string{"eval"},
		All:  &ast.EvalClause{},
	},
	{
		Strs: []string{"eval a=b foo"},
		All: &ast.EvalClause{Stmt: &ast.Stmt{
			Cmd: litCall("foo"),
			Assigns: []*ast.Assign{{
				Name:  lit("a"),
				Value: *litWord("b"),
			}},
		}},
	},
	{
		Strs: []string{`let i++`},
		All: letClause(
			&ast.UnaryExpr{
				Op:   token.INC,
				Post: true,
				X:    litWord("i"),
			},
		),
	},
	{
		Strs: []string{`let a++ b++ c +d`},
		All: letClause(
			&ast.UnaryExpr{
				Op:   token.INC,
				Post: true,
				X:    litWord("a"),
			},
			&ast.UnaryExpr{
				Op:   token.INC,
				Post: true,
				X:    litWord("b"),
			},
			litWord("c"),
			&ast.UnaryExpr{
				Op: token.ADD,
				X:  litWord("d"),
			},
		),
	},
	{
		Strs: []string{`let "--i"`},
		All: letClause(
			word(dblQuoted(lit("--i"))),
		),
	},
	{
		Strs: []string{`let ++i >/dev/null`},
		All: &ast.Stmt{
			Cmd: letClause(
				&ast.UnaryExpr{
					Op: token.INC,
					X:  litWord("i"),
				},
			),
			Redirs: []*ast.Redirect{
				{Op: token.GTR, Word: *litWord("/dev/null")},
			},
		},
	},
	{
		Strs: []string{
			`let a=(1 + 2) b=3+4`,
			`let a=(1+2) b=3+4`,
		},
		All: letClause(
			&ast.BinaryExpr{
				Op: token.ASSIGN,
				X:  litWord("a"),
				Y: parenExpr(&ast.BinaryExpr{
					Op: token.ADD,
					X:  litWord("1"),
					Y:  litWord("2"),
				}),
			},
			&ast.BinaryExpr{
				Op: token.ASSIGN,
				X:  litWord("b"),
				Y: &ast.BinaryExpr{
					Op: token.ADD,
					X:  litWord("3"),
					Y:  litWord("4"),
				},
			},
		),
	},
	{
		Strs: []string{"(foo-bar)"},
		All:  subshell(litStmt("foo-bar")),
	},
	{
		Strs: []string{
			"let i++\nbar",
			"let i++; bar",
		},
		All: []*ast.Stmt{
			stmt(letClause(
				&ast.UnaryExpr{
					Op:   token.INC,
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
		All: []*ast.Stmt{
			stmt(letClause(
				&ast.UnaryExpr{
					Op:   token.INC,
					Post: true,
					X:    litWord("i"),
				},
			)),
			{
				Assigns: []*ast.Assign{{
					Name: lit("foo"),
					Value: *word(
						&ast.ArrayExpr{List: litWords("bar")},
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
		All: &ast.CaseClause{
			Word: *word(lit("a")),
			List: []*ast.PatternList{{
				Op:       token.DSEMICOLON,
				Patterns: litWords("b"),
				Stmts: stmts(letClause(
					&ast.UnaryExpr{
						Op:   token.INC,
						Post: true,
						X:    litWord("i"),
					},
				)),
			}},
		},
	},
	{
		Strs: []string{"let $?"},
		All:  letClause(word(litParamExp("?"))),
	},
	{
		Strs: []string{"a=(b c) foo"},
		All: &ast.Stmt{
			Assigns: []*ast.Assign{{
				Name: lit("a"),
				Value: *word(
					&ast.ArrayExpr{List: litWords("b", "c")},
				),
			}},
			Cmd: litCall("foo"),
		},
	},
	{
		Strs: []string{"a=(b c) foo", "a=(\nb\nc\n) foo"},
		All: &ast.Stmt{
			Assigns: []*ast.Assign{{
				Name: lit("a"),
				Value: *word(
					&ast.ArrayExpr{List: litWords("b", "c")},
				),
			}},
			Cmd: litCall("foo"),
		},
	},
	{
		Strs: []string{"a+=1 b+=(2 3)"},
		All: &ast.Stmt{
			Assigns: []*ast.Assign{
				{
					Append: true,
					Name:   lit("a"),
					Value:  *litWord("1"),
				},
				{
					Append: true,
					Name:   lit("b"),
					Value: *word(
						&ast.ArrayExpr{List: litWords("2", "3")},
					),
				},
			},
		},
	},
	{
		Strs: []string{"<<EOF | b\nfoo\nEOF", "<<EOF|b;\nfoo\n"},
		All: &ast.BinaryCmd{
			Op: token.OR,
			X: &ast.Stmt{Redirs: []*ast.Redirect{{
				Op:   token.SHL,
				Word: *litWord("EOF"),
				Hdoc: *litWord("foo\n"),
			}}},
			Y: litStmt("b"),
		},
	},
	{
		Strs: []string{"<<EOF1 <<EOF2 | c && d\nEOF1\nEOF2"},
		All: &ast.BinaryCmd{
			Op: token.LAND,
			X: stmt(&ast.BinaryCmd{
				Op: token.OR,
				X: &ast.Stmt{Redirs: []*ast.Redirect{
					{
						Op:   token.SHL,
						Word: *litWord("EOF1"),
						Hdoc: *word(),
					},
					{
						Op:   token.SHL,
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
		All: &ast.BinaryCmd{
			Op: token.LAND,
			X: &ast.Stmt{Redirs: []*ast.Redirect{{
				Op:   token.SHL,
				Word: *litWord("EOF"),
				Hdoc: *litWord("hdoc\n"),
			}}},
			Y: stmt(block(litStmt("bar"))),
		},
	},
	{
		Strs: []string{"foo() {\n\t<<EOF && { bar; }\nhdoc\nEOF\n}"},
		All: &ast.FuncDecl{
			Name: *lit("foo"),
			Body: stmt(block(stmt(&ast.BinaryCmd{
				Op: token.LAND,
				X: &ast.Stmt{Redirs: []*ast.Redirect{{
					Op:   token.SHL,
					Word: *litWord("EOF"),
					Hdoc: *litWord("hdoc\n"),
				}}},
				Y: stmt(block(litStmt("bar"))),
			}))),
		},
	},
	{
		Strs: []string{"\"a`\"\"`\""},
		All: word(dblQuoted(
			lit("a"),
			bckQuoted(
				stmt(call(
					*word(dblQuoted()),
				)),
			),
		)),
	},
}

func fullProg(v interface{}) *ast.File {
	switch x := v.(type) {
	case *ast.File:
		return x
	case []*ast.Stmt:
		return &ast.File{Stmts: x}
	case *ast.Stmt:
		return &ast.File{Stmts: []*ast.Stmt{x}}
	case []ast.Command:
		f := &ast.File{}
		for _, cmd := range x {
			f.Stmts = append(f.Stmts, stmt(cmd))
		}
		return f
	case *ast.Word:
		return fullProg(call(*x))
	case ast.Command:
		return fullProg(stmt(x))
	}
	return nil
}

func SetPosRecurse(tb testing.TB, src string, v interface{}, to token.Pos, diff bool) {
	checkSrc := func(pos token.Pos, strs []string) {
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
			got = strings.Replace(got, "\\\n", "", -1)[:len(want)]
			if got == want {
				return
			}
		}
		tb.Fatalf("Expected one of %q at %d in %q, found %q",
			strs, offs, src, gotErr)
	}
	setPos := func(p *token.Pos, strs ...string) {
		checkSrc(*p, strs)
		if diff && *p == to {
			tb.Fatalf("Pos() in %T is already %v", v, to)
		}
		*p = to
	}
	checkPos := func(n ast.Node) {
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
		if n, ok := v.(ast.Node); ok {
			checkPos(n)
		}
	}
	switch x := v.(type) {
	case *ast.File:
		recurse(x.Stmts)
		checkPos(x)
	case []*ast.Stmt:
		for _, s := range x {
			recurse(s)
		}
	case *ast.Stmt:
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
	case []*ast.Assign:
		for _, a := range x {
			if a.Name != nil {
				recurse(a.Name)
			}
			recurse(&a.Value)
			checkPos(a)
		}
	case *ast.CallExpr:
		recurse(x.Args)
	case []ast.Word:
		for i := range x {
			recurse(&x[i])
		}
	case *ast.Word:
		recurse(x.Parts)
	case []ast.WordPart:
		for i := range x {
			recurse(&x[i])
		}
	case *ast.WordPart:
		recurse(*x)
	case *ast.Loop:
		recurse(*x)
	case *ast.ArithmExpr:
		recurse(*x)
	case *ast.Lit:
		setPos(&x.ValuePos, x.Value)
	case *ast.Subshell:
		setPos(&x.Lparen, "(")
		setPos(&x.Rparen, ")")
		recurse(x.Stmts)
	case *ast.Block:
		setPos(&x.Lbrace, "{")
		setPos(&x.Rbrace, "}")
		recurse(x.Stmts)
	case *ast.IfClause:
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
	case *ast.WhileClause:
		setPos(&x.While, "while")
		setPos(&x.Do, "do")
		setPos(&x.Done, "done")
		recurse(x.CondStmts)
		recurse(x.DoStmts)
	case *ast.UntilClause:
		setPos(&x.Until, "until")
		setPos(&x.Do, "do")
		setPos(&x.Done, "done")
		recurse(x.CondStmts)
		recurse(x.DoStmts)
	case *ast.ForClause:
		setPos(&x.For, "for")
		setPos(&x.Do, "do")
		setPos(&x.Done, "done")
		recurse(&x.Loop)
		recurse(x.DoStmts)
	case *ast.WordIter:
		recurse(&x.Name)
		recurse(x.List)
	case *ast.CStyleLoop:
		setPos(&x.Lparen, "((")
		setPos(&x.Rparen, "))")
		recurse(&x.Init)
		recurse(&x.Cond)
		recurse(&x.Post)
	case *ast.SglQuoted:
		setPos(&x.Quote, "'")
	case *ast.Quoted:
		setPos(&x.QuotePos, x.Quote.String())
		recurse(x.Parts)
	case *ast.UnaryExpr:
		strs := []string{x.Op.String()}
		switch x.Op {
		case token.TEXISTS:
			strs = append(strs, "-a")
		case token.TSMBLINK:
			strs = append(strs, "-h")
		}
		setPos(&x.OpPos, strs...)
		recurse(&x.X)
	case *ast.BinaryCmd:
		setPos(&x.OpPos, x.Op.String())
		recurse(x.X)
		recurse(x.Y)
	case *ast.BinaryExpr:
		setPos(&x.OpPos, x.Op.String())
		recurse(&x.X)
		recurse(&x.Y)
	case *ast.FuncDecl:
		if x.BashStyle {
			setPos(&x.Position, "function")
		} else {
			setPos(&x.Position)
		}
		recurse(&x.Name)
		recurse(x.Body)
	case *ast.ParamExp:
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
	case *ast.ArithmExp:
		if src != "" && src[x.Left] == '[' {
			// deprecated $(( form
			setPos(&x.Left, "$[")
			setPos(&x.Right, "]")
		} else {
			setPos(&x.Left, x.Token.String())
			setPos(&x.Right, "))")
		}
		recurse(&x.X)
	case *ast.ParenExpr:
		setPos(&x.Lparen, "(")
		setPos(&x.Rparen, ")")
		recurse(&x.X)
	case *ast.CmdSubst:
		if x.Backquotes {
			setPos(&x.Left, "`")
			setPos(&x.Right, "`")
		} else {
			setPos(&x.Left, "$(")
			setPos(&x.Right, ")")
		}
		recurse(x.Stmts)
	case *ast.CaseClause:
		setPos(&x.Case, "case")
		setPos(&x.Esac, "esac")
		recurse(&x.Word)
		for _, pl := range x.List {
			setPos(&pl.OpPos)
			recurse(pl.Patterns)
			recurse(pl.Stmts)
		}
	case *ast.TestClause:
		setPos(&x.Left, "[[")
		setPos(&x.Right, "]]")
		recurse(x.X)
	case *ast.DeclClause:
		if x.Local {
			setPos(&x.Declare, "local")
		} else {
			setPos(&x.Declare, "declare")
		}
		recurse(x.Opts)
		recurse(x.Assigns)
	case *ast.EvalClause:
		setPos(&x.Eval, "eval")
		if x.Stmt != nil {
			recurse(x.Stmt)
		}
	case *ast.LetClause:
		setPos(&x.Let, "let")
		for i := range x.Exprs {
			recurse(&x.Exprs[i])
		}
	case *ast.ArrayExpr:
		setPos(&x.Lparen, "(")
		setPos(&x.Rparen, ")")
		recurse(x.List)
	case *ast.ProcSubst:
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
