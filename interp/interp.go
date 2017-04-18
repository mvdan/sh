// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path"
	"strconv"
	"strings"
	"syscall"
	"unicode"
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

	// like vars, but local to a cmd i.e. "foo=bar prog args..."
	cmdVars map[string]string

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

type RunError struct {
	syntax.Position
	Text string
}

func (e RunError) Error() string {
	return fmt.Sprintf("%s: %s", e.Position.String(), e.Text)
}

func (r *Runner) runErr(pos syntax.Pos, format string, a ...interface{}) {
	if r.err == nil {
		r.err = RunError{
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

func (r *Runner) lookupVar(name string) (string, bool) {
	switch name {
	case "PWD":
		dir, _ := os.Getwd()
		return dir, true
	case "HOME":
		u, _ := user.Current()
		return u.HomeDir, true
	}
	if val, e := r.cmdVars[name]; e {
		return val, true
	}
	if val, e := r.vars[name]; e {
		return val, true
	}
	return os.LookupEnv(name)
}

func (r *Runner) getVar(name string) string {
	val, _ := r.lookupVar(name)
	return val
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
	r.stmts(r.File.Stmts)
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

func (r *Runner) stmt(st *syntax.Stmt) {
	if st.Background {
		r2 := *r
		r = &r2
		go r.stmtSync(st)
	} else {
		r.stmtSync(st)
	}
}

func (r *Runner) stmtSync(st *syntax.Stmt) {
	oldVars := r.cmdVars
	for _, as := range st.Assigns {
		name, value := as.Name.Value, ""
		if as.Value != nil {
			value = r.loneWord(as.Value)
		}
		if st.Cmd == nil {
			r.setVar(name, value)
			continue
		}
		if r.cmdVars == nil {
			r.cmdVars = make(map[string]string, len(st.Assigns))
		}
		r.cmdVars[name] = value
	}
	oldIn, oldOut, oldErr := r.Stdin, r.Stdout, r.Stderr
	for _, rd := range st.Redirs {
		cls, err := r.redir(rd)
		if err != nil {
			r.exit = 1
			return
		}
		if cls != nil {
			defer cls.Close()
		}
	}
	if st.Cmd == nil {
		r.exit = 0
	} else {
		r.cmd(st.Cmd)
	}
	if st.Negated {
		if r.exit == 0 {
			r.exit = 1
		} else {
			r.exit = 0
		}
	}
	r.cmdVars = oldVars
	r.Stdin, r.Stdout, r.Stderr = oldIn, oldOut, oldErr
}

func (r *Runner) cmd(cm syntax.Command) {
	if r.err != nil {
		return
	}
	switch x := cm.(type) {
	case *syntax.Block:
		r.stmts(x.Stmts)
	case *syntax.Subshell:
		r2 := *r
		r2.stmts(x.Stmts)
		r.exit = r2.exit
	case *syntax.CallExpr:
		fields := r.fields(x.Args)
		r.call(x.Args[0].Pos(), fields[0], fields[1:])
	case *syntax.BinaryCmd:
		switch x.Op {
		case syntax.AndStmt:
			r.stmt(x.X)
			if r.exit == 0 {
				r.stmt(x.Y)
			}
		case syntax.OrStmt:
			r.stmt(x.X)
			if r.exit != 0 {
				r.stmt(x.Y)
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
				r2.stmt(x.X)
				pw.Close()
			}()
			r.stmt(x.Y)
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
		if r.arithm(x.X) == 0 {
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
	case *syntax.TestClause:
		if r.bashTest(x.X) == "" {
			r.exit = 1
		}
	default:
		panic(fmt.Sprintf("unhandled command node: %T", x))
	}
}

func (r *Runner) stmts(stmts []*syntax.Stmt) {
	for _, stmt := range stmts {
		r.stmt(stmt)
	}
}

func (r *Runner) redir(rd *syntax.Redirect) (io.Closer, error) {
	if rd.Hdoc != nil {
		hdoc := r.loneWord(rd.Hdoc)
		r.Stdin = strings.NewReader(hdoc)
		return nil, nil
	}
	orig := &r.Stdout
	if rd.N != nil {
		switch rd.N.Value {
		case "1":
		case "2":
			orig = &r.Stderr
		}
	}
	arg := r.loneWord(rd.Word)
	switch rd.Op {
	case syntax.WordHdoc:
		r.Stdin = strings.NewReader(arg + "\n")
		return nil, nil
	case syntax.DplOut:
		switch arg {
		case "1":
			*orig = r.Stdout
		case "2":
			*orig = r.Stderr
		}
		return nil, nil
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
		// TODO: print to stderr?
		return nil, err
	}
	switch rd.Op {
	case syntax.RdrIn:
		r.Stdin = f
	case syntax.RdrOut, syntax.AppOut:
		*orig = f
	case syntax.RdrAll, syntax.AppAll:
		r.Stdout = f
		r.Stderr = f
	default:
		panic(fmt.Sprintf("unhandled redirect op: %v", rd.Op))
	}
	return f, nil
}

func (r *Runner) loopStmtsBroken(stmts []*syntax.Stmt) bool {
	r.inLoop = true
	defer func() { r.inLoop = false }()
	for _, stmt := range stmts {
		r.stmt(stmt)
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
			val := r.paramExp(x)
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

func (r *Runner) paramExp(pe *syntax.ParamExp) string {
	name := pe.Param.Value
	val := ""
	set := false
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
				val, set = r.args[i], true
			}
		} else {
			val, set = r.lookupVar(name)
		}
	}
	switch {
	case pe.Length:
		val = strconv.Itoa(utf8.RuneCountInString(val))
	case pe.Excl:
		val, set = r.lookupVar(val)
	}
	if pe.Ind != nil {
		panic("unhandled param exp index")
	}
	slicePos := func(expr syntax.ArithmExpr) int {
		p := r.arithm(expr)
		if p < 0 {
			p = len(val) + p
			if p < 0 {
				p = len(val)
			}
		} else if p > len(val) {
			p = len(val)
		}
		return p
	}
	if pe.Slice != nil {
		if pe.Slice.Offset != nil {
			offset := slicePos(pe.Slice.Offset)
			val = val[offset:]
		}
		if pe.Slice.Length != nil {
			length := slicePos(pe.Slice.Length)
			val = val[:length]
		}
	}
	if pe.Repl != nil {
		orig := r.loneWord(pe.Repl.Orig)
		with := r.loneWord(pe.Repl.With)
		n := 1
		if pe.Repl.All {
			n = -1
		}
		val = strings.Replace(val, orig, with, n)
	}
	if pe.Exp != nil {
		arg := r.loneWord(pe.Exp.Word)
		switch pe.Exp.Op {
		case syntax.SubstColPlus:
			if val == "" {
				break
			}
			fallthrough
		case syntax.SubstPlus:
			if set {
				val = arg
			}
		case syntax.SubstMinus:
			if set {
				break
			}
			fallthrough
		case syntax.SubstColMinus:
			if val == "" {
				val = arg
			}
		case syntax.SubstQuest:
			if set {
				break
			}
			fallthrough
		case syntax.SubstColQuest:
			if val == "" {
				r.errf("%s", arg)
				r.exit = 1
				r.lastExit()
			}
		case syntax.SubstAssgn:
			if set {
				break
			}
			fallthrough
		case syntax.SubstColAssgn:
			if val == "" {
				r.setVar(name, arg)
				val = arg
			}
		case syntax.RemSmallPrefix:
			val = removePattern(val, arg, false, false)
		case syntax.RemLargePrefix:
			val = removePattern(val, arg, false, true)
		case syntax.RemSmallSuffix:
			val = removePattern(val, arg, true, false)
		case syntax.RemLargeSuffix:
			val = removePattern(val, arg, true, true)
		case syntax.UpperFirst:
			rs := []rune(val)
			if len(rs) > 0 {
				rs[0] = unicode.ToUpper(rs[0])
			}
			val = string(rs)
		case syntax.UpperAll:
			val = strings.ToUpper(val)
		case syntax.LowerFirst:
			rs := []rune(val)
			if len(rs) > 0 {
				rs[0] = unicode.ToLower(rs[0])
			}
			val = string(rs)
		case syntax.LowerAll:
			val = strings.ToLower(val)
		//case syntax.OtherParamOps:
		default:
			panic(fmt.Sprintf("unhandled param expansion op: %v", pe.Exp.Op))
		}
	}
	return val
}

func removePattern(val, pattern string, fromEnd, longest bool) string {
	// TODO: really slow to not re-implement path.Match.
	last := val
	s := val
	i := len(val)
	if fromEnd {
		i = 0
	}
	for {
		if m, _ := path.Match(pattern, s); m {
			last = val[i:]
			if fromEnd {
				last = val[:i]
			}
			if longest {
				return last
			}
		}
		if fromEnd {
			i++
			if i >= len(val) {
				break
			}
			s = val[i:]
		} else {
			i--
			if i < 1 {
				break
			}
			s = val[:i]
		}
	}
	return last
}

func (r *Runner) call(pos syntax.Pos, name string, args []string) {
	if body := r.funcs[name]; body != nil {
		// stack them to support nested func calls
		oldArgs := r.args
		r.args = args
		r.stmt(body)
		r.args = oldArgs
		return
	}
	if r.builtin(pos, name, args) {
		return
	}
	cmd := exec.Command(name, args...)
	cmd.Env = os.Environ()
	for name, val := range r.cmdVars {
		cmd.Env = append(cmd.Env, name+"="+val)
	}
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
