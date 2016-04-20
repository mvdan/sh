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

func litNodes(strs ...string) []Node {
	l := make([]Node, 0, len(strs))
	for _, s := range strs {
		l = append(l, litWord(s))
	}
	return l
}

func litWords(strs ...string) []Word {
	l := make([]Word, 0, len(strs))
	for _, s := range strs {
		l = append(l, litWord(s))
	}
	return l
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
		Command{Args: litNodes("foo")},
	},
	{
		[]string{"foo; bar", "foo; bar;", "foo;bar;", "\nfoo\nbar\n"},
		[]Node{
			Command{Args: litNodes("foo")},
			Command{Args: litNodes("bar")},
		},
	},
	{
		[]string{"foo a b", " foo  a  b ", "foo \\\n a b"},
		Command{Args: litNodes("foo", "a", "b")},
	},
	{
		[]string{"foobar", "foo\\\nbar"},
		Command{Args: litNodes("foobar")},
	},
	{
		[]string{"foo'bar'"},
		Command{Args: litNodes("foo'bar'")},
	},
	{
		[]string{"(foo)", "(foo;)", "(\nfoo\n)"},
		Subshell{Stmts: []Stmt{
			{Node: Command{Args: litNodes("foo")}},
		}},
	},
	{
		[]string{"{ foo; }", "{foo;}", "{\nfoo\n}"},
		Block{Stmts: []Stmt{
			{Node: Command{Args: litNodes("foo")}},
		}},
	},
	{
		[]string{
			"if a; then b; fi",
			"if a\nthen\nb\nfi",
		},
		IfStmt{
			Cond: Stmt{Node: Command{Args: litNodes("a")}},
			ThenStmts: []Stmt{
				{Node: Command{Args: litNodes("b")}},
			},
		},
	},
	{
		[]string{
			"if a; then b; else c; fi",
			"if a\nthen b\nelse\nc\nfi",
		},
		IfStmt{
			Cond: Stmt{Node: Command{Args: litNodes("a")}},
			ThenStmts: []Stmt{
				{Node: Command{Args: litNodes("b")}},
			},
			ElseStmts: []Stmt{
				{Node: Command{Args: litNodes("c")}},
			},
		},
	},
	{
		[]string{
			"if a; then a; elif b; then b; elif c; then c; else d; fi",
			"if a\nthen a\nelif b\nthen b\nelif c\nthen c\nelse\nd\nfi",
		},
		IfStmt{
			Cond: Stmt{Node: Command{Args: litNodes("a")}},
			ThenStmts: []Stmt{
				{Node: Command{Args: litNodes("a")}},
			},
			Elifs: []Elif{
				{Cond: Stmt{Node: Command{Args: litNodes("b")}},
					ThenStmts: []Stmt{
						{Node: Command{Args: litNodes("b")}},
					}},
				{Cond: Stmt{Node: Command{Args: litNodes("c")}},
					ThenStmts: []Stmt{
						{Node: Command{Args: litNodes("c")}},
					}},
			},
			ElseStmts: []Stmt{
				{Node: Command{Args: litNodes("d")}},
			},
		},
	},
	{
		[]string{"while a; do b; done", "while a\ndo\nb\ndone"},
		WhileStmt{
			Cond: Stmt{Node: Command{Args: litNodes("a")}},
			DoStmts: []Stmt{
				{Node: Command{Args: litNodes("b")}},
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
			WordList: litNodes("1", "2", "3"),
			DoStmts: []Stmt{
				{Node: Command{Args: []Node{
					litWord("echo"),
					Word{Parts: []Node{
						ParamExp{Short: true, Text: "i"},
					}},
				}}},
			},
		},
	},
	{
		[]string{`echo ' ' "foo bar"`},
		Command{Args: []Node{
			litWord("echo"),
			litWord("' '"),
			Word{Parts: []Node{
				DblQuoted{Parts: []Node{Lit{Val: "foo bar"}}},
			}},
		}},
	},
	{
		[]string{`"foo \" bar"`},
		Command{Args: []Node{
			Word{Parts: []Node{
				DblQuoted{Parts: []Node{Lit{Val: `foo \" bar`}}},
			}},
		}},
	},
	{
		[]string{"\">foo\" \"\nbar\""},
		Command{Args: []Node{
			Word{Parts: []Node{
				DblQuoted{Parts: []Node{Lit{Val: ">foo"}}},
			}},
			Word{Parts: []Node{
				DblQuoted{Parts: []Node{Lit{Val: "\nbar"}}},
			}},
		}},
	},
	{
		[]string{`foo \" bar`},
		Command{Args: litNodes(`foo`, `\"`, `bar`)},
	},
	{
		[]string{"s{s s=s"},
		Command{Args: litNodes("s{s", "s=s")},
	},
	{
		[]string{"foo && bar", "foo&&bar", "foo &&\nbar"},
		BinaryExpr{
			Op: LAND,
			X:  Stmt{Node: Command{Args: litNodes("foo")}},
			Y:  Stmt{Node: Command{Args: litNodes("bar")}},
		},
	},
	{
		[]string{"foo || bar", "foo||bar", "foo ||\nbar"},
		BinaryExpr{
			Op: LOR,
			X:  Stmt{Node: Command{Args: litNodes("foo")}},
			Y:  Stmt{Node: Command{Args: litNodes("bar")}},
		},
	},
	{
		[]string{"if a; then b; fi || while a; do b; done"},
		BinaryExpr{
			Op: LOR,
			X: Stmt{Node: IfStmt{
				Cond: Stmt{Node: Command{Args: litNodes("a")}},
				ThenStmts: []Stmt{
					{Node: Command{Args: litNodes("b")}},
				},
			}},
			Y: Stmt{Node: WhileStmt{
				Cond: Stmt{Node: Command{Args: litNodes("a")}},
				DoStmts: []Stmt{
					Stmt{Node: Command{Args: litNodes("b")}},
				},
			}},
		},
	},
	{
		[]string{"foo && bar1 || bar2"},
		BinaryExpr{
			Op: LAND,
			X:  Stmt{Node: Command{Args: litNodes("foo")}},
			Y: Stmt{Node: BinaryExpr{
				Op: LOR,
				X:  Stmt{Node: Command{Args: litNodes("bar1")}},
				Y:  Stmt{Node: Command{Args: litNodes("bar2")}},
			}},
		},
	},
	{
		[]string{"foo | bar", "foo|bar"},
		BinaryExpr{
			Op: OR,
			X:  Stmt{Node: Command{Args: litNodes("foo")}},
			Y:  Stmt{Node: Command{Args: litNodes("bar")}},
		},
	},
	{
		[]string{"foo | bar | extra"},
		BinaryExpr{
			Op: OR,
			X:  Stmt{Node: Command{Args: litNodes("foo")}},
			Y: Stmt{Node: BinaryExpr{
				Op: OR,
				X:  Stmt{Node: Command{Args: litNodes("bar")}},
				Y:  Stmt{Node: Command{Args: litNodes("extra")}},
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
				{Node: Command{Args: litNodes("a")}},
				{Node: Command{Args: litNodes("b")}},
			}}},
		},
	},
	{
		[]string{
			"foo >a >>b <c",
			"foo > a >> b < c",
			"foo>a >>b<c",
		},
		Command{
			Args: []Node{
				litWord("foo"),
				Redirect{Op: RDROUT, Obj: litWord("a")},
				Redirect{Op: APPEND, Obj: litWord("b")},
				Redirect{Op: RDRIN, Obj: litWord("c")},
			},
		},
	},
	{
		[]string{"foo >a bar"},
		Command{
			Args: []Node{
				litWord("foo"),
				Redirect{Op: RDROUT, Obj: litWord("a")},
				litWord("bar"),
			},
		},
	},
	{
		[]string{"foo &", "foo&"},
		Stmt{
			Node:       Command{Args: litNodes("foo")},
			Background: true,
		},
	},
	{
		[]string{"if foo; then bar; fi &"},
		Stmt{
			Node: IfStmt{
				Cond: Stmt{Node: Command{Args: litNodes("foo")}},
				ThenStmts: []Stmt{
					{Node: Command{Args: litNodes("bar")}},
				},
			},
			Background: true,
		},
	},
	{
		[]string{"echo foo#bar"},
		Command{Args: litNodes("echo", "foo#bar")},
	},
	{
		[]string{"echo $(foo bar)"},
		Command{Args: []Node{
			litWord("echo"),
			Word{Parts: []Node{
				CmdSubst{Stmts: []Stmt{
					{Node: Command{Args: litNodes("foo", "bar")}},
				}},
			}},
		}},
	},
	{
		[]string{"echo $(foo | bar)"},
		Command{Args: []Node{
			litWord("echo"),
			Word{Parts: []Node{
				CmdSubst{Stmts: []Stmt{
					{Node: BinaryExpr{
						Op: OR,
						X:  Stmt{Node: Command{Args: litNodes("foo")}},
						Y:  Stmt{Node: Command{Args: litNodes("bar")}},
					}},
				}},
			}},
		}},
	},
	{
		[]string{`echo "$foo"`},
		Command{Args: []Node{
			litWord("echo"),
			Word{Parts: []Node{
				DblQuoted{Parts: []Node{
					ParamExp{Short: true, Text: "foo"},
				}},
			}},
		}},
	},
	{
		[]string{`echo "${foo}"`},
		Command{Args: []Node{
			litWord("echo"),
			Word{Parts: []Node{
				DblQuoted{Parts: []Node{
					ParamExp{Text: "foo"}},
				},
			}},
		}},
	},
	{
		[]string{`echo "$(foo)"`},
		Command{Args: []Node{
			litWord("echo"),
			Word{Parts: []Node{
				DblQuoted{Parts: []Node{
					CmdSubst{Stmts: []Stmt{
						{Node: Command{Args: litNodes("foo")}},
					}},
				}},
			}},
		}},
	},
	{
		[]string{`echo '${foo}'`},
		Command{Args: litNodes("echo", "'${foo}'")},
	},
	{
		[]string{"echo ${foo bar}"},
		Command{Args: []Node{
			litWord("echo"),
			Word{Parts: []Node{
				ParamExp{Text: "foo bar"},
			}},
		}},
	},
	{
		[]string{"echo $(($x-1))"},
		Command{Args: []Node{
			litWord("echo"),
			Word{Parts: []Node{
				ArithmExp{Text: "$x-1"},
			}},
		}},
	},
	{
		[]string{"echo foo$bar"},
		Command{Args: []Node{
			litWord("echo"),
			Word{Parts: []Node{
				Lit{Val: "foo"},
				ParamExp{Short: true, Text: "bar"},
			}},
		}},
	},
	{
		[]string{"echo foo$(bar bar)"},
		Command{Args: []Node{
			litWord("echo"),
			Word{Parts: []Node{
				Lit{Val: "foo"},
				CmdSubst{Stmts: []Stmt{
					{Node: Command{Args: litNodes("bar", "bar")}},
				}},
			}},
		}},
	},
	{
		[]string{"echo foo${bar bar}"},
		Command{Args: []Node{
			litWord("echo"),
			Word{Parts: []Node{
				Lit{Val: "foo"},
				ParamExp{Text: "bar bar"},
			}},
		}},
	},
	{
		[]string{"echo 'foo${bar'"},
		Command{Args: litNodes("echo", "'foo${bar'")},
	},
	{
		[]string{"(foo); bar"},
		[]Node{
			Subshell{Stmts: []Stmt{
				{Node: Command{Args: litNodes("foo")}},
			}},
			Command{Args: litNodes("bar")},
		},
	},
	{
		[]string{"a=\"\nbar\""},
		Command{Args: []Node{
			Word{Parts: []Node{
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
			PatLists: []Node{
				PatternList{
					Patterns: litWords("1"),
					Stmts: []Stmt{
						{Node: Command{Args: litNodes("foo")}},
					},
				},
				PatternList{
					Patterns: litWords("2", "3*"),
					Stmts: []Stmt{
						{Node: Command{Args: litNodes("bar")}},
					},
				},
			},
		},
	},
	{
		[]string{"foo | while read a; do b; done"},
		BinaryExpr{
			Op: OR,
			X:  Stmt{Node: Command{Args: litNodes("foo")}},
			Y: Stmt{Node: WhileStmt{
				Cond: Stmt{Node: Command{Args: litNodes("read", "a")}},
				DoStmts: []Stmt{
					Stmt{Node: Command{Args: litNodes("b")}},
				},
			}},
		},
	},
	{
		[]string{"while read l; do foo || bar; done"},
		WhileStmt{
			Cond: Stmt{Node: Command{Args: litNodes("read", "l")}},
			DoStmts: []Stmt{
				{Node: BinaryExpr{
					Op: LOR,
					X:  Stmt{Node: Command{Args: litNodes("foo")}},
					Y:  Stmt{Node: Command{Args: litNodes("bar")}},
				}},
			},
		},
	},
	{
		[]string{"echo if while"},
		Command{Args: litNodes("echo", "if", "while")},
	},
	{
		[]string{"echo ${foo}if"},
		Command{Args: []Node{
			litWord("echo"),
			Word{Parts: []Node{
				ParamExp{Text: "foo"},
				Lit{Val: "if"},
			}},
		}},
	},
	{
		[]string{"echo $if"},
		Command{Args: []Node{
			litWord("echo"),
			Word{Parts: []Node{
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
