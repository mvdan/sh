// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"reflect"
	"strings"
	"testing"
)

func lits(strs ...string) []Node {
	l := make([]Node, 0, len(strs))
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
		[]string{"", " ", "\n"},
		nil,
	},
	{
		[]string{"foo", "foo ", " foo"},
		Command{Args: lits("foo")},
	},
	{
		[]string{"# foo", "# foo\n"},
		Comment{Text: " foo"},
	},
	{
		[]string{"foo; bar", "foo; bar;", "foo;bar;", "\nfoo\nbar\n"},
		[]Node{
			Command{Args: lits("foo")},
			Command{Args: lits("bar")},
		},
	},
	{
		[]string{"foo a b", " foo  a  b ", "foo \\\n a b"},
		Command{Args: lits("foo", "a", "b")},
	},
	{
		[]string{"foobar", "foo\\\nbar"},
		Command{Args: lits("foobar")},
	},
	{
		[]string{"foo'bar'"},
		Command{Args: lits("foo'bar'")},
	},
	{
		[]string{"( foo; )", "(foo;)", "(\nfoo\n)"},
		Subshell{Stmts: []Node{
			Command{Args: lits("foo")},
		}},
	},
	{
		[]string{"{ foo; }", "{foo;}", "{\nfoo\n}"},
		Block{Stmts: []Node{
			Command{Args: lits("foo")},
		}},
	},
	{
		[]string{
			"if a; then b; fi",
			"if a\nthen\nb\nfi",
		},
		IfStmt{
			Cond: Command{Args: lits("a")},
			ThenStmts: []Node{
				Command{Args: lits("b")},
			},
		},
	},
	{
		[]string{
			"if a; then b; else c; fi",
			"if a\nthen b\nelse\nc\nfi",
		},
		IfStmt{
			Cond: Command{Args: lits("a")},
			ThenStmts: []Node{
				Command{Args: lits("b")},
			},
			ElseStmts: []Node{
				Command{Args: lits("c")},
			},
		},
	},
	{
		[]string{
			"if a; then a; elif b; then b; elif c; then c; else d; fi",
			"if a\nthen a\nelif b\nthen b\nelif c\nthen c\nelse\nd\nfi",
		},
		IfStmt{
			Cond: Command{Args: lits("a")},
			ThenStmts: []Node{
				Command{Args: lits("a")},
			},
			Elifs: []Node{
				Elif{Cond: Command{Args: lits("b")},
					ThenStmts: []Node{
						Command{Args: lits("b")},
					}},
				Elif{Cond: Command{Args: lits("c")},
					ThenStmts: []Node{
						Command{Args: lits("c")},
					}},
			},
			ElseStmts: []Node{
				Command{Args: lits("d")},
			},
		},
	},
	{
		[]string{"while a; do b; done", "while a\ndo\nb\ndone"},
		WhileStmt{
			Cond: Command{Args: lits("a")},
			DoStmts: []Node{
				Command{Args: lits("b")},
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
			WordList: lits("1", "2", "3"),
			DoStmts: []Node{
				Command{Args: lits("echo", "$i")},
			},
		},
	},
	{
		[]string{`echo ' ' "foo bar"`},
		Command{Args: lits("echo", "' '", `"foo bar"`)},
	},
	{
		[]string{`"foo \" bar"`},
		Command{Args: lits(`"foo \" bar"`)},
	},
	{
		[]string{`foo \" bar`},
		Command{Args: lits(`foo`, `\"`, `bar`)},
	},
	{
		[]string{"$a s{s s=s"},
		Command{Args: lits("$a", "s{s", "s=s")},
	},
	{
		[]string{"foo && bar", "foo&&bar", "foo &&\nbar"},
		BinaryExpr{
			Op: LAND,
			X:  Command{Args: lits("foo")},
			Y:  Command{Args: lits("bar")},
		},
	},
	{
		[]string{"foo || bar", "foo||bar", "foo ||\nbar"},
		BinaryExpr{
			Op: LOR,
			X:  Command{Args: lits("foo")},
			Y:  Command{Args: lits("bar")},
		},
	},
	{
		[]string{"foo && bar || else"},
		BinaryExpr{
			Op: LAND,
			X:  Command{Args: lits("foo")},
			Y: BinaryExpr{
				Op: LOR,
				X:  Command{Args: lits("bar")},
				Y:  Command{Args: lits("else")},
			},
		},
	},
	{
		[]string{"foo | bar", "foo|bar"},
		BinaryExpr{
			Op: OR,
			X:  Command{Args: lits("foo")},
			Y:  Command{Args: lits("bar")},
		},
	},
	{
		[]string{"foo | bar | extra"},
		BinaryExpr{
			Op: OR,
			X:  Command{Args: lits("foo")},
			Y: BinaryExpr{
				Op: OR,
				X:  Command{Args: lits("bar")},
				Y:  Command{Args: lits("extra")},
			},
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
			Body: Block{Stmts: []Node{
				Command{Args: lits("a")},
				Command{Args: lits("b")},
			}},
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
		[]string{"foo &", "foo&"},
		Command{
			Args:       lits("foo"),
			Background: true,
		},
	},
	{
		[]string{"echo foo#bar"},
		Command{Args: lits("echo", "foo#bar")},
	},
	{
		[]string{"echo `foo bar`"},
		Command{Args: lits("echo", "`foo bar`")},
	},
	{
		[]string{"echo $(foo bar)"},
		Command{Args: []Node{
			litWord("echo"),
			Word{Parts: []Node{
				CmdSubst{Stmts: []Node{
					Command{Args: lits("foo", "bar")},
				}},
			}},
		}},
	},
	{
		[]string{"echo $(foo | bar)"},
		Command{Args: []Node{
			litWord("echo"),
			Word{Parts: []Node{
				CmdSubst{Stmts: []Node{
					BinaryExpr{
						Op: OR,
						X:  Command{Args: lits("foo")},
						Y:  Command{Args: lits("bar")},
					},
				}},
			}},
		}},
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
				Lit{Val: "$bar"},
			}},
		}},
	},
	{
		[]string{"echo foo$(bar bar)"},
		Command{Args: []Node{
			litWord("echo"),
			Word{Parts: []Node{
				Lit{Val: "foo"},
				CmdSubst{Stmts: []Node{
					Command{Args: lits("bar", "bar")},
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
		Command{Args: lits("echo", "'foo${bar'")},
	},
}

func wantedProg(v interface{}) (p Prog) {
	switch x := v.(type) {
	case []Node:
		p.Stmts = x
	case Node:
		p.Stmts = append(p.Stmts, x)
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
