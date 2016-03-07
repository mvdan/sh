// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"reflect"
	"strings"
	"testing"
)

var tests = []struct {
	in   string
	want []node
}{
	{
		in:   "",
		want: nil,
	},
	{
		in:   "# comment",
		want: nil,
	},
	{
		in: "foo",
		want: []node{
			command{args: []lit{"foo"}},
		},
	},
	{
		in: "# comment\nfoo",
		want: []node{
			command{args: []lit{"foo"}},
		},
	},
	{
		in: "foo arg1 arg2",
		want: []node{
			command{args: []lit{"foo", "arg1", "arg2"}},
		},
	},
	{
		in: "( foo; )",
		want: []node{
			subshell{stmts: []node{
				command{args: []lit{"foo"}},
			}},
		},
	},
	{
		in: "{ foo; }",
		want: []node{
			block{stmts: []node{
				command{args: []lit{"foo"}},
			}},
		},
	},
	{
		in: "if foo; then bar; fi",
		want: []node{ifStmt{
			cond: command{args: []lit{"foo"}},
			thenStmts: []node{
				command{args: []lit{"bar"}},
			}},
		},
	},
	{
		in: "if foo; then bar; else pass; fi",
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
		in: "if a; then a; elif b; then b; elif c; then c; else d; fi",
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
		in: "while foo; do bar; done",
		want: []node{whileStmt{
			cond: command{args: []lit{"foo"}},
			doStmts: []node{
				command{args: []lit{"bar"}},
			}},
		},
	},
	{
		in: "echo ' ' \"foo bar\"",
		want: []node{
			command{args: []lit{"echo", "' '", "\"foo bar\""}},
		},
	},
	{
		in: "$a ${b} s{s s=s",
		want: []node{
			command{args: []lit{"$a", "${b}", "s{s", "s=s"}},
		},
	},
}

func TestParseAST(t *testing.T) {
	for _, c := range tests {
		r := strings.NewReader(c.in)
		got, err := parse(r, "")
		if err != nil {
			t.Fatalf("Unexpected error in %q: %v", c.in, err)
		}
		want := prog{
			stmts: c.want,
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("AST mismatch in %q\nwant: %s\ngot:  %s\ndumps:\n%#v\n%#v",
				c.in, want.String(), got.String(), want, got)
		}
	}
}
