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
		ins:  []string{"#a", "# a b", "#a\n#b"},
		want: nil,
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
		ins: []string{"# comment\nfoo"},
		want: []node{
			command{args: []lit{"foo"}},
		},
	},
	{
		ins: []string{"foo arg1 arg2"},
		want: []node{
			command{args: []lit{"foo", "arg1", "arg2"}},
		},
	},
	{
		ins: []string{"( foo; )"},
		want: []node{
			subshell{stmts: []node{
				command{args: []lit{"foo"}},
			}},
		},
	},
	{
		ins: []string{"{ foo; }"},
		want: []node{
			block{stmts: []node{
				command{args: []lit{"foo"}},
			}},
		},
	},
	{
		ins: []string{"if foo; then bar; fi"},
		want: []node{ifStmt{
			cond: command{args: []lit{"foo"}},
			thenStmts: []node{
				command{args: []lit{"bar"}},
			}},
		},
	},
	{
		ins: []string{"if foo; then bar; else pass; fi"},
		want: []node{ifStmt{
			cond: command{args: []lit{"foo"}},
			thenStmts: []node{
				command{args: []lit{"bar"}},
			},
			elseStmts: []node{
				command{args: []lit{"pass"}},
			}},
		},
	},
	{
		ins: []string{"if a; then a; elif b; then b; elif c; then c; else d; fi"},
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
		ins: []string{"while foo; do bar; done"},
		want: []node{whileStmt{
			cond: command{args: []lit{"foo"}},
			doStmts: []node{
				command{args: []lit{"bar"}},
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
		ins: []string{"foo && bar"},
		want: []node{binaryExpr{
			op: "&&",
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
