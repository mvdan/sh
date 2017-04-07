// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"

	"github.com/mvdan/sh/syntax"
)

// A Runner interprets shell programs. It cannot be reused once a
// program has been interpreted.
//
// TODO: add context to kill the runner before it's done
type Runner struct {
	// TODO: syntax.Node instead of *syntax.File?
	File *syntax.File

	vars map[string]string

	err  error // current fatal error
	exit int   // current (last) exit code

	// TODO: stdin, stderr
	Stdout io.Writer
}

type ExitCode uint8

func (e ExitCode) Error() string { return fmt.Sprintf("exit status %d", e) }

type InterpError struct {
	syntax.Position
	Text string
}

func (i InterpError) Error() string {
	return fmt.Sprintf("%s: %s", i.Position.String(), i.Text)
}

func (r *Runner) interpErr(pos syntax.Pos, format string, a ...interface{}) {
	if r.err == nil {
		r.err = InterpError{
			Position: r.File.Position(pos),
			Text:     fmt.Sprintf(format, a...),
		}
	}
}

func (r *Runner) lastExit() {
	if r.err == nil && r.exit != 0 {
		r.err = ExitCode(r.exit)
	}
}

func (r *Runner) setVar(name, val string) {
	if r.vars == nil {
		r.vars = make(map[string]string, 4)
	}
	r.vars[name] = val
}

func (r *Runner) getVar(name string) string {
	// TODO: env vars too
	return r.vars[name]
}

func (r *Runner) delVar(name string) {
	// TODO: env vars too
	delete(r.vars, name)
}

// Run starts the interpreter and returns any error.
func (r *Runner) Run() error {
	r.node(r.File)
	r.lastExit()
	return r.err
}

func (r *Runner) node(node syntax.Node) {
	if r.err != nil {
		return
	}
	switch x := node.(type) {
	case *syntax.File:
		r.stmts(x.Stmts)
	case *syntax.Block:
		r.stmts(x.Stmts)
	case *syntax.Subshell:
		// TODO: child process? encapsulate somehow anyway
		r.stmts(x.Stmts)
	case *syntax.Stmt:
		// TODO: handle background
		// TODO: redirects

		// TODO: assigns only apply to x.Cmd if x.Cmd != nil
		for _, as := range x.Assigns {
			name, value := as.Name.Value, ""
			if as.Value != nil {
				value = r.word(as.Value)
			}
			r.setVar(name, value)
		}
		if x.Cmd == nil {
			r.exit = 0
		} else {
			r.node(x.Cmd)
		}
		if x.Negated {
			if r.exit == 0 {
				r.exit = 1
			} else {
				r.exit = 0
			}
		}
	case *syntax.CallExpr:
		r.call(x.Args[0], x.Args[1:])
	case *syntax.IfClause:
		r.stmts(x.CondStmts)
		if r.exit == 0 {
			r.stmts(x.ThenStmts)
			return
		}
		for _, el := range x.Elifs {
			r.stmts(el.CondStmts)
			if r.exit == 0 {
				r.stmts(el.ThenStmts)
				return
			}
		}
		r.stmts(x.ElseStmts)
		if len(x.Elifs)+len(x.ElseStmts) == 0 {
			r.exit = 0
		}
	case *syntax.WhileClause:
		for r.err == nil {
			r.stmts(x.CondStmts)
			if r.exit != 0 {
				r.exit = 0
				break
			}
			r.stmts(x.DoStmts)
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

func (r *Runner) wordParts(w io.Writer, wps []syntax.WordPart) {
	for _, wp := range wps {
		switch x := wp.(type) {
		case *syntax.Lit:
			io.WriteString(w, x.Value)
		case *syntax.SglQuoted:
			io.WriteString(w, x.Value)
		case *syntax.DblQuoted:
			r.wordParts(w, x.Parts)
		case *syntax.ParamExp:
			name := x.Param.Value
			val := r.getVar(name)
			if x.Length {
				fmt.Fprint(w, utf8.RuneCountInString(val))
			} else {
				io.WriteString(w, val)
			}
		default:
			panic(fmt.Sprintf("unhandled word part: %T", x))
		}
	}
}

func (r *Runner) word(w *syntax.Word) string {
	var buf bytes.Buffer
	r.wordParts(&buf, w.Parts)
	return buf.String()
}

func (r *Runner) call(prog *syntax.Word, args []*syntax.Word) {
	exit := 0
	name := r.word(prog)
	// TODO: builtins can be re-defined as funcs, vars, etc
	switch name {
	case "true", ":":
	case "false":
		exit = 1
	case "exit":
		switch len(args) {
		case 0:
			r.lastExit()
		case 1:
			str := r.word(args[0])
			if n, err := strconv.Atoi(str); err != nil {
				r.interpErr(args[0].Pos(), "invalid exit code: %q", str)
			} else if n != 0 {
				r.err = ExitCode(n)
			}
		default:
			r.interpErr(prog.Pos(), "exit cannot take multiple arguments")
		}
	case "unset":
		for _, arg := range args {
			r.delVar(r.word(arg))
		}
	case "echo":
		for i, arg := range args {
			if i > 0 {
				fmt.Fprint(r.Stdout, " ")
			}
			fmt.Fprint(r.Stdout, r.word(arg))
		}
		fmt.Fprintln(r.Stdout)
	case "printf":
		if len(args) == 0 {
			// TODO: stderr
			fmt.Fprintln(r.Stdout, "usage: printf format [arguments]")
			exit = 1
			break
		}
		format := r.word(args[0])
		var a []interface{}
		for _, arg := range args[1:] {
			a = append(a, r.word(arg))
		}
		fmt.Fprintf(r.Stdout, format, a...)
	default:
		// TODO: default should call binary in $PATH
		panic(fmt.Sprintf("unhandled builtin: %s", name))
	}
	r.exit = exit
}
