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
				command{args: []lit{"foo"}},
			}},
		},
		{
			in: "# comment\nfoo",
			want: prog{stmts: []node{
				command{args: []lit{"foo"}},
			}},
		},
		{
			in: "foo arg1 arg2",
			want: prog{stmts: []node{
				command{args: []lit{"foo", "arg1", "arg2"}},
			}},
		},
		{
			in: "( foo; )",
			want: prog{stmts: []node{
				subshell{stmts: []node{
					command{args: []lit{"foo"}},
				}},
			}},
		},
		{
			in: "{ foo; }",
			want: prog{stmts: []node{
				block{stmts: []node{
					command{args: []lit{"foo"}},
				}},
			}},
		},
		{
			in: "if foo; then bar; fi",
			want: prog{stmts: []node{
				ifStmt{
					cond: command{args: []lit{"foo"}},
					thenStmts: []node{
						command{args: []lit{"bar"}},
					},
				},
			}},
		},
		{
			in: "if foo; then bar; else pass; fi",
			want: prog{stmts: []node{
				ifStmt{
					cond: command{args: []lit{"foo"}},
					thenStmts: []node{
						command{args: []lit{"bar"}},
					},
					elseStmts: []node{
						command{args: []lit{"pass"}},
					},
				},
			}},
		},
		{
			in: "if a; then a; elif b; then b; elif c; then c; else d; fi",
			want: prog{stmts: []node{
				ifStmt{
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
					},
				},
			}},
		},
		{
			in: "while foo; do bar; done",
			want: prog{stmts: []node{
				whileStmt{
					cond: command{args: []lit{"foo"}},
					doStmts: []node{
						command{args: []lit{"bar"}},
					},
				},
			}},
		},
		{
			in: "echo ' ' \"foo bar\"",
			want: prog{stmts: []node{
				command{args: []lit{"echo", "' '", "\"foo bar\""}},
			}},
		},
		{
			in: "$a ${b} s{s s=s",
			want: prog{stmts: []node{
				command{args: []lit{"$a", "${b}", "s{s", "s=s"}},
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
			t.Fatalf("AST mismatch in %q\nwant: %s\ngot:  %s\ndumps:\n%#v\n%#v",
				c.in, c.want.String(), got.String(), c.want, got)
		}
	}
}
