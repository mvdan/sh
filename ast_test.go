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
		l = append(l, Lit{Val: s})
	}
	return l
}

var tests = []struct {
	ins  []string
	want interface{}
}{
	{
		ins:  []string{"", " ", "\n"},
		want: nil,
	},
	{
		ins:  []string{"foo", "foo ", " foo"},
		want: Command{Args: lits("foo")},
	},
	{
		ins:  []string{"# foo", "# foo\n"},
		want: Comment{Text: " foo"},
	},
	{
		ins: []string{"foo; bar", "foo; bar;", "foo;bar;", "\nfoo\nbar\n"},
		want: []Node{
			Command{Args: lits("foo")},
			Command{Args: lits("bar")},
		},
	},
	{
		ins:  []string{"foo a b", " foo  a  b ", "foo \\\n a b"},
		want: Command{Args: lits("foo", "a", "b")},
	},
	{
		ins:  []string{"foo'bar'"},
		want: Command{Args: lits("foo'bar'")},
	},
	{
		ins: []string{"( foo; )", "(foo;)", "(\nfoo\n)"},
		want: Subshell{Stmts: []Node{
			Command{Args: lits("foo")},
		}},
	},
	{
		ins: []string{"{ foo; }", "{foo;}", "{\nfoo\n}"},
		want: Block{Stmts: []Node{
			Command{Args: lits("foo")},
		}},
	},
	{
		ins: []string{
			"if a; then b; fi",
			"if a\nthen\nb\nfi",
		},
		want: IfStmt{
			Cond: Command{Args: lits("a")},
			ThenStmts: []Node{
				Command{Args: lits("b")},
			},
		},
	},
	{
		ins: []string{
			"if a; then b; else c; fi",
			"if a\nthen b\nelse\nc\nfi",
		},
		want: IfStmt{
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
		ins: []string{
			"if a; then a; elif b; then b; elif c; then c; else d; fi",
			"if a\nthen a\nelif b\nthen b\nelif c\nthen c\nelse\nd\nfi",
		},
		want: IfStmt{
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
		ins: []string{"while a; do b; done", "while a\ndo\nb\ndone"},
		want: WhileStmt{
			Cond: Command{Args: lits("a")},
			DoStmts: []Node{
				Command{Args: lits("b")},
			},
		},
	},
	{
		ins:  []string{`echo ' ' "foo bar"`},
		want: Command{Args: lits("echo", "' '", `"foo bar"`)},
	},
	{
		ins:  []string{`"foo \" bar"`},
		want: Command{Args: lits(`"foo \" bar"`)},
	},
	{
		ins:  []string{"$a ${b} s{s s=s"},
		want: Command{Args: lits("$a", "${b}", "s{s", "s=s")},
	},
	{
		ins: []string{"foo && bar", "foo&&bar", "foo &&\nbar"},
		want: BinaryExpr{
			Op: LAND,
			X:  Command{Args: lits("foo")},
			Y:  Command{Args: lits("bar")},
		},
	},
	{
		ins: []string{"foo || bar", "foo||bar", "foo ||\nbar"},
		want: BinaryExpr{
			Op: LOR,
			X:  Command{Args: lits("foo")},
			Y:  Command{Args: lits("bar")},
		},
	},
	{
		ins: []string{"foo && bar || else"},
		want: BinaryExpr{
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
		ins: []string{"foo | bar", "foo|bar"},
		want: BinaryExpr{
			Op: OR,
			X:  Command{Args: lits("foo")},
			Y:  Command{Args: lits("bar")},
		},
	},
	{
		ins: []string{"foo | bar | extra"},
		want: BinaryExpr{
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
		ins: []string{
			"foo() { a; b; }",
			"foo() {\na\nb\n}",
			"foo ( ) {\na\nb\n}",
		},
		want: FuncDecl{
			Name: Lit{Val: "foo"},
			Body: Block{Stmts: []Node{
				Command{Args: lits("a")},
				Command{Args: lits("b")},
			}},
		},
	},
	{
		ins: []string{
			"foo >a >>b <c",
			"foo > a >> b < c",
			"foo>a >>b<c",
		},
		want: Command{
			Args: []Node{
				Lit{Val: "foo"},
				Redirect{Op: GTR, Obj: Lit{Val: "a"}},
				Redirect{Op: SHR, Obj: Lit{Val: "b"}},
				Redirect{Op: LSS, Obj: Lit{Val: "c"}},
			},
		},
	},
	{
		ins: []string{"foo &", "foo&"},
		want: Command{
			Args:       lits("foo"),
			Background: true,
		},
	},
	{
		ins:  []string{"echo foo#bar"},
		want: Command{Args: lits("echo", "foo#bar")},
	},
	{
		ins:  []string{"echo `foo bar`"},
		want: Command{Args: lits("echo", "`foo bar`")},
	},
	{
		ins:  []string{"echo $(foo bar)"},
		want: Command{Args: lits("echo", "$(foo bar)")},
	},
	{
		ins:  []string{"echo ${foo bar}"},
		want: Command{Args: lits("echo", "${foo bar}")},
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
