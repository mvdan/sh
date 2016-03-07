// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseAST(t *testing.T) {
	tests := []struct {
		in   string
		want prog
	}{
		{
			in:   "",
			want: prog{},
		},
		{
			in:   "# comment",
			want: prog{},
		},
		{
			in: "foo",
			want: prog{stmts: []node{
				command{args: []string{"foo"}},
			}},
		},
		{
			in: "# comment\nfoo",
			want: prog{stmts: []node{
				command{args: []string{"foo"}},
			}},
		},
		{
			in: "foo arg1 arg2",
			want: prog{stmts: []node{
				command{args: []string{"foo", "arg1", "arg2"}},
			}},
		},
		{
			in: "( foo; )",
			want: prog{stmts: []node{
				subshell{stmts: []node{
					command{args: []string{"foo"}},
				}},
			}},
		},
		{
			in: "{ foo; }",
			want: prog{stmts: []node{
				block{stmts: []node{
					command{args: []string{"foo"}},
				}},
			}},
		},
		{
			in: "if foo; then bar; fi",
			want: prog{stmts: []node{
				ifStmt{
					cond: command{args: []string{"foo"}},
					thenStmts: []node{
						command{args: []string{"bar"}},
					},
				},
			}},
		},
		{
			in: "if foo; then bar; else pass; fi",
			want: prog{stmts: []node{
				ifStmt{
					cond: command{args: []string{"foo"}},
					thenStmts: []node{
						command{args: []string{"bar"}},
					},
					elseStmts: []node{
						command{args: []string{"pass"}},
					},
				},
			}},
		},
		{
			in: "while foo; do bar; done",
			want: prog{stmts: []node{
				whileStmt{
					cond: command{args: []string{"foo"}},
					doStmts: []node{
						command{args: []string{"bar"}},
					},
				},
			}},
		},
		{
			in: "echo ' ' \"foo bar\"",
			want: prog{stmts: []node{
				command{args: []string{"echo", "' '", "\"foo bar\""}},
			}},
		},
		{
			in: "echo $foo ${bar} str{ing",
			want: prog{stmts: []node{
				command{args: []string{"echo", "$foo", "${bar}", "str{ing"}},
			}},
		},
	}
	for _, c := range tests {
		r := strings.NewReader(c.in)
		got, err := parse(r, "")
		if err != nil {
			t.Fatalf("Unexpected error in %q: %v", c.in, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Fatalf("AST mismatch in %q\nwant: %#v\ngot:  %#v",
				c.in, c.want, got)
		}
	}
}
