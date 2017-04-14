// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/mvdan/sh/syntax"
)

// A Runner interprets shell programs. It cannot be reused once a
// program has been interpreted.
//
// Note that writes to Stdout and Stderr may not be sequential. If
// you plan on using an io.Writer implementation that isn't safe for
// concurrent use, consider a workaround like hiding writes behind a
// mutex.
type Runner struct {
	// TODO: syntax.Node instead of *syntax.File?
	File *syntax.File

	// Separate maps, note that bash allows a name to be both a var
	// and a func simultaneously
	vars  map[string]string
	funcs map[string]*syntax.Stmt

	// Current arguments, if executing a function
	args []string

	// >0 to break or continue out of N enclosing loops
	breakEnclosing, contnEnclosing int

	inLoop bool

	err  error // current fatal error
	exit int   // current (last) exit code

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	// TODO: add context to kill the runner before it's done
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
	if val, e := r.vars[name]; e {
		return val
	}
	return os.Getenv(name)
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

func (r *Runner) fields(words []*syntax.Word) []string {
	fields := make([]string, 0, len(words))
	for _, word := range words {
		fields = append(fields, r.wordParts(word.Parts, false)...)
	}
	return fields
}

func (r *Runner) loneWord(word *syntax.Word) string {
	return strings.Join(r.wordParts(word.Parts, false), "")
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
		r2 := *r
		r2.stmts(x.Stmts)
		r.exit = r2.exit
	case *syntax.Stmt:
		if x.Background {
			r2 := *r
			r = &r2
		}

		// TODO: assigns only apply to x.Cmd if x.Cmd != nil
		for _, as := range x.Assigns {
			name, value := as.Name.Value, ""
			if as.Value != nil {
				value = r.loneWord(as.Value)
			}
			r.setVar(name, value)
		}
		oldIn, oldOut, oldErr := r.Stdin, r.Stdout, r.Stderr
		var closers []io.Closer
		for _, rd := range x.Redirs {
			if closer := r.redir(rd); closer != nil {
				closers = append(closers, closer)
			}
		}
		if x.Background {
			go func() {
				if x.Cmd != nil {
					r.node(x.Cmd)
				}
				for _, closer := range closers {
					closer.Close()
				}
			}()
			break
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
		r.Stdin, r.Stdout, r.Stderr = oldIn, oldOut, oldErr
		for _, closer := range closers {
			closer.Close()
		}
	case *syntax.CallExpr:
		fields := r.fields(x.Args)
		r.call(x.Args[0].Pos(), fields[0], fields[1:])
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
			pr, pw := io.Pipe()
			r2 := Runner{
				File:   r.File,
				Stdin:  r.Stdin,
				Stdout: pw,
			}
			if x.Op == syntax.PipeAll {
				r2.Stderr = pw
			} else {
				r2.Stderr = r.Stderr
			}
			r.Stdin = pr
			go func() {
				r2.node(x.X)
				pw.Close()
			}()
			r.node(x.Y)
			pr.Close()
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
				break
			}
			r.exit = 0
			if r.loopStmtsBroken(x.DoStmts) {
				break
			}
		}
	case *syntax.ForClause:
		switch y := x.Loop.(type) {
		case *syntax.WordIter:
			name := y.Name.Value
			for _, field := range r.fields(y.List) {
				r.setVar(name, field)
				if r.loopStmtsBroken(x.DoStmts) {
					break
				}
			}
		case *syntax.CStyleLoop:
			r.arithm(y.Init)
			for r.arithm(y.Cond) != 0 {
				if r.loopStmtsBroken(x.DoStmts) {
					break
				}
				r.arithm(y.Post)
			}
		}
	case *syntax.FuncDecl:
		r.setFunc(x.Name.Value, x.Body)
	case *syntax.ArithmCmd:
		val := r.arithm(x.X)
		if val == 0 {
			r.exit = 1
		}
	case *syntax.LetClause:
		var val int
		for _, expr := range x.Exprs {
			val = r.arithm(expr)
		}
		if val == 0 {
			r.exit = 1
		}
	case *syntax.CaseClause:
		str := r.loneWord(x.Word)
		for _, pl := range x.List {
			for _, word := range pl.Patterns {
				pat := r.loneWord(word)
				// TODO: error?
				matched, _ := path.Match(pat, str)
				if matched {
					r.stmts(pl.Stmts)
					return
				}
			}
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

func (r *Runner) redir(rd *syntax.Redirect) io.Closer {
	if rd.Hdoc != nil {
		hdoc := r.loneWord(rd.Hdoc)
		r.Stdin = strings.NewReader(hdoc)
		return nil
	}
	arg := r.loneWord(rd.Word)
	switch rd.Op {
	case syntax.WordHdoc:
		r.Stdin = strings.NewReader(arg + "\n")
		return nil
	case syntax.DplOut:
		switch arg {
		case "2":
			r.Stdout = r.Stderr
		}
		return nil
	case syntax.DplIn:
		panic(fmt.Sprintf("unhandled redirect op: %v", rd.Op))
	}
	mode := os.O_RDONLY
	switch rd.Op {
	case syntax.AppOut, syntax.AppAll:
		mode = os.O_RDWR | os.O_CREATE | os.O_APPEND
	case syntax.RdrOut, syntax.RdrAll:
		mode = os.O_RDWR | os.O_CREATE | os.O_TRUNC
	}
	f, err := os.OpenFile(arg, mode, 0644)
	if err != nil {
		// TODO: error
		return nil
	}
	switch rd.Op {
	case syntax.RdrIn:
		r.Stdin = f
	case syntax.RdrOut, syntax.AppOut:
		r.Stdout = f
	case syntax.RdrAll, syntax.AppAll:
		r.Stdout = f
		r.Stderr = f
	default:
		panic(fmt.Sprintf("unhandled redirect op: %v", rd.Op))
	}
	return f
}

func (r *Runner) loopStmtsBroken(stmts []*syntax.Stmt) bool {
	r.inLoop = true
	defer func() { r.inLoop = false }()
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

func (r *Runner) wordParts(wps []syntax.WordPart, quoted bool) []string {
	var parts []string
	var curBuf bytes.Buffer
	flush := func() {
		if curBuf.Len() == 0 {
			return
		}
		parts = append(parts, curBuf.String())
		curBuf.Reset()
	}
	splitAdd := func(val string) {
		// TODO: use IFS
		for i, field := range strings.Fields(val) {
			if i > 0 {
				flush()
			}
			curBuf.WriteString(field)
		}
	}
	for _, wp := range wps {
		switch x := wp.(type) {
		case *syntax.Lit:
			curBuf.WriteString(x.Value)
		case *syntax.SglQuoted:
			curBuf.WriteString(x.Value)
		case *syntax.DblQuoted:
			if len(x.Parts) == 1 {
				pe, ok := x.Parts[0].(*syntax.ParamExp)
				if ok && pe.Param.Value == "@" {
					for i, arg := range r.args {
						if i > 0 {
							flush()
						}
						curBuf.WriteString(arg)
					}
					continue
				}
			}
			for _, str := range r.wordParts(x.Parts, true) {
				curBuf.WriteString(str)
			}
		case *syntax.ParamExp:
			name := x.Param.Value
			val := ""
			switch name {
			case "#":
				val = strconv.Itoa(len(r.args))
			case "*", "@":
				val = strings.Join(r.args, " ")
			case "?":
				val = strconv.Itoa(r.exit)
			default:
				if n, err := strconv.Atoi(name); err == nil {
					if i := n - 1; i < len(r.args) {
						val = r.args[i]
					}
				} else {
					val = r.getVar(name)
				}
			}
			if x.Length {
				val = strconv.Itoa(utf8.RuneCountInString(val))
			}
			if quoted {
				curBuf.WriteString(val)
			} else {
				splitAdd(val)
			}
		case *syntax.CmdSubst:
			oldOut := r.Stdout
			var outBuf bytes.Buffer
			r.Stdout = &outBuf
			r.stmts(x.Stmts)
			r.Stdout = oldOut
			val := strings.TrimRight(outBuf.String(), "\n")
			if quoted {
				curBuf.WriteString(val)
			} else {
				splitAdd(val)
			}
		case *syntax.ArithmExp:
			curBuf.WriteString(strconv.Itoa(r.arithm(x.X)))
		default:
			panic(fmt.Sprintf("unhandled word part: %T", x))
		}
	}
	flush()
	return parts
}

func (r *Runner) call(pos syntax.Pos, name string, args []string) {
	if body := r.funcs[name]; body != nil {
		// stack them to support nested func calls
		oldArgs := r.args
		r.args = args
		r.node(body)
		r.args = oldArgs
		return
	}
	if r.builtin(pos, name, args) {
		return
	}
	cmd := exec.Command(name, args...)
	cmd.Stdin = r.Stdin
	cmd.Stdout = r.Stdout
	cmd.Stderr = r.Stderr
	err := cmd.Run()
	if err == nil {
		r.exit = 0
		return
	}
	switch x := err.(type) {
	case *exec.ExitError:
		// started, but errored - default to 1 if OS
		// doesn't have exit statuses
		if status, ok := x.Sys().(syscall.WaitStatus); ok {
			r.exit = status.ExitStatus()
		} else {
			r.exit = 1
		}
	case *exec.Error:
		// did not start
		// TODO: can this be anything other than
		// "command not found"?
		r.exit = 127
		// TODO: print something?
	}
}
