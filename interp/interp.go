// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"bytes"
	"fmt"
	"io"

	"github.com/mvdan/sh/syntax"
)

// A Runner interprets shell programs. It cannot be reused once a
// program has been interpreted.
//
// TODO: add context to kill the runner before it's done
type Runner struct {
	// TODO: syntax.Node instead of *syntax.File?
	File *syntax.File

	// TODO: different error types for exit codes and interprer errors
	err error

	// TODO: stdin, stderr
	Stdout io.Writer
}

// Run starts the interpreter and returns any error.
func (r *Runner) Run() error {
	r.node(r.File)
	return r.err
}

func (r *Runner) node(node syntax.Node) {
	if r.err != nil {
		return
	}
	switch x := node.(type) {
	case *syntax.File:
		for _, stmt := range x.Stmts {
			r.node(stmt)
		}
	case *syntax.Stmt:
		r.node(x.Cmd)
	case *syntax.CallExpr:
		prog := r.word(x.Args[0])
		r.call(prog, x.Args[1:])
	default:
		panic(fmt.Sprintf("unhandled node: %T", x))
	}
}

func (r *Runner) word(word *syntax.Word) string {
	var buf bytes.Buffer
	for _, wp := range word.Parts {
		switch x := wp.(type) {
		case *syntax.Lit:
			buf.WriteString(x.Value)
		default:
			panic(fmt.Sprintf("unhandled word part: %T", x))
		}
	}
	return buf.String()
}

func (r *Runner) call(prog string, args []*syntax.Word) {
	switch prog {
	case "echo":
		for _, arg := range args {
			fmt.Fprint(r.Stdout, r.word(arg))
		}
		fmt.Fprintln(r.Stdout)
	default:
		panic(fmt.Sprintf("unhandled builtin: %s", prog))
	}
}
