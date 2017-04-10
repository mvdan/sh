// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"syscall"
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

	// Separate maps, note that bash allows a name to be both a var
	// and a func simultaneously
	vars  map[string]string
	funcs map[string]*syntax.Stmt

	// Current parameters, if executing a function
	params []string

	// >0 to break or continue out of N enclosing loops
	breakEnclosing, contnEnclosing int

	err  error // current fatal error
	exit int   // current (last) exit code

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
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
	if r.err == nil {
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

func (r *Runner) setFunc(name string, body *syntax.Stmt) {
	if r.funcs == nil {
		r.funcs = make(map[string]*syntax.Stmt, 4)
	}
	r.funcs[name] = body
}

// Run starts the interpreter and returns any error.
func (r *Runner) Run() error {
	r.node(r.File)
	r.lastExit()
	if r.err == ExitCode(0) {
		r.err = nil
	}
	return r.err
}

func (r *Runner) outf(format string, a ...interface{}) {
	fmt.Fprintf(r.Stdout, format, a...)
}

func (r *Runner) errf(format string, a ...interface{}) {
	fmt.Fprintf(r.Stderr, format, a...)
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
	case *syntax.BinaryCmd:
		switch x.Op {
		case syntax.AndStmt:
			r.node(x.X)
			if r.exit == 0 {
				r.node(x.Y)
			}
		case syntax.OrStmt:
			r.node(x.X)
			if r.exit != 0 {
				r.node(x.Y)
			}
		case syntax.Pipe, syntax.PipeAll:
			panic(fmt.Sprintf("unhandled binary cmd op: %v", x.Op))
		}
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
			if r.loopStmtsBroken(x.DoStmts) {
				break
			}
		}
	case *syntax.UntilClause:
		for r.err == nil {
			r.stmts(x.CondStmts)
			if r.exit == 0 {
				r.exit = 0
				break
			}
			if r.loopStmtsBroken(x.DoStmts) {
				break
			}
		}
	case *syntax.ForClause:
		switch y := x.Loop.(type) {
		case *syntax.WordIter:
			name := y.Name.Value
			for _, word := range y.List {
				r.setVar(name, r.word(word))
				if r.loopStmtsBroken(x.DoStmts) {
					break
				}
			}
		case *syntax.CStyleLoop:
			panic(fmt.Sprintf("unhandled loop: %T", y))
		}
	case *syntax.FuncDecl:
		r.setFunc(x.Name.Value, x.Body)
	default:
		panic(fmt.Sprintf("unhandled node: %T", x))
	}
}

func (r *Runner) stmts(stmts []*syntax.Stmt) {
	for _, stmt := range stmts {
		r.node(stmt)
	}
}

func (r *Runner) loopStmtsBroken(stmts []*syntax.Stmt) bool {
	for _, stmt := range stmts {
		r.node(stmt)
		if r.contnEnclosing > 0 {
			r.contnEnclosing--
			return false
		}
		if r.breakEnclosing > 0 {
			r.breakEnclosing--
			return true
		}
	}
	return false
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
			val := ""
			switch name {
			case "#":
				val = strconv.Itoa(len(r.params))
			case "?":
				val = strconv.Itoa(r.exit)
			default:
				if n, err := strconv.Atoi(name); err == nil {
					if i := n - 1; i < len(r.params) {
						val = r.params[i]
					}
				} else {
					val = r.getVar(name)
				}
			}
			if x.Length {
				r.outf("%d", utf8.RuneCountInString(val))
			} else {
				io.WriteString(w, val)
			}
		case *syntax.CmdSubst:
			oldOut := r.Stdout
			r.Stdout = w
			r.stmts(x.Stmts)
			r.Stdout = oldOut
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
	name := r.word(prog)
	if body := r.funcs[name]; body != nil {
		// stack them to support nested func calls
		oldParams := r.params
		r.params = make([]string, len(args))
		for i, word := range args {
			r.params[i] = r.word(word)
		}
		r.node(body)
		r.params = oldParams
		return
	}
	exit := 0
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
				r.outf(" ")
			}
			r.outf("%s", r.word(arg))
		}
		r.outf("\n")
	case "printf":
		if len(args) == 0 {
			r.errf("usage: printf format [arguments]\n")
			exit = 2
			break
		}
		format := r.word(args[0])
		var a []interface{}
		for _, arg := range args[1:] {
			a = append(a, r.word(arg))
		}
		r.outf(format, a...)
	case "break":
		switch len(args) {
		case 0:
			r.breakEnclosing = 1
		case 1:
			str := r.word(args[0])
			if n, err := strconv.Atoi(str); err == nil {
				r.breakEnclosing = n
				break
			}
			fallthrough
		default:
			r.errf("usage: break [n]\n")
			exit = 2
			break
		}
	case "continue":
		switch len(args) {
		case 0:
			r.contnEnclosing = 1
		case 1:
			str := r.word(args[0])
			if n, err := strconv.Atoi(str); err == nil {
				r.contnEnclosing = n
				break
			}
			fallthrough
		default:
			r.errf("usage: continue [n]")
			exit = 2
			break
		}
	default:
		strs := make([]string, len(args))
		for i, word := range args {
			strs[i] = r.word(word)
		}
		cmd := exec.Command(name, strs...)
		cmd.Stdin = r.Stdin
		cmd.Stdout = r.Stdout
		cmd.Stderr = r.Stderr
		err := cmd.Run()
		if err == nil {
			break
		}
		switch x := err.(type) {
		case *exec.ExitError:
			// started, but errored - default to 1 if OS
			// doesn't have exit statuses
			exit = 1
			if status, ok := x.Sys().(syscall.WaitStatus); ok {
				exit = status.ExitStatus()
			}
		case *exec.Error:
			// did not start
			// TODO: can this be anything other than
			// "command not found"?
			exit = 127
			// TODO: print something?
		}
	}
	r.exit = exit
}
