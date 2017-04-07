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

	// current fatal error
	err error
	// current (last) exit code
	exit int

	// TODO: stdin, stderr
	Stdout io.Writer
}

type ExitCode int

func (e ExitCode) Error() string { return fmt.Sprintf("exit code %d", e) }

// Run starts the interpreter and returns any error.
func (r *Runner) Run() error {
	r.node(r.File)
	if r.err == nil && r.exit != 0 {
		r.err = ExitCode(r.exit)
	}
	return r.err
}

func (r *Runner) node(node syntax.Node) {
	if r.err != nil {
		return
	}
	r.exit = 0
	switch x := node.(type) {
	case *syntax.File:
		r.stmts(x.Stmts)
	case *syntax.Stmt:
		r.node(x.Cmd)
	case *syntax.CallExpr:
		prog := r.word(x.Args[0])
		r.call(prog, x.Args[1:])
	case *syntax.IfClause:
		r.stmts(x.CondStmts)
		if r.exit == 0 {
			r.stmts(x.ThenStmts)
		} else {
			r.exit = 0
		}
	default:
		panic(fmt.Sprintf("unhandled node: %T", x))
	}
}

func (r *Runner) stmts(stmts []*syntax.Stmt) {
	for _, stmt := range stmts {
		r.node(stmt)
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
	case "true":
	case "false":
		r.exit = 1
	case "echo":
		for _, arg := range args {
			fmt.Fprint(r.Stdout, r.word(arg))
		}
		fmt.Fprintln(r.Stdout)
	default:
		panic(fmt.Sprintf("unhandled builtin: %s", prog))
	}
}
