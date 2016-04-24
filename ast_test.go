// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"reflect"
	"strings"
	"testing"
)

func litWord(val string) Word {
	return Word{Parts: []Node{Lit{Val: val}}}
}

func litWords(strs ...string) []Word {
	l := make([]Word, 0, len(strs))
	for _, s := range strs {
		l = append(l, litWord(s))
	}
	return l
}

func litCmd(strs ...string) Command {
	return Command{Args: litWords(strs...)}
}

var tests = []struct {
	ins  []string
	want interface{}
}{
	{
		[]string{"", " ", "\n", "# foo"},
		nil,
	},
	{
		[]string{"foo", "foo ", " foo", "foo # bar"},
		litCmd("foo"),
	},
	{
		[]string{"foo; bar", "foo; bar;", "foo;bar;", "\nfoo\nbar\n"},
		[]Node{
			litCmd("foo"),
			litCmd("bar"),
		},
	},
	{
		[]string{"foo a b", " foo  a  b ", "foo \\\n a b"},
		litCmd("foo", "a", "b"),
	},
	{
		[]string{"foobar", "foo\\\nbar"},
		litCmd("foobar"),
	},
	{
		[]string{"foo'bar'"},
		litCmd("foo'bar'"),
	},
	{
		[]string{"(foo)", "(foo;)", "(\nfoo\n)"},
		Subshell{Stmts: []Stmt{
			{Node: litCmd("foo")},
		}},
	},
	{
		[]string{"{ foo; }", "{foo;}", "{\nfoo\n}"},
		Block{Stmts: []Stmt{
			{Node: litCmd("foo")},
		}},
	},
	{
		[]string{
			"if a; then b; fi",
			"if a\nthen\nb\nfi",
		},
		IfStmt{
			Cond: Stmt{Node: litCmd("a")},
			ThenStmts: []Stmt{
				{Node: litCmd("b")},
			},
		},
	},
	{
		[]string{
			"if a; then b; else c; fi",
			"if a\nthen b\nelse\nc\nfi",
		},
		IfStmt{
			Cond: Stmt{Node: litCmd("a")},
			ThenStmts: []Stmt{
				{Node: litCmd("b")},
			},
			ElseStmts: []Stmt{
				{Node: litCmd("c")},
			},
		},
	},
	{
		[]string{
			"if a; then a; elif b; then b; elif c; then c; else d; fi",
			"if a\nthen a\nelif b\nthen b\nelif c\nthen c\nelse\nd\nfi",
		},
		IfStmt{
			Cond: Stmt{Node: litCmd("a")},
			ThenStmts: []Stmt{
				{Node: litCmd("a")},
			},
			Elifs: []Elif{
				{Cond: Stmt{Node: litCmd("b")},
					ThenStmts: []Stmt{
						{Node: litCmd("b")},
					}},
				{Cond: Stmt{Node: litCmd("c")},
					ThenStmts: []Stmt{
						{Node: litCmd("c")},
					}},
			},
			ElseStmts: []Stmt{
				{Node: litCmd("d")},
			},
		},
	},
	{
		[]string{"while a; do b; done", "while a\ndo\nb\ndone"},
		WhileStmt{
			Cond: Stmt{Node: litCmd("a")},
			DoStmts: []Stmt{
				{Node: litCmd("b")},
			},
		},
	},
	{
		[]string{
			"for i in 1 2 3; do echo $i; done",
			"for i in 1 2 3\ndo echo $i\ndone",
		},
		ForStmt{
			Name:     Lit{Val: "i"},
			WordList: litWords("1", "2", "3"),
			DoStmts: []Stmt{
				{Node: Command{Args: []Word{
					litWord("echo"),
					{Parts: []Node{
						ParamExp{Short: true, Text: "i"},
					}},
				}}},
			},
		},
	},
	{
		[]string{`echo ' ' "foo bar"`},
		Command{Args: []Word{
			litWord("echo"),
			litWord("' '"),
			{Parts: []Node{
				DblQuoted{Parts: []Node{Lit{Val: "foo bar"}}},
			}},
		}},
	},
	{
		[]string{`"foo \" bar"`},
		Command{Args: []Word{
			{Parts: []Node{
				DblQuoted{Parts: []Node{Lit{Val: `foo \" bar`}}},
			}},
		}},
	},
	{
		[]string{"\">foo\" \"\nbar\""},
		Command{Args: []Word{
			{Parts: []Node{
				DblQuoted{Parts: []Node{Lit{Val: ">foo"}}},
			}},
			{Parts: []Node{
				DblQuoted{Parts: []Node{Lit{Val: "\nbar"}}},
			}},
		}},
	},
	{
		[]string{`foo \" bar`},
		litCmd(`foo`, `\"`, `bar`),
	},
	{
		[]string{"s{s s=s"},
		litCmd("s{s", "s=s"),
	},
	{
		[]string{"foo && bar", "foo&&bar", "foo &&\nbar"},
		BinaryExpr{
			Op: LAND,
			X:  Stmt{Node: litCmd("foo")},
			Y:  Stmt{Node: litCmd("bar")},
		},
	},
	{
		[]string{"foo || bar", "foo||bar", "foo ||\nbar"},
		BinaryExpr{
			Op: LOR,
			X:  Stmt{Node: litCmd("foo")},
			Y:  Stmt{Node: litCmd("bar")},
		},
	},
	{
		[]string{"if a; then b; fi || while a; do b; done"},
		BinaryExpr{
			Op: LOR,
			X: Stmt{Node: IfStmt{
				Cond: Stmt{Node: litCmd("a")},
				ThenStmts: []Stmt{
					{Node: litCmd("b")},
				},
			}},
			Y: Stmt{Node: WhileStmt{
				Cond: Stmt{Node: litCmd("a")},
				DoStmts: []Stmt{
					{Node: litCmd("b")},
				},
			}},
		},
	},
	{
		[]string{"foo && bar1 || bar2"},
		BinaryExpr{
			Op: LAND,
			X:  Stmt{Node: litCmd("foo")},
			Y: Stmt{Node: BinaryExpr{
				Op: LOR,
				X:  Stmt{Node: litCmd("bar1")},
				Y:  Stmt{Node: litCmd("bar2")},
			}},
		},
	},
	{
		[]string{"foo | bar", "foo|bar"},
		BinaryExpr{
			Op: OR,
			X:  Stmt{Node: litCmd("foo")},
			Y:  Stmt{Node: litCmd("bar")},
		},
	},
	{
		[]string{"foo | bar | extra"},
		BinaryExpr{
			Op: OR,
			X:  Stmt{Node: litCmd("foo")},
			Y: Stmt{Node: BinaryExpr{
				Op: OR,
				X:  Stmt{Node: litCmd("bar")},
				Y:  Stmt{Node: litCmd("extra")},
			}},
		},
	},
	{
		[]string{
			"foo() { a; b; }",
			"foo() {\na\nb\n}",
			"foo ( ) {\na\nb\n}",
		},
		FuncDecl{
			Name: Lit{Val: "foo"},
			Body: Stmt{Node: Block{Stmts: []Stmt{
				{Node: litCmd("a")},
				{Node: litCmd("b")},
			}}},
		},
	},
	{
		[]string{
			"foo >a >>b <c",
			"foo > a >> b < c",
			">a >>b foo <c",
		},
		Stmt{
			Node: Command{
				Args: []Word{litWord("foo")},
			},
			Redirs: []Redirect{
				{Op: RDROUT, Word: litWord("a")},
				{Op: APPEND, Word: litWord("b")},
				{Op: RDRIN, Word: litWord("c")},
			},
		},
	},
	{
		[]string{
			"foo bar >a",
			"foo >a bar",
		},
		Stmt{
			Node: Command{
				Args: litWords("foo", "bar"),
			},
			Redirs: []Redirect{
				{Op: RDROUT, Word: litWord("a")},
			},
		},
	},
	{
		[]string{"foo <<EOF\nbar\nEOF"},
		Stmt{
			Node: Command{
				Args: []Word{litWord("foo")},
			},
			Redirs: []Redirect{
				{Op: HEREDOC, Word: litWord("EOF\nbar\nEOF")},
			},
		},
	},
	{
		[]string{"foo <<FOOBAR\nbar\nFOOBAR"},
		Stmt{
			Node: Command{
				Args: []Word{litWord("foo")},
			},
			Redirs: []Redirect{
				{Op: HEREDOC, Word: litWord("FOOBAR\nbar\nFOOBAR")},
			},
		},
	},
	{
		[]string{"foo >&2 <&0 2>file"},
		Stmt{
			Node: Command{
				Args: []Word{litWord("foo")},
			},
			Redirs: []Redirect{
				{Op: DPLOUT, Word: litWord("2")},
				{Op: DPLIN, Word: litWord("0")},
				{Op: RDROUT, N: Lit{Val: "2"}, Word: litWord("file")},
			},
		},
	},
	{
		[]string{"a >f1; b >f2"},
		[]Stmt{
			{
				Node: Command{Args: []Word{litWord("a")}},
				Redirs: []Redirect{{Op: RDROUT, Word: litWord("f1")}},
			},
			{
				Node: Command{Args: []Word{litWord("b")}},
				Redirs: []Redirect{{Op: RDROUT, Word: litWord("f2")}},
			},
		},
	},
	{
		[]string{"foo &", "foo&"},
		Stmt{
			Node:       litCmd("foo"),
			Background: true,
		},
	},
	{
		[]string{"if foo; then bar; fi >/dev/null &"},
		Stmt{
			Node: IfStmt{
				Cond: Stmt{Node: litCmd("foo")},
				ThenStmts: []Stmt{
					{Node: litCmd("bar")},
				},
			},
			Redirs: []Redirect{
				{Op: RDROUT, Word: litWord("/dev/null")},
			},
			Background: true,
		},
	},
	{
		[]string{"echo foo#bar"},
		litCmd("echo", "foo#bar"),
	},
	{
		[]string{"echo $(foo bar)"},
		Command{Args: []Word{
			litWord("echo"),
			{Parts: []Node{
				CmdSubst{Stmts: []Stmt{
					{Node: litCmd("foo", "bar")},
				}},
			}},
		}},
	},
	{
		[]string{"echo $(foo | bar)"},
		Command{Args: []Word{
			litWord("echo"),
			{Parts: []Node{
				CmdSubst{Stmts: []Stmt{
					{Node: BinaryExpr{
						Op: OR,
						X:  Stmt{Node: litCmd("foo")},
						Y:  Stmt{Node: litCmd("bar")},
					}},
				}},
			}},
		}},
	},
	{
		[]string{`echo "$foo"`},
		Command{Args: []Word{
			litWord("echo"),
			{Parts: []Node{
				DblQuoted{Parts: []Node{
					ParamExp{Short: true, Text: "foo"},
				}},
			}},
		}},
	},
	{
		[]string{`$@ $# $$`},
		Command{Args: []Word{
			{Parts: []Node{ParamExp{Short: true, Text: "@"}}},
			{Parts: []Node{ParamExp{Short: true, Text: "#"}}},
			{Parts: []Node{ParamExp{Short: true, Text: "$"}}},
		}},
	},
	{
		[]string{`echo "${foo}"`},
		Command{Args: []Word{
			litWord("echo"),
			{Parts: []Node{
				DblQuoted{Parts: []Node{
					ParamExp{Text: "foo"}},
				},
			}},
		}},
	},
	{
		[]string{`echo "$(foo)"`},
		Command{Args: []Word{
			litWord("echo"),
			{Parts: []Node{
				DblQuoted{Parts: []Node{
					CmdSubst{Stmts: []Stmt{
						{Node: litCmd("foo")},
					}},
				}},
			}},
		}},
	},
	{
		[]string{`echo '${foo}'`},
		litCmd("echo", "'${foo}'"),
	},
	{
		[]string{"echo ${foo bar}"},
		Command{Args: []Word{
			litWord("echo"),
			{Parts: []Node{
				ParamExp{Text: "foo bar"},
			}},
		}},
	},
	{
		[]string{"echo $(($x-1))"},
		Command{Args: []Word{
			litWord("echo"),
			{Parts: []Node{
				ArithmExp{Text: "$x-1"},
			}},
		}},
	},
	{
		[]string{"echo foo$bar"},
		Command{Args: []Word{
			litWord("echo"),
			{Parts: []Node{
				Lit{Val: "foo"},
				ParamExp{Short: true, Text: "bar"},
			}},
		}},
	},
	{
		[]string{"echo foo$(bar bar)"},
		Command{Args: []Word{
			litWord("echo"),
			{Parts: []Node{
				Lit{Val: "foo"},
				CmdSubst{Stmts: []Stmt{
					{Node: litCmd("bar", "bar")},
				}},
			}},
		}},
	},
	{
		[]string{"echo foo${bar bar}"},
		Command{Args: []Word{
			litWord("echo"),
			{Parts: []Node{
				Lit{Val: "foo"},
				ParamExp{Text: "bar bar"},
			}},
		}},
	},
	{
		[]string{"echo 'foo${bar'"},
		litCmd("echo", "'foo${bar'"),
	},
	{
		[]string{"(foo); bar"},
		[]Node{
			Subshell{Stmts: []Stmt{
				{Node: litCmd("foo")},
			}},
			litCmd("bar"),
		},
	},
	{
		[]string{"a=\"\nbar\""},
		Command{Args: []Word{
			{Parts: []Node{
				Lit{Val: "a="},
				DblQuoted{Parts: []Node{Lit{Val: "\nbar"}}},
			}},
		}},
	},
	{
		[]string{
			"case $i in 1) foo;; 2 | 3*) bar; esac",
			"case $i in 1) foo;; 2 | 3*) bar;; esac",
			"case $i in\n1)\nfoo\n;;\n2 | 3*)\nbar\n;;\nesac",
		},
		CaseStmt{
			Word: Word{Parts: []Node{
				ParamExp{Short: true, Text: "i"},
			}},
			List: []PatternList{
				{
					Patterns: litWords("1"),
					Stmts: []Stmt{
						{Node: litCmd("foo")},
					},
				},
				{
					Patterns: litWords("2", "3*"),
					Stmts: []Stmt{
						{Node: litCmd("bar")},
					},
				},
			},
		},
	},
	{
		[]string{"foo | while read a; do b; done"},
		BinaryExpr{
			Op: OR,
			X:  Stmt{Node: litCmd("foo")},
			Y: Stmt{Node: WhileStmt{
				Cond: Stmt{Node: litCmd("read", "a")},
				DoStmts: []Stmt{
					{Node: litCmd("b")},
				},
			}},
		},
	},
	{
		[]string{"while read l; do foo || bar; done"},
		WhileStmt{
			Cond: Stmt{Node: litCmd("read", "l")},
			DoStmts: []Stmt{
				{Node: BinaryExpr{
					Op: LOR,
					X:  Stmt{Node: litCmd("foo")},
					Y:  Stmt{Node: litCmd("bar")},
				}},
			},
		},
	},
	{
		[]string{"echo if while"},
		litCmd("echo", "if", "while"),
	},
	{
		[]string{"echo ${foo}if"},
		Command{Args: []Word{
			litWord("echo"),
			{Parts: []Node{
				ParamExp{Text: "foo"},
				Lit{Val: "if"},
			}},
		}},
	},
	{
		[]string{"echo $if"},
		Command{Args: []Word{
			litWord("echo"),
			{Parts: []Node{
				ParamExp{Short: true, Text: "if"},
			}},
		}},
	},
}

func wantedProg(v interface{}) (p Prog) {
	switch x := v.(type) {
	case []Stmt:
		p.Stmts = x
	case Stmt:
		p.Stmts = append(p.Stmts, x)
	case []Node:
		for _, n := range x {
			p.Stmts = append(p.Stmts, Stmt{Node: n})
		}
	case Node:
		p.Stmts = append(p.Stmts, Stmt{Node: x})
	}
	return
}

func TestParseAST(t *testing.T) {
	for _, c := range tests {
		want := wantedProg(c.want)
		for _, in := range c.ins {
			r := strings.NewReader(in)
			got, err := Parse(r, "")
			if err != nil {
				t.Fatalf("Unexpected error in %q: %v", in, err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("AST mismatch in %q\nwant: %s\ngot:  %s\ndumps:\n%#v\n%#v",
					in, want.String(), got.String(), want, got)
			}
		}
	}
}

func TestPrintAST(t *testing.T) {
	for _, c := range tests {
		in := wantedProg(c.want)
		want := c.ins[0]
		got := in.String()
		if got != want {
			t.Fatalf("AST print mismatch\nwant: %s\ngot:  %s",
				want, got)
		}
	}
}
