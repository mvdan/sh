// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"reflect"
	"strings"
	"testing"
)

var tests = []struct {
	ins  []string
	want []node
}{
	{
		ins:  []string{"", " ", "\n"},
		want: nil,
	},
	{
		ins:  []string{"# foo", "# foo\n"},
		want: []node{
			comment{text: " foo"},
		},
	},
	{
		ins: []string{"foo", "foo ", " foo"},
		want: []node{
			command{args: []lit{"foo"}},
		},
	},
	{
		ins: []string{"foo; bar", "foo; bar;", "\nfoo\nbar\n"},
		want: []node{
			command{args: []lit{"foo"}},
			command{args: []lit{"bar"}},
		},
	},
	{
		ins: []string{"foo a b", " foo  a  b ", "foo \\\n a b"},
		want: []node{
			command{args: []lit{"foo", "a", "b"}},
		},
	},
	{
		ins: []string{"( foo; )", "(foo;)", "(\nfoo\n)"},
		want: []node{
			subshell{stmts: []node{
				command{args: []lit{"foo"}},
			}},
		},
	},
	{
		ins: []string{"{ foo; }", "{foo;}", "{\nfoo\n}"},
		want: []node{
			block{stmts: []node{
				command{args: []lit{"foo"}},
			}},
		},
	},
	{
		ins: []string{
			"if a; then b; fi",
			"if a\nthen\nb\nfi",
		},
		want: []node{ifStmt{
			cond: command{args: []lit{"a"}},
			thenStmts: []node{
				command{args: []lit{"b"}},
			}},
		},
	},
	{
		ins: []string{
			"if a; then b; else c; fi",
			"if a\nthen b\nelse\nc\nfi",
		},
		want: []node{ifStmt{
			cond: command{args: []lit{"a"}},
			thenStmts: []node{
				command{args: []lit{"b"}},
			},
			elseStmts: []node{
				command{args: []lit{"c"}},
			}},
		},
	},
	{
		ins: []string{
			"if a; then a; elif b; then b; elif c; then c; else d; fi",
			"if a\nthen a\nelif b\nthen b\nelif c\nthen c\nelse\nd\nfi",
		},
		want: []node{ifStmt{
			cond: command{args: []lit{"a"}},
			thenStmts: []node{
				command{args: []lit{"a"}},
			},
			elifs: []node{
				elif{cond: command{args: []lit{"b"}},
					thenStmts: []node{
						command{args: []lit{"b"}},
					}},
				elif{cond: command{args: []lit{"c"}},
					thenStmts: []node{
						command{args: []lit{"c"}},
					}},
			},
			elseStmts: []node{
				command{args: []lit{"d"}},
			}},
		},
	},
	{
		ins: []string{"while a; do b; done", "while a\ndo\nb\ndone"},
		want: []node{whileStmt{
			cond: command{args: []lit{"a"}},
			doStmts: []node{
				command{args: []lit{"b"}},
			}},
		},
	},
	{
		ins: []string{"echo ' ' \"foo bar\""},
		want: []node{
			command{args: []lit{"echo", "' '", "\"foo bar\""}},
		},
	},
	{
		ins: []string{"$a ${b} s{s s=s"},
		want: []node{
			command{args: []lit{"$a", "${b}", "s{s", "s=s"}},
		},
	},
	{
		ins: []string{"foo && bar", "foo&&bar"},
		want: []node{binaryExpr{
			op: "&&",
			X:  command{args: []lit{"foo"}},
			Y:  command{args: []lit{"bar"}},
		}},
	},
	{
		ins: []string{"foo || bar", "foo||bar"},
		want: []node{binaryExpr{
			op: "||",
			X:  command{args: []lit{"foo"}},
			Y:  command{args: []lit{"bar"}},
		}},
	},
	{
		ins: []string{"foo && bar || else"},
		want: []node{binaryExpr{
			op: "&&",
			X:  command{args: []lit{"foo"}},
			Y: binaryExpr{
				op: "||",
				X:  command{args: []lit{"bar"}},
				Y:  command{args: []lit{"else"}},
			},
		}},
	},
}

func TestParseAST(t *testing.T) {
	for _, c := range tests {
		want := prog{
			stmts: c.want,
		}
		for _, in := range c.ins {
			r := strings.NewReader(in)
			got, err := parse(r, "")
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
