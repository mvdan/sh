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
		FileTests[i].Ast = fullProg(FileTests[i].Ast)
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
	Ast  interface{}
}

var FileTests = []TestCase{
	{
		[]string{"", " ", "\t", "\n \n", "\r \r\n"},
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
		[]string{`\`},
		litWord(`\`),
	},
	{
		[]string{`foo\`, "f\\\noo\\"},
		litWord(`foo\`),
	},
	{
		[]string{`foo\a`, "f\\\noo\\a"},
		litWord(`foo\a`),
	},
	{
		[]string{
			"foo\nbar",
			"foo; bar;",
			"foo;bar;",
			"\nfoo\nbar\n",
			"foo\r\nbar\r\n",
		},
		litStmts("foo", "bar"),
	},
	{
		[]string{"foo a b", " foo  a  b ", "foo \\\n a b"},
		litCall("foo", "a", "b"),
	},
	{
		[]string{"foobar", "foo\\\nbar", "foo\\\nba\\\nr"},
		litWord("foobar"),
	},
	{
		[]string{"foo", "foo \\\n"},
		litWord("foo"),
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
		[]string{"{ foo; }", "{\nfoo\n}"},
		block(litStmt("foo")),
	},
	{
		[]string{
			"if a; then b; fi",
			"if a\nthen\nb\nfi",
			"if a \nthen\nb\nfi",
		},
		&ast.IfClause{
			Cond:      &ast.StmtCond{Stmts: litStmts("a")},
			ThenStmts: litStmts("b"),
		},
	},
	{
		[]string{
			"if a; then b; else c; fi",
			"if a\nthen b\nelse\nc\nfi",
		},
		&ast.IfClause{
			Cond:      &ast.StmtCond{Stmts: litStmts("a")},
			ThenStmts: litStmts("b"),
			ElseStmts: litStmts("c"),
		},
	},
	{
		[]string{
			"if a; then a; elif b; then b; elif c; then c; else d; fi",
			"if a\nthen a\nelif b\nthen b\nelif c\nthen c\nelse\nd\nfi",
		},
		&ast.IfClause{
			Cond:      &ast.StmtCond{Stmts: litStmts("a")},
			ThenStmts: litStmts("a"),
			Elifs: []*ast.Elif{
				{
					Cond:      &ast.StmtCond{Stmts: litStmts("b")},
					ThenStmts: litStmts("b"),
				},
				{
					Cond:      &ast.StmtCond{Stmts: litStmts("c")},
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
		&ast.IfClause{
			Cond: &ast.StmtCond{Stmts: []*ast.Stmt{
				litStmt("a1"),
				litStmt("a2", "foo"),
				litStmt("a3", "bar"),
			}},
			ThenStmts: litStmts("b"),
		},
	},
	{
		[]string{"if ((1 > 2)); then b; fi"},
		&ast.IfClause{
			Cond: &ast.CStyleCond{X: &ast.BinaryExpr{
				Op: token.GTR,
				X:  litWord("1"),
				Y:  litWord("2"),
			}},
			ThenStmts: litStmts("b"),
		},
	},
	{
		[]string{
			"while a; do b; done",
			"wh\\\nile a; do b; done",
			"while a\ndo\nb\ndone",
		},
		&ast.WhileClause{
			Cond:    &ast.StmtCond{Stmts: litStmts("a")},
			DoStmts: litStmts("b"),
		},
	},
	{
		[]string{"while { a; }; do b; done", "while { a; } do b; done"},
		&ast.WhileClause{
			Cond: &ast.StmtCond{Stmts: []*ast.Stmt{
				stmt(block(litStmt("a"))),
			}},
			DoStmts: litStmts("b"),
		},
	},
	{
		[]string{"while (a); do b; done", "while (a) do b; done"},
		&ast.WhileClause{
			Cond: &ast.StmtCond{Stmts: []*ast.Stmt{
				stmt(subshell(litStmt("a"))),
			}},
			DoStmts: litStmts("b"),
		},
	},
	{
		[]string{"while ((1 > 2)); do b; done"},
		&ast.WhileClause{
			Cond: &ast.CStyleCond{X: &ast.BinaryExpr{
				Op: token.GTR,
				X:  litWord("1"),
				Y:  litWord("2"),
			}},
			DoStmts: litStmts("b"),
		},
	},
	{
		[]string{"until a; do b; done", "until a\ndo\nb\ndone"},
		&ast.UntilClause{
			Cond:    &ast.StmtCond{Stmts: litStmts("a")},
			DoStmts: litStmts("b"),
		},
	},
	{
		[]string{
			"for i; do foo; done",
			"for i in; do foo; done",
		},
		&ast.ForClause{
			Loop: &ast.WordIter{
				Name: *lit("i"),
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
		&ast.ForClause{
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
		[]string{
			"for ((i = 0; i < 10; i++)); do echo $i; done",
			"for ((i=0;i<10;i++)) do echo $i; done",
			"for (( i = 0 ; i < 10 ; i++ ))\ndo echo $i\ndone",
		},
		&ast.ForClause{
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
		[]string{`' ' "foo bar"`},
		call(
			*word(sglQuoted(" ")),
			*word(dblQuoted(lits("foo bar")...)),
		),
	},
	{
		[]string{`"foo \" bar"`},
		word(dblQuoted(lits(`foo \" bar`)...)),
	},
	{
		[]string{"\">foo\" \"\nbar\""},
		call(
			*word(dblQuoted(lit(">foo"))),
			*word(dblQuoted(lit("\nbar"))),
		),
	},
	{
		[]string{`foo \" bar`},
		litCall(`foo`, `\"`, `bar`),
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
		[]string{`""`},
		word(dblQuoted()),
	},
	{
		[]string{"=a s{s s=s"},
		litCall("=a", "s{s", "s=s"),
	},
	{
		[]string{"foo && bar", "foo&&bar", "foo &&\nbar"},
		&ast.BinaryCmd{
			Op: token.LAND,
			X:  litStmt("foo"),
			Y:  litStmt("bar"),
		},
	},
	{
		[]string{"foo \\\n\t&& bar"},
		&ast.BinaryCmd{
			Op: token.LAND,
			X:  litStmt("foo"),
			Y:  litStmt("bar"),
		},
	},
	{
		[]string{"foo || bar", "foo||bar", "foo ||\nbar"},
		&ast.BinaryCmd{
			Op: token.LOR,
			X:  litStmt("foo"),
			Y:  litStmt("bar"),
		},
	},
	{
		[]string{"if a; then b; fi || while a; do b; done"},
		&ast.BinaryCmd{
			Op: token.LOR,
			X: stmt(&ast.IfClause{
				Cond:      &ast.StmtCond{Stmts: litStmts("a")},
				ThenStmts: litStmts("b"),
			}),
			Y: stmt(&ast.WhileClause{
				Cond:    &ast.StmtCond{Stmts: litStmts("a")},
				DoStmts: litStmts("b"),
			}),
		},
	},
	{
		[]string{"foo && bar1 || bar2"},
		&ast.BinaryCmd{
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
		[]string{"foo | bar", "foo|bar", "foo |\n#etc\nbar"},
		&ast.BinaryCmd{
			Op: token.OR,
			X:  litStmt("foo"),
			Y:  litStmt("bar"),
		},
	},
	{
		[]string{"foo | bar | extra"},
		&ast.BinaryCmd{
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
		[]string{"foo |& bar", "foo|&bar"},
		&ast.BinaryCmd{
			Op: token.PIPEALL,
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
		&ast.FuncDecl{
			Name: *lit("foo"),
			Body: stmt(block(litStmts("a", "b")...)),
		},
	},
	{
		[]string{"foo() { a; }\nbar", "foo() {\na\n}; bar"},
		[]ast.Command{
			&ast.FuncDecl{
				Name: *lit("foo"),
				Body: stmt(block(litStmts("a")...)),
			},
			litCall("bar"),
		},
	},
	{
		[]string{"-foo_.,+-bar() { a; }"},
		&ast.FuncDecl{
			Name: *lit("-foo_.,+-bar"),
			Body: stmt(block(litStmts("a")...)),
		},
	},
	{
		[]string{
			"function foo() {\n\ta\n\tb\n}",
			"function foo {\n\ta\n\tb\n}",
			"function foo() { a; b; }",
		},
		&ast.FuncDecl{
			BashStyle: true,
			Name:      *lit("foo"),
			Body:      stmt(block(litStmts("a", "b")...)),
		},
	},
	{
		[]string{"function foo() (a)"},
		&ast.FuncDecl{
			BashStyle: true,
			Name:      *lit("foo"),
			Body:      stmt(subshell(litStmt("a"))),
		},
	},
	{
		[]string{"a=b foo=$bar foo=start$bar"},
		&ast.Stmt{
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
		[]string{"a=\"\nbar\""},
		&ast.Stmt{
			Assigns: []*ast.Assign{{
				Name:  lit("a"),
				Value: *word(dblQuoted(lit("\nbar"))),
			}},
		},
	},
	{
		[]string{"a= foo"},
		&ast.Stmt{
			Cmd:     litCall("foo"),
			Assigns: []*ast.Assign{{Name: lit("a")}},
		},
	},
	{
		[]string{"à=b foo"},
		litStmt("à=b", "foo"),
	},
	{
		[]string{
			"foo >a >>b <c",
			"foo > a >> b < c",
			">a >>b <c foo",
		},
		&ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{
				{Op: token.GTR, Word: *litWord("a")},
				{Op: token.SHR, Word: *litWord("b")},
				{Op: token.LSS, Word: *litWord("c")},
			},
		},
	},
	{
		[]string{
			"foo bar >a",
			"foo >a bar",
		},
		&ast.Stmt{
			Cmd: litCall("foo", "bar"),
			Redirs: []*ast.Redirect{
				{Op: token.GTR, Word: *litWord("a")},
			},
		},
	},
	{
		[]string{`>a >\b`},
		&ast.Stmt{
			Redirs: []*ast.Redirect{
				{Op: token.GTR, Word: *litWord("a")},
				{Op: token.GTR, Word: *litWord(`\b`)},
			},
		},
	},
	{
		[]string{">a\n>b", ">a; >b"},
		[]*ast.Stmt{
			{Redirs: []*ast.Redirect{
				{Op: token.GTR, Word: *litWord("a")},
			}},
			{Redirs: []*ast.Redirect{
				{Op: token.GTR, Word: *litWord("b")},
			}},
		},
	},
	{
		[]string{"foo1\nfoo2 >r2", "foo1; >r2 foo2"},
		[]*ast.Stmt{
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
		[]string{"foo >bar`etc`"},
		&ast.Stmt{
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
		[]string{
			"foo <<EOF\nbar\nEOF",
			"foo <<EOF\nbar\n",
		},
		&ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{{
				Op:   token.SHL,
				Word: *litWord("EOF"),
				Hdoc: *litWord("bar\n"),
			}},
		},
	},
	{
		[]string{"foo <<EOF\n1\n2\n3\nEOF"},
		&ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{{
				Op:   token.SHL,
				Word: *litWord("EOF"),
				Hdoc: *litWord("1\n2\n3\n"),
			}},
		},
	},
	{
		[]string{"a <<EOF\nfoo$bar\nEOF"},
		&ast.Stmt{
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
		[]string{"a <<EOF\n\"$bar\"\nEOF"},
		&ast.Stmt{
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
		[]string{"a <<EOF\n$''$bar\nEOF"},
		&ast.Stmt{
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
		[]string{"a <<EOF\n`b`\nc\nEOF"},
		&ast.Stmt{
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
		[]string{"a <<EOF\n\\${\nEOF"},
		&ast.Stmt{
			Cmd: litCall("a"),
			Redirs: []*ast.Redirect{{
				Op:   token.SHL,
				Word: *litWord("EOF"),
				Hdoc: *litWord("\\${\n"),
			}},
		},
	},
	{
		[]string{"{ foo <<EOF\nbar\nEOF\n}"},
		block(&ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{{
				Op:   token.SHL,
				Word: *litWord("EOF"),
				Hdoc: *litWord("bar\n"),
			}},
		}),
	},
	{
		[]string{"$(foo <<EOF\nbar\nEOF\n)"},
		word(cmdSubst(&ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{{
				Op:   token.SHL,
				Word: *litWord("EOF"),
				Hdoc: *litWord("bar\n"),
			}},
		})),
	},
	{
		[]string{"foo >f <<EOF\nbar\nEOF"},
		&ast.Stmt{
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
		[]string{"foo <<EOF >f\nbar\nEOF"},
		&ast.Stmt{
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
		[]string{"if true; then foo <<-EOF\n\tbar\n\tEOF\nfi"},
		&ast.IfClause{
			Cond: &ast.StmtCond{Stmts: litStmts("true")},
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
		[]string{"if true; then foo <<-EOF\n\tEOF\nfi"},
		&ast.IfClause{
			Cond: &ast.StmtCond{Stmts: litStmts("true")},
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
		[]string{"foo <<EOF\nbar\nEOF\nfoo2"},
		[]*ast.Stmt{
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
		[]string{"foo <<FOOBAR\nbar\nFOOBAR"},
		&ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{{
				Op:   token.SHL,
				Word: *litWord("FOOBAR"),
				Hdoc: *litWord("bar\n"),
			}},
		},
	},
	{
		[]string{"foo <<\"EOF\"\nbar\nEOF"},
		&ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{{
				Op:   token.SHL,
				Word: *word(dblQuoted(lit("EOF"))),
				Hdoc: *litWord("bar\n"),
			}},
		},
	},
	{
		[]string{"foo <<'EOF'\nbar\nEOF"},
		&ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{
				{
					Op:   token.SHL,
					Word: *word(sglQuoted("EOF")),
					Hdoc: *litWord("bar\n"),
				},
			},
		},
	},
	{
		[]string{"foo <<\"EOF\"2\nbar\nEOF2"},
		&ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{{
				Op:   token.SHL,
				Word: *word(dblQuoted(lit("EOF")), lit("2")),
				Hdoc: *litWord("bar\n"),
			}},
		},
	},
	{
		[]string{"foo <<\\EOF\nbar\nEOF"},
		&ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{{
				Op:   token.SHL,
				Word: *litWord("\\EOF"),
				Hdoc: *litWord("bar\n"),
			}},
		},
	},
	{
		[]string{"foo <<$EOF\nbar\n$EOF"},
		&ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{{
				Op:   token.SHL,
				Word: *word(litParamExp("EOF")),
				Hdoc: *litWord("bar\n"),
			}},
		},
	},
	{
		[]string{
			"foo <<-EOF\nbar\nEOF",
			"foo <<- EOF\nbar\nEOF",
		},
		&ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{{
				Op:   token.DHEREDOC,
				Word: *litWord("EOF"),
				Hdoc: *litWord("bar\n"),
			}},
		},
	},
	{
		[]string{
			"foo <<-EOF\n\tEOF",
			"foo <<-EOF\n\t",
		},
		&ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{{
				Op:   token.DHEREDOC,
				Word: *litWord("EOF"),
				Hdoc: *litWord("\t"),
			}},
		},
	},
	{
		[]string{
			"foo <<-EOF\n\tbar\n\tEOF",
			"foo <<-EOF\n\tbar\n\t",
		},
		&ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{{
				Op:   token.DHEREDOC,
				Word: *litWord("EOF"),
				Hdoc: *litWord("\tbar\n\t"),
			}},
		},
	},
	{
		[]string{
			"f1 <<EOF1\nh1\nEOF1\nf2 <<EOF2\nh2\nEOF2",
			"f1 <<EOF1; f2 <<EOF2\nh1\nEOF1\nh2\nEOF2",
		},
		[]*ast.Stmt{
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
		[]string{
			"a <<EOF\nfoo\nEOF\nb\nb\nb\nb\nb\nb\nb\nb\nb",
			"a <<EOF;b;b;b;b;b;b;b;b;b\nfoo\nEOF",
		},
		[]*ast.Stmt{
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
		[]string{
			"foo \"\narg\" <<EOF\nbar\nEOF",
			"foo <<EOF \"\narg\"\nbar\nEOF",
		},
		&ast.Stmt{
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
		[]string{"foo >&2 <&0 2>file <>f2 &>f3 &>>f4"},
		&ast.Stmt{
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
		[]string{"foo 2>file bar", "2>file foo bar"},
		&ast.Stmt{
			Cmd: litCall("foo", "bar"),
			Redirs: []*ast.Redirect{
				{Op: token.GTR, N: lit("2"), Word: *litWord("file")},
			},
		},
	},
	{
		[]string{"a >f1\nb >f2", "a >f1; b >f2"},
		[]*ast.Stmt{
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
		[]string{
			"foo <<<input",
			"foo <<< input",
		},
		&ast.Stmt{
			Cmd: litCall("foo"),
			Redirs: []*ast.Redirect{
				{Op: token.WHEREDOC, Word: *litWord("input")},
			},
		},
	},
	{
		[]string{
			`foo <<<"spaced input"`,
			`foo <<< "spaced input"`,
		},
		&ast.Stmt{
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
		[]string{"foo >(foo)"},
		&ast.Stmt{
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
		[]string{"foo < <(foo)"},
		&ast.Stmt{
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
		[]string{"!"},
		&ast.Stmt{Negated: true},
	},
	{
		[]string{"! foo"},
		&ast.Stmt{
			Negated: true,
			Cmd:     litCall("foo"),
		},
	},
	{
		[]string{"foo &\nbar", "foo &; bar", "foo & bar", "foo&bar"},
		[]*ast.Stmt{
			{
				Cmd:        litCall("foo"),
				Background: true,
			},
			litStmt("bar"),
		},
	},
	{
		[]string{"! if foo; then bar; fi >/dev/null &"},
		&ast.Stmt{
			Negated: true,
			Cmd: &ast.IfClause{
				Cond:      &ast.StmtCond{Stmts: litStmts("foo")},
				ThenStmts: litStmts("bar"),
			},
			Redirs: []*ast.Redirect{
				{Op: token.GTR, Word: *litWord("/dev/null")},
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
		[]string{"$({ echo; })"},
		word(cmdSubst(stmt(
			block(litStmt("echo")),
		))),
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
		[]string{`foo"bar"`, "fo\\\no\"bar\""},
		word(lit("foo"), dblQuoted(lit("bar"))),
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
			stmt(&ast.BinaryCmd{
				Op: token.OR,
				X:  litStmt("foo"),
				Y:  litStmt("bar"),
			}),
		)),
	},
	{
		[]string{"$(foo $(b1 b2))"},
		word(cmdSubst(
			stmt(call(
				*litWord("foo"),
				*word(cmdSubst(litStmt("b1", "b2"))),
			)),
		)),
	},
	{
		[]string{`"$(foo "bar")"`},
		word(dblQuoted(cmdSubst(
			stmt(call(
				*litWord("foo"),
				*word(dblQuoted(lit("bar"))),
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
			stmt(&ast.BinaryCmd{
				Op: token.OR,
				X:  litStmt("foo"),
				Y:  litStmt("bar"),
			}),
		)),
	},
	{
		[]string{"`foo 'bar'`"},
		word(bckQuoted(stmt(call(
			*litWord("foo"),
			*word(sglQuoted("bar")),
		)))),
	},
	{
		[]string{"`foo \"bar\"`"},
		word(bckQuoted(
			stmt(call(
				*litWord("foo"),
				*word(dblQuoted(lit("bar"))),
			)),
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
		call(
			*word(litParamExp("@")),
			*word(litParamExp("#")),
			*word(litParamExp("$")),
			*word(litParamExp("?")),
		),
	},
	{
		[]string{`$`, `$ #`},
		litWord("$"),
	},
	{
		[]string{`${@} ${$} ${?}`},
		call(
			*word(&ast.ParamExp{Param: *lit("@")}),
			*word(&ast.ParamExp{Param: *lit("$")}),
			*word(&ast.ParamExp{Param: *lit("?")}),
		),
	},
	{
		[]string{`${foo}`},
		word(&ast.ParamExp{Param: *lit("foo")}),
	},
	{
		[]string{`${foo}"bar"`},
		word(
			&ast.ParamExp{Param: *lit("foo")},
			dblQuoted(lit("bar")),
		),
	},
	{
		[]string{`${foo-bar}`},
		word(&ast.ParamExp{
			Param: *lit("foo"),
			Exp: &ast.Expansion{
				Op:   token.SUB,
				Word: *litWord("bar"),
			},
		}),
	},
	{
		[]string{`${foo+bar}"bar"`},
		word(
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
		[]string{`${foo:=<"bar"}`},
		word(&ast.ParamExp{
			Param: *lit("foo"),
			Exp: &ast.Expansion{
				Op:   token.CASSIGN,
				Word: *word(lit("<"), dblQuoted(lit("bar"))),
			},
		}),
	},
	{
		[]string{"${foo:=b${c}`d`}"},
		word(&ast.ParamExp{
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
		[]string{`${foo?"${bar}"}`},
		word(&ast.ParamExp{
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
		[]string{`${foo:?bar1 bar2}`},
		word(&ast.ParamExp{
			Param: *lit("foo"),
			Exp: &ast.Expansion{
				Op:   token.CQUEST,
				Word: *litWord("bar1 bar2"),
			},
		}),
	},
	{
		[]string{`${a:+b}${a:-b}${a=b}`},
		word(
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
		[]string{`${foo%bar}${foo%%bar*}`},
		word(
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
		[]string{`${foo#bar}${foo##bar*}`},
		word(
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
		[]string{`${foo%?}`},
		word(&ast.ParamExp{
			Param: *lit("foo"),
			Exp: &ast.Expansion{
				Op:   token.REM,
				Word: *litWord("?"),
			},
		}),
	},
	{
		[]string{`${foo::}`},
		word(&ast.ParamExp{
			Param: *lit("foo"),
			Exp: &ast.Expansion{
				Op:   token.COLON,
				Word: *litWord(":"),
			},
		}),
	},
	{
		[]string{`${foo[bar]}`},
		word(&ast.ParamExp{
			Param: *lit("foo"),
			Ind: &ast.Index{
				Word: *litWord("bar"),
			},
		}),
	},
	{
		[]string{`${foo[bar]-etc}`},
		word(&ast.ParamExp{
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
		[]string{`${foo[${bar}]}`},
		word(&ast.ParamExp{
			Param: *lit("foo"),
			Ind: &ast.Index{
				Word: *word(&ast.ParamExp{Param: *lit("bar")}),
			},
		}),
	},
	{
		[]string{`${foo/b1/b2}`},
		word(&ast.ParamExp{
			Param: *lit("foo"),
			Repl: &ast.Replace{
				Orig: *litWord("b1"),
				With: *litWord("b2"),
			},
		}),
	},
	{
		[]string{`${foo/a b/c d}`},
		word(&ast.ParamExp{
			Param: *lit("foo"),
			Repl: &ast.Replace{
				Orig: *litWord("a b"),
				With: *litWord("c d"),
			},
		}),
	},
	{
		[]string{`${foo/[/]}`},
		word(&ast.ParamExp{
			Param: *lit("foo"),
			Repl: &ast.Replace{
				Orig: *litWord("["),
				With: *litWord("]"),
			},
		}),
	},
	{
		[]string{`${foo/bar/b/a/r}`},
		word(&ast.ParamExp{
			Param: *lit("foo"),
			Repl: &ast.Replace{
				Orig: *litWord("bar"),
				With: *litWord("b/a/r"),
			},
		}),
	},
	{
		[]string{`${foo/$a/$b}`},
		word(&ast.ParamExp{
			Param: *lit("foo"),
			Repl: &ast.Replace{
				Orig: *word(litParamExp("a")),
				With: *word(litParamExp("b")),
			},
		}),
	},
	{
		[]string{`${foo//b1/b2}`},
		word(&ast.ParamExp{
			Param: *lit("foo"),
			Repl: &ast.Replace{
				All:  true,
				Orig: *litWord("b1"),
				With: *litWord("b2"),
			},
		}),
	},
	{
		[]string{
			`${foo//#/}`,
			`${foo//#}`,
		},
		word(&ast.ParamExp{
			Param: *lit("foo"),
			Repl: &ast.Replace{
				All:  true,
				Orig: *litWord("#"),
			},
		}),
	},
	{
		[]string{`${#foo}`},
		word(&ast.ParamExp{
			Length: true,
			Param:  *lit("foo"),
		}),
	},
	{
		[]string{`${#} ${#?}`},
		call(
			*word(&ast.ParamExp{Length: true}),
			*word(&ast.ParamExp{Length: true, Param: *lit("?")}),
		),
	},
	{
		[]string{`"${foo}"`},
		word(dblQuoted(&ast.ParamExp{Param: *lit("foo")})),
	},
	{
		[]string{`"(foo)"`},
		word(dblQuoted(lit("(foo)"))),
	},
	{
		[]string{`"${foo}>"`},
		word(dblQuoted(
			&ast.ParamExp{Param: *lit("foo")},
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
		word(arithmExp(nil)),
	},
	{
		[]string{"$((1))"},
		word(arithmExp(litWord("1"))),
	},
	{
		[]string{"$((1 + 3))", "$((1+3))", "$[1+3]"},
		word(arithmExp(&ast.BinaryExpr{
			Op: token.ADD,
			X:  litWord("1"),
			Y:  litWord("3"),
		})),
	},
	{
		[]string{`"$((foo))"`, `"$[foo]"`},
		word(dblQuoted(arithmExp(
			litWord("foo"),
		))),
	},
	{
		[]string{`$((arr[0]++))`},
		word(arithmExp(
			&ast.UnaryExpr{
				Op: token.INC,
				Post: true,
				X:  litWord("arr[0]"),
			},
		)),
	},
	{
		[]string{"$((5 * 2 - 1))", "$((5*2-1))"},
		word(arithmExp(&ast.BinaryExpr{
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
		[]string{"$(($i | 13))"},
		word(arithmExp(&ast.BinaryExpr{
			Op: token.OR,
			X:  word(litParamExp("i")),
			Y:  litWord("13"),
		})),
	},
	{
		[]string{"$((3 & $((4))))"},
		word(arithmExp(&ast.BinaryExpr{
			Op: token.AND,
			X:  litWord("3"),
			Y:  word(arithmExp(litWord("4"))),
		})),
	},
	{
		[]string{
			"$((3 % 7))",
			"$((3\n% 7))",
			"$((3\\\n % 7))",
		},
		word(arithmExp(&ast.BinaryExpr{
			Op: token.REM,
			X:  litWord("3"),
			Y:  litWord("7"),
		})),
	},
	{
		[]string{`"$((1 / 3))"`},
		word(dblQuoted(arithmExp(&ast.BinaryExpr{
			Op: token.QUO,
			X:  litWord("1"),
			Y:  litWord("3"),
		}))),
	},
	{
		[]string{"$((2 ** 10))"},
		word(arithmExp(&ast.BinaryExpr{
			Op: token.POW,
			X:  litWord("2"),
			Y:  litWord("10"),
		})),
	},
	{
		[]string{`$(((1) ^ 3))`},
		word(arithmExp(&ast.BinaryExpr{
			Op: token.XOR,
			X:  parenExpr(litWord("1")),
			Y:  litWord("3"),
		})),
	},
	{
		[]string{`$((1 >> (3 << 2)))`},
		word(arithmExp(&ast.BinaryExpr{
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
		[]string{`$((-(1)))`},
		word(arithmExp(&ast.UnaryExpr{
			Op: token.SUB,
			X:  parenExpr(litWord("1")),
		})),
	},
	{
		[]string{`$((i++))`},
		word(arithmExp(&ast.UnaryExpr{
			Op:   token.INC,
			Post: true,
			X:    litWord("i"),
		})),
	},
	{
		[]string{`$((--i))`},
		word(arithmExp(&ast.UnaryExpr{
			Op: token.DEC,
			X:  litWord("i"),
		})),
	},
	{
		[]string{`$((!i))`},
		word(arithmExp(&ast.UnaryExpr{
			Op: token.NOT,
			X:  litWord("i"),
		})),
	},
	{
		[]string{`$((-!+i))`},
		word(arithmExp(&ast.UnaryExpr{
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
		[]string{`$((!!i))`},
		word(arithmExp(&ast.UnaryExpr{
			Op: token.NOT,
			X: &ast.UnaryExpr{
				Op: token.NOT,
				X:  litWord("i"),
			},
		})),
	},
	{
		[]string{`$((1 < 3))`},
		word(arithmExp(&ast.BinaryExpr{
			Op: token.LSS,
			X:  litWord("1"),
			Y:  litWord("3"),
		})),
	},
	{
		[]string{`$((i = 2))`, `$((i=2))`},
		word(arithmExp(&ast.BinaryExpr{
			Op: token.ASSIGN,
			X:  litWord("i"),
			Y:  litWord("2"),
		})),
	},
	{
		[]string{"$((a += 2, b -= 3))"},
		word(arithmExp(&ast.BinaryExpr{
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
		[]string{"$((a >>= 2, b <<= 3))"},
		word(arithmExp(&ast.BinaryExpr{
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
		[]string{"$((a == b && c > d))"},
		word(arithmExp(&ast.BinaryExpr{
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
		[]string{"$((a != b))"},
		word(arithmExp(&ast.BinaryExpr{
			Op: token.NEQ,
			X:  litWord("a"),
			Y:  litWord("b"),
		})),
	},
	{
		[]string{"$((a &= b))"},
		word(arithmExp(&ast.BinaryExpr{
			Op: token.ANDASSGN,
			X:  litWord("a"),
			Y:  litWord("b"),
		})),
	},
	{
		[]string{"$((a |= b))"},
		word(arithmExp(&ast.BinaryExpr{
			Op: token.ORASSGN,
			X:  litWord("a"),
			Y:  litWord("b"),
		})),
	},
	{
		[]string{"$((a %= b))"},
		word(arithmExp(&ast.BinaryExpr{
			Op: token.REMASSGN,
			X:  litWord("a"),
			Y:  litWord("b"),
		})),
	},
	{
		[]string{"$((a /= b))"},
		word(arithmExp(&ast.BinaryExpr{
			Op: token.QUOASSGN,
			X:  litWord("a"),
			Y:  litWord("b"),
		})),
	},
	{
		[]string{"$((a ^= b))"},
		word(arithmExp(&ast.BinaryExpr{
			Op: token.XORASSGN,
			X:  litWord("a"),
			Y:  litWord("b"),
		})),
	},
	{
		[]string{"$((i *= 3))"},
		word(arithmExp(&ast.BinaryExpr{
			Op: token.MULASSGN,
			X:  litWord("i"),
			Y:  litWord("3"),
		})),
	},
	{
		[]string{"$((2 >= 10))"},
		word(arithmExp(&ast.BinaryExpr{
			Op: token.GEQ,
			X:  litWord("2"),
			Y:  litWord("10"),
		})),
	},
	{
		[]string{"$((foo ? b1 : b2))"},
		word(arithmExp(&ast.BinaryExpr{
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
		[]string{`$((a <= (1 || 2)))`},
		word(arithmExp(&ast.BinaryExpr{
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
		[]string{`((a <= 2))`},
		word(&ast.ArithmExp{Token: token.DLPAREN, X: &ast.BinaryExpr{
			Op: token.LEQ,
			X:  litWord("a"),
			Y:  litWord("2"),
		}}),
	},
	{
		[]string{"foo$", "foo$\n"},
		word(lit("foo"), lit("$")),
	},
	{
		[]string{`$'foo'`},
		word(&ast.Quoted{Quote: token.DOLLSQ, Parts: lits("foo")}),
	},
	{
		[]string{`$'foo${'`},
		word(&ast.Quoted{Quote: token.DOLLSQ, Parts: lits("foo${")}),
	},
	{
		[]string{"$'foo bar`'"},
		word(&ast.Quoted{Quote: token.DOLLSQ, Parts: lits("foo bar`")}),
	},
	{
		[]string{`$'f\'oo'`},
		word(&ast.Quoted{Quote: token.DOLLSQ, Parts: lits(`f\'oo`)}),
	},
	{
		[]string{`$"foo"`},
		word(&ast.Quoted{Quote: token.DOLLDQ, Parts: lits("foo")}),
	},
	{
		[]string{`$"foo$"`},
		word(&ast.Quoted{Quote: token.DOLLDQ, Parts: lits("foo", "$")}),
	},
	{
		[]string{`$"foo bar"`},
		word(&ast.Quoted{Quote: token.DOLLDQ, Parts: lits(`foo bar`)}),
	},
	{
		[]string{`$"f\"oo"`},
		word(&ast.Quoted{Quote: token.DOLLDQ, Parts: lits(`f\"oo`)}),
	},
	{
		[]string{`"foo$"`},
		word(dblQuoted(lit("foo"), lit("$"))),
	},
	{
		[]string{`"foo$$"`},
		word(dblQuoted(lit("foo"), litParamExp("$"))),
	},
	{
		[]string{"`foo$`"},
		word(bckQuoted(
			stmt(call(*word(lit("foo"), lit("$")))),
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
		word(lit("foo"), &ast.ParamExp{Param: *lit("bar")}),
	},
	{
		[]string{"'foo${bar'"},
		word(sglQuoted("foo${bar")),
	},
	{
		[]string{"(foo)\nbar", "(foo); bar"},
		[]ast.Command{
			subshell(litStmt("foo")),
			litCall("bar"),
		},
	},
	{
		[]string{"foo\n(bar)", "foo; (bar)"},
		[]ast.Command{
			litCall("foo"),
			subshell(litStmt("bar")),
		},
	},
	{
		[]string{"foo\n(bar)", "foo; (bar)"},
		[]ast.Command{
			litCall("foo"),
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
		&ast.CaseClause{
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
		[]string{"case $i in 1) a ;;& 2) b ;& 3) c ;; esac"},
		&ast.CaseClause{
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
		[]string{"foo | while read a; do b; done"},
		&ast.BinaryCmd{
			Op: token.OR,
			X:  litStmt("foo"),
			Y: stmt(&ast.WhileClause{
				Cond: &ast.StmtCond{Stmts: []*ast.Stmt{
					litStmt("read", "a"),
				}},
				DoStmts: litStmts("b"),
			}),
		},
	},
	{
		[]string{"while read l; do foo || bar; done"},
		&ast.WhileClause{
			Cond: &ast.StmtCond{Stmts: []*ast.Stmt{litStmt("read", "l")}},
			DoStmts: stmts(&ast.BinaryCmd{
				Op: token.LOR,
				X:  litStmt("foo"),
				Y:  litStmt("bar"),
			}),
		},
	},
	{
		[]string{"echo if while"},
		litCall("echo", "if", "while"),
	},
	{
		[]string{"${foo}if"},
		word(&ast.ParamExp{Param: *lit("foo")}, lit("if")),
	},
	{
		[]string{"$if"},
		word(litParamExp("if")),
	},
	{
		[]string{"if; then; fi", "if\nthen\nfi"},
		&ast.IfClause{},
	},
	{
		[]string{"if; then a=; fi", "if; then a=\nfi"},
		&ast.IfClause{
			ThenStmts: []*ast.Stmt{
				{Assigns: []*ast.Assign{
					{Name: lit("a")},
				}},
			},
		},
	},
	{
		[]string{"if; then >f; fi", "if; then >f\nfi"},
		&ast.IfClause{
			ThenStmts: []*ast.Stmt{
				{Redirs: []*ast.Redirect{
					{Op: token.GTR, Word: *litWord("f")},
				}},
			},
		},
	},
	{
		[]string{"if; then (a); fi", "if; then (a) fi"},
		&ast.IfClause{
			ThenStmts: []*ast.Stmt{
				stmt(subshell(litStmt("a"))),
			},
		},
	},
	{
		[]string{"a=b\nc=d", "a=b; c=d"},
		[]*ast.Stmt{
			{Assigns: []*ast.Assign{
				{Name: lit("a"), Value: *litWord("b")},
			}},
			{Assigns: []*ast.Assign{
				{Name: lit("c"), Value: *litWord("d")},
			}},
		},
	},
	{
		[]string{"while; do; done", "while\ndo\ndone"},
		&ast.WhileClause{},
	},
	{
		[]string{"while; do; done", "while\ndo\n#foo\ndone"},
		&ast.WhileClause{},
	},
	{
		[]string{"until; do; done", "until\ndo\ndone"},
		&ast.UntilClause{},
	},
	{
		[]string{"for i; do; done", "for i\ndo\ndone"},
		&ast.ForClause{Loop: &ast.WordIter{Name: *lit("i")}},
	},
	{
		[]string{"case i in; esac"},
		&ast.CaseClause{Word: *litWord("i")},
	},
	{
		[]string{"foo && write | read"},
		&ast.BinaryCmd{
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
		[]string{"write | read && bar"},
		&ast.BinaryCmd{
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
		[]string{"foo >f | bar"},
		&ast.BinaryCmd{
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
		[]string{"(foo) >f | bar"},
		&ast.BinaryCmd{
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
		[]string{"foo | >f"},
		&ast.BinaryCmd{
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
		[]string{"declare -f func"},
		&ast.DeclClause{
			Opts: litWords("-f"),
			Assigns: []*ast.Assign{
				{Value: *litWord("func")},
			},
		},
	},
	{
		[]string{"declare -a -bc foo=bar"},
		&ast.DeclClause{
			Opts: litWords("-a", "-bc"),
			Assigns: []*ast.Assign{
				{Name: lit("foo"), Value: *litWord("bar")},
			},
		},
	},
	{
		[]string{"declare -a foo=(b1 `b2`)"},
		&ast.DeclClause{
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
		[]string{"local -a foo=(b1 `b2`)"},
		&ast.DeclClause{
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
		[]string{"declare -f func >/dev/null"},
		&ast.Stmt{
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
		[]string{"eval"},
		&ast.EvalClause{},
	},
	{
		[]string{"eval a=b foo"},
		&ast.EvalClause{Stmt: &ast.Stmt{
			Cmd: litCall("foo"),
			Assigns: []*ast.Assign{{
				Name:  lit("a"),
				Value: *litWord("b"),
			}},
		}},
	},
	{
		[]string{`let i++`},
		letClause(
			&ast.UnaryExpr{
				Op:   token.INC,
				Post: true,
				X:    litWord("i"),
			},
		),
	},
	{
		[]string{`let a++ b++ c +d`},
		letClause(
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
		[]string{`let "--i"`},
		letClause(
			word(dblQuoted(lit("--i"))),
		),
	},
	{
		[]string{`let ++i >/dev/null`},
		&ast.Stmt{
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
		[]string{
			`let a=(1 + 2) b=3+4`,
			`let a=(1+2) b=3+4`,
		},
		letClause(
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
		[]string{"(foo-bar)"},
		subshell(litStmt("foo-bar")),
	},
	{
		[]string{
			"let i++\nbar",
			"let i++; bar",
		},
		[]*ast.Stmt{
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
		[]string{
			"let i++\nfoo=(bar)",
			"let i++; foo=(bar)",
			"let i++; foo=(bar)\n",
		},
		[]*ast.Stmt{
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
		[]string{
			"case a in b) let i++ ;; esac",
			"case a in b) let i++;; esac",
		},
		&ast.CaseClause{
			Word: *word(lit("a")),
			List: []*ast.PatternList{{
				Op:       token.DSEMICOLON,
				Patterns: litWords("b"),
				Stmts: []*ast.Stmt{stmt(letClause(
					&ast.UnaryExpr{
						Op:   token.INC,
						Post: true,
						X:    litWord("i"),
					},
				))},
			}},
		},
	},
	{
		[]string{"let $?"},
		letClause(word(litParamExp("?"))),
	},
	{
		[]string{"a=(b c) foo"},
		&ast.Stmt{
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
		[]string{"a=(b c) foo", "a=(\nb\nc\n) foo"},
		&ast.Stmt{
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
		[]string{"a+=1 b+=(2 3)"},
		&ast.Stmt{
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
		[]string{"<<EOF | b\nfoo\nEOF", "<<EOF|b;\nfoo\n"},
		&ast.BinaryCmd{
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
		[]string{"<<EOF1 <<EOF2 | c && d\nEOF1\nEOF2"},
		&ast.BinaryCmd{
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
		[]string{
			"<<EOF && { bar; }\nhdoc\nEOF",
			"<<EOF &&\nhdoc\nEOF\n{ bar; }",
		},
		&ast.BinaryCmd{
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
		[]string{"foo() {\n\t<<EOF && { bar; }\nhdoc\nEOF\n}"},
		&ast.FuncDecl{
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
		[]string{"\"a`\"\"`\""},
		word(dblQuoted(
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
	case nil:
		return &ast.File{}
	}
	return nil
}

func SetPosRecurse(tb testing.TB, src string, v interface{}, to token.Pos, diff bool) {
	checkSrc := func(pos token.Pos, want string) {
		if src == "" {
			return
		}
		offs := int(pos - 1)
		got := string([]byte(src[offs:]))
		got = strings.Replace(got, "\\\n", "", -1)[:len(want)]
		if got != want {
			tb.Fatalf("Expected %q at %d in %q, found %q",
				want, offs, src, got)
		}
	}
	setPos := func(p *token.Pos, s string) {
		if s != "" {
			checkSrc(*p, s)
		}
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
		setPos(&x.Position, "")
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
	case *ast.Cond:
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
		recurse(&x.Cond)
		recurse(x.ThenStmts)
		for _, e := range x.Elifs {
			setPos(&e.Elif, "elif")
			setPos(&e.Then, "then")
			recurse(e.Cond)
			recurse(e.ThenStmts)
		}
		if len(x.ElseStmts) > 0 {
			setPos(&x.Else, "else")
			recurse(x.ElseStmts)
		}
	case *ast.StmtCond:
		recurse(x.Stmts)
	case *ast.CStyleCond:
		setPos(&x.Lparen, "((")
		setPos(&x.Rparen, "))")
		recurse(&x.X)
	case *ast.WhileClause:
		setPos(&x.While, "while")
		setPos(&x.Do, "do")
		setPos(&x.Done, "done")
		recurse(&x.Cond)
		recurse(x.DoStmts)
	case *ast.UntilClause:
		setPos(&x.Until, "until")
		setPos(&x.Do, "do")
		setPos(&x.Done, "done")
		recurse(&x.Cond)
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
		setPos(&x.OpPos, x.Op.String())
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
			setPos(&x.Position, "")
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
			setPos(&pl.OpPos, "")
			recurse(pl.Patterns)
			recurse(pl.Stmts)
		}
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
