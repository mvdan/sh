// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/xerrors"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/pattern"
	"mvdan.cc/sh/v3/syntax"
)

// RunnerOption is a function which can be passed to New to alter Runner behaviour.
// To apply option to existing Runner call it directly,
// for example interp.Params("-e")(runner).
type RunnerOption func(*Runner) error

// New creates a new Runner, applying a number of options. If applying any of
// the options results in an error, it is returned.
//
// Any unset options fall back to their defaults. For example, not supplying the
// environment falls back to the process's environment, and not supplying the
// standard output writer means that the output will be discarded.
func New(opts ...RunnerOption) (*Runner, error) {
	r := &Runner{
		usedNew:     true,
		execHandler: DefaultExecHandler(2 * time.Second),
		openHandler: DefaultOpenHandler(),
	}
	r.dirStack = r.dirBootstrap[:0]
	for _, opt := range opts {
		if err := opt(r); err != nil {
			return nil, err
		}
	}
	// Set the default fallbacks, if necessary.
	if r.Env == nil {
		Env(nil)(r)
	}
	if r.Dir == "" {
		if err := Dir("")(r); err != nil {
			return nil, err
		}
	}
	if r.stdout == nil || r.stderr == nil {
		StdIO(r.stdin, r.stdout, r.stderr)(r)
	}
	return r, nil
}

func (r *Runner) fillExpandConfig(ctx context.Context) {
	r.ectx = ctx
	r.ecfg = &expand.Config{
		Env: expandEnv{r},
		CmdSubst: func(w io.Writer, cs *syntax.CmdSubst) error {
			switch len(cs.Stmts) {
			case 0: // nothing to do
				return nil
			case 1: // $(<file)
				word := catShortcutArg(cs.Stmts[0])
				if word == nil {
					break
				}
				path := r.literal(word)
				f, err := r.open(ctx, path, os.O_RDONLY, 0, true)
				if err != nil {
					return err
				}
				_, err = io.Copy(w, f)
				return err
			}
			r2 := r.sub()
			r2.stdout = w
			r2.stmts(ctx, cs.Stmts)
			return r2.err
		},
	}
	r.updateExpandOpts()
}

// catShortcutArg checks if a statement is of the form "$(<file)". The redirect
// word is returned if there's a match, and nil otherwise.
func catShortcutArg(stmt *syntax.Stmt) *syntax.Word {
	if stmt.Cmd != nil || stmt.Negated || stmt.Background || stmt.Coprocess {
		return nil
	}
	if len(stmt.Redirs) != 1 {
		return nil
	}
	redir := stmt.Redirs[0]
	if redir.Op != syntax.RdrIn {
		return nil
	}
	return redir.Word
}

func (r *Runner) updateExpandOpts() {
	if r.opts[optNoGlob] {
		r.ecfg.ReadDir = nil
	} else {
		r.ecfg.ReadDir = ioutil.ReadDir
	}
	r.ecfg.GlobStar = r.opts[optGlobStar]
}

func (r *Runner) expandErr(err error) {
	if err != nil {
		r.errf("%v\n", err)
		r.exit = 1
		r.exitShell = true
	}
}

func (r *Runner) arithm(expr syntax.ArithmExpr) int {
	n, err := expand.Arithm(r.ecfg, expr)
	r.expandErr(err)
	return n
}

func (r *Runner) fields(words ...*syntax.Word) []string {
	strs, err := expand.Fields(r.ecfg, words...)
	r.expandErr(err)
	return strs
}

func (r *Runner) literal(word *syntax.Word) string {
	str, err := expand.Literal(r.ecfg, word)
	r.expandErr(err)
	return str
}

func (r *Runner) document(word *syntax.Word) string {
	str, err := expand.Document(r.ecfg, word)
	r.expandErr(err)
	return str
}

func (r *Runner) pattern(word *syntax.Word) string {
	str, err := expand.Pattern(r.ecfg, word)
	r.expandErr(err)
	return str
}

// expandEnv exposes Runner's variables to the expand package.
type expandEnv struct {
	r *Runner
}

var _ expand.WriteEnviron = expandEnv{}

func (e expandEnv) Get(name string) expand.Variable {
	return e.r.lookupVar(name)
}

func (e expandEnv) Set(name string, vr expand.Variable) error {
	e.r.setVarInternal(name, vr)
	return nil // TODO: return any errors
}

func (e expandEnv) Each(fn func(name string, vr expand.Variable) bool) {
	e.r.Env.Each(fn)
	for name, vr := range e.r.Vars {
		if !fn(name, vr) {
			return
		}
	}
}

// Env sets the interpreter's environment. If nil, a copy of the current
// process's environment is used.
func Env(env expand.Environ) RunnerOption {
	return func(r *Runner) error {
		if env == nil {
			env = expand.ListEnviron(os.Environ()...)
		}
		r.Env = env
		return nil
	}
}

// Dir sets the interpreter's working directory. If empty, the process's current
// directory is used.
func Dir(path string) RunnerOption {
	return func(r *Runner) error {
		if path == "" {
			path, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("could not get current dir: %v", err)
			}
			r.Dir = path
			return nil
		}
		path, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("could not get absolute dir: %v", err)
		}
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("could not stat: %v", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", path)
		}
		r.Dir = path
		return nil
	}
}

// Params populates the shell options and parameters. For example, Params("-e",
// "--", "foo") will set the "-e" option and the parameters ["foo"], and
// Params("+e") will unset the "-e" option and leave the parameters untouched.
//
// This is similar to what the interpreter's "set" builtin does.
func Params(args ...string) RunnerOption {
	return func(r *Runner) error {
		onlyFlags := true
		for len(args) > 0 {
			arg := args[0]
			if arg == "" || (arg[0] != '-' && arg[0] != '+') {
				onlyFlags = false
				break
			}
			if arg == "--" {
				onlyFlags = false
				args = args[1:]
				break
			}
			enable := arg[0] == '-'
			var opt *bool
			if flag := arg[1:]; flag == "o" {
				args = args[1:]
				if len(args) == 0 && enable {
					for i, opt := range &shellOptsTable {
						r.printOptLine(opt.name, r.opts[i])
					}
					break
				}
				if len(args) == 0 && !enable {
					for i, opt := range &shellOptsTable {
						setFlag := "+o"
						if r.opts[i] {
							setFlag = "-o"
						}
						r.outf("set %s %s\n", setFlag, opt.name)
					}
					break
				}
				opt = r.optByName(args[0], false)
			} else {
				opt = r.optByFlag(flag)
			}
			if opt == nil {
				return fmt.Errorf("invalid option: %q", arg)
			}
			*opt = enable
			args = args[1:]
		}
		if !onlyFlags {
			// If "--" wasn't given and there were zero arguments,
			// we don't want to override the current parameters.
			r.Params = args
		}
		return nil
	}
}

// ExecHandler sets command execution handler. See ExecHandlerFunc for more info.
func ExecHandler(f ExecHandlerFunc) RunnerOption {
	return func(r *Runner) error {
		r.execHandler = f
		return nil
	}
}

// OpenHandler sets file open handler. See OpenHandlerFunc for more info.
func OpenHandler(f OpenHandlerFunc) RunnerOption {
	return func(r *Runner) error {
		r.openHandler = f
		return nil
	}
}

// StdIO configures an interpreter's standard input, standard output, and
// standard error. If out or err are nil, they default to a writer that discards
// the output.
func StdIO(in io.Reader, out, err io.Writer) RunnerOption {
	return func(r *Runner) error {
		r.stdin = in
		if out == nil {
			out = ioutil.Discard
		}
		r.stdout = out
		if err == nil {
			err = ioutil.Discard
		}
		r.stderr = err
		return nil
	}
}

// A Runner interprets shell programs. It can be reused, but it is not safe for
// concurrent use. You should typically use New to build a new Runner.
//
// Note that writes to Stdout and Stderr may be concurrent if background
// commands are used. If you plan on using an io.Writer implementation that
// isn't safe for concurrent use, consider a workaround like hiding writes
// behind a mutex.
//
// To create a Runner, use New. Runner's exported fields are meant to be
// configured via runner options; once a Runner has been created, the fields
// should be treated as read-only.
type Runner struct {
	// Env specifies the environment of the interpreter, which must be
	// non-nil.
	Env expand.Environ

	// Dir specifies the working directory of the command, which must be an
	// absolute path.
	Dir string

	// Params are the current shell parameters, e.g. from running a shell
	// file or calling a function. Accessible via the $@/$* family of vars.
	Params []string

	// Separate maps - note that bash allows a name to be both a var and a
	// func simultaneously

	Vars  map[string]expand.Variable
	Funcs map[string]*syntax.Stmt

	// execHandler is a function responsible for executing programs. It must be non-nil.
	execHandler ExecHandlerFunc

	// openHandler is a function responsible for opening files. It must be non-nil.
	openHandler OpenHandlerFunc

	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer

	ecfg *expand.Config
	ectx context.Context // just so that Runner.Sub can use it again

	// didReset remembers whether the runner has ever been reset. This is
	// used so that Reset is automatically called when running any program
	// or node for the first time on a Runner.
	didReset bool

	usedNew bool

	filename string // only if Node was a File

	// like Vars, but local to a func i.e. "local foo=bar"
	funcVars map[string]expand.Variable

	// like Vars, but local to a cmd i.e. "foo=bar prog args..."
	cmdVars map[string]string

	// >0 to break or continue out of N enclosing loops
	breakEnclosing, contnEnclosing int

	inLoop    bool
	inFunc    bool
	inSource  bool
	noErrExit bool

	err       error // current shell exit code or fatal error
	exit      int   // current (last) exit status code
	exitShell bool  // whether the shell needs to exit

	bgShells errgroup.Group

	opts runnerOpts

	origDir    string
	origParams []string
	origOpts   runnerOpts
	origStdin  io.Reader
	origStdout io.Writer
	origStderr io.Writer

	// Most scripts don't use pushd/popd, so make space for the initial PWD
	// without requiring an extra allocation.
	dirStack     []string
	dirBootstrap [1]string

	optState getopts

	// keepRedirs is used so that "exec" can make any redirections
	// apply to the current shell, and not just the command.
	keepRedirs bool

	// So that we can get io.Copy to reuse the same buffer within a runner.
	// For example, this saves an allocation for every shell pipe, since
	// io.PipeReader does not implement io.WriterTo.
	bufCopier bufCopier
}

type bufCopier struct {
	io.Reader
	buf []byte
}

func (r *bufCopier) WriteTo(w io.Writer) (n int64, err error) {
	if r.buf == nil {
		r.buf = make([]byte, 32*1024)
	}
	return io.CopyBuffer(w, r.Reader, r.buf)
}

func (r *Runner) optByFlag(flag string) *bool {
	for i, opt := range &shellOptsTable {
		if opt.flag == flag {
			return &r.opts[i]
		}
	}
	return nil
}

func (r *Runner) optByName(name string, bash bool) *bool {
	if bash {
		for i, optName := range bashOptsTable {
			if optName == name {
				return &r.opts[len(shellOptsTable)+i]
			}
		}
	}
	for i, opt := range &shellOptsTable {
		if opt.name == name {
			return &r.opts[i]
		}
	}
	return nil
}

type runnerOpts [len(shellOptsTable) + len(bashOptsTable)]bool

var shellOptsTable = [...]struct {
	flag, name string
}{
	// sorted alphabetically by name; use a space for the options
	// that have no flag form
	{"a", "allexport"},
	{"e", "errexit"},
	{"n", "noexec"},
	{"f", "noglob"},
	{"u", "nounset"},
	{" ", "pipefail"},
}

var bashOptsTable = [...]string{
	// sorted alphabetically by name
	"globstar",
}

// To access the shell options arrays without a linear search when we
// know which option we're after at compile time. First come the shell options,
// then the bash options.
const (
	optAllExport = iota
	optErrExit
	optNoExec
	optNoGlob
	optNoUnset
	optPipeFail

	optGlobStar
)

// Reset returns a runner to its initial state, right before the first call to
// Run or Reset.
//
// Typically, this function only needs to be called if a runner is reused to run
// multiple programs non-incrementally. Not calling Reset between each run will
// mean that the shell state will be kept, including variables, options, and the
// current directory.
func (r *Runner) Reset() {
	if !r.usedNew {
		panic("use interp.New to construct a Runner")
	}
	if !r.didReset {
		r.origDir = r.Dir
		r.origParams = r.Params
		r.origOpts = r.opts
		r.origStdin = r.stdin
		r.origStdout = r.stdout
		r.origStderr = r.stderr
	}
	// reset the internal state
	*r = Runner{
		Env:         r.Env,
		execHandler: r.execHandler,
		openHandler: r.openHandler,

		// These can be set by functions like Dir or Params, but
		// builtins can overwrite them; reset the fields to whatever the
		// constructor set up.
		Dir:    r.origDir,
		Params: r.origParams,
		opts:   r.origOpts,
		stdin:  r.origStdin,
		stdout: r.origStdout,
		stderr: r.origStderr,

		origDir:    r.origDir,
		origParams: r.origParams,
		origOpts:   r.origOpts,
		origStdin:  r.origStdin,
		origStdout: r.origStdout,
		origStderr: r.origStderr,

		// emptied below, to reuse the space
		Vars:      r.Vars,
		cmdVars:   r.cmdVars,
		dirStack:  r.dirStack[:0],
		usedNew:   r.usedNew,
		bufCopier: r.bufCopier,
	}
	if r.Vars == nil {
		r.Vars = make(map[string]expand.Variable)
	} else {
		for k := range r.Vars {
			delete(r.Vars, k)
		}
	}
	if r.cmdVars == nil {
		r.cmdVars = make(map[string]string)
	} else {
		for k := range r.cmdVars {
			delete(r.cmdVars, k)
		}
	}
	if vr := r.Env.Get("HOME"); !vr.IsSet() {
		home, _ := os.UserHomeDir()
		r.Vars["HOME"] = expand.Variable{Kind: expand.String, Str: home}
	}
	r.Vars["UID"] = expand.Variable{
		Kind:     expand.String,
		ReadOnly: true,
		Str:      strconv.Itoa(os.Getuid()),
	}
	r.Vars["PWD"] = expand.Variable{Kind: expand.String, Str: r.Dir}
	r.Vars["IFS"] = expand.Variable{Kind: expand.String, Str: " \t\n"}
	r.Vars["OPTIND"] = expand.Variable{Kind: expand.String, Str: "1"}

	if runtime.GOOS == "windows" {
		// convert $PATH to a unix path list
		path := r.Env.Get("PATH").String()
		path = strings.Join(filepath.SplitList(path), ":")
		r.Vars["PATH"] = expand.Variable{Kind: expand.String, Str: path}
	}

	r.dirStack = append(r.dirStack, r.Dir)
	r.didReset = true
	r.bufCopier.Reader = nil
}

func (r *Runner) handlerCtx(ctx context.Context) context.Context {
	hc := HandlerContext{
		Dir:    r.Dir,
		Stdin:  r.stdin,
		Stdout: r.stdout,
		Stderr: r.stderr,
	}
	oenv := overlayEnviron{
		parent: r.Env,
		values: make(map[string]expand.Variable),
	}
	for name, vr := range r.Vars {
		oenv.Set(name, vr)
	}
	for name, vr := range r.funcVars {
		oenv.Set(name, vr)
	}
	for name, value := range r.cmdVars {
		oenv.Set(name, expand.Variable{Exported: true, Kind: expand.String, Str: value})
	}
	hc.Env = oenv
	return context.WithValue(ctx, handlerCtxKey{}, hc)
}

// exitStatus is a non-zero status code resulting from running a shell node.
type exitStatus uint8

func (s exitStatus) Error() string { return fmt.Sprintf("exit status %d", s) }

// NewExitStatus creates an error which contains the specified exit status code.
func NewExitStatus(status uint8) error {
	return exitStatus(status)
}

// IsExitStatus checks whether error contains an exit status and returns it.
func IsExitStatus(err error) (status uint8, ok bool) {
	var s exitStatus
	if xerrors.As(err, &s) {
		return uint8(s), true
	}
	return 0, false
}

func (r *Runner) setErr(err error) {
	if r.err == nil {
		r.err = err
	}
}

// Run interprets a node, which can be a *File, *Stmt, or Command. If a non-nil
// error is returned, it will typically contain commands exit status,
// which can be retrieved with IsExitStatus.
//
// Run can be called multiple times synchronously to interpret programs
// incrementally. To reuse a Runner without keeping the internal shell state,
// call Reset.
func (r *Runner) Run(ctx context.Context, node syntax.Node) error {
	if !r.didReset {
		r.Reset()
	}
	r.fillExpandConfig(ctx)
	r.err = nil
	r.exitShell = false
	r.filename = ""
	switch x := node.(type) {
	case *syntax.File:
		r.filename = x.Name
		r.stmts(ctx, x.Stmts)
	case *syntax.Stmt:
		r.stmt(ctx, x)
	case syntax.Command:
		r.cmd(ctx, x)
	default:
		return fmt.Errorf("node can only be File, Stmt, or Command: %T", x)
	}
	if r.exit != 0 {
		r.setErr(NewExitStatus(uint8(r.exit)))
	}
	return r.err
}

// Exited reports whether the last Run call should exit an entire shell. This
// can be triggered by the "exit" built-in command, for example.
//
// Note that this state is overwritten at every Run call, so it should be
// checked immediately after each Run call.
func (r *Runner) Exited() bool {
	return r.exitShell
}

func (r *Runner) out(s string) {
	io.WriteString(r.stdout, s)
}

func (r *Runner) outf(format string, a ...interface{}) {
	fmt.Fprintf(r.stdout, format, a...)
}

func (r *Runner) errf(format string, a ...interface{}) {
	fmt.Fprintf(r.stderr, format, a...)
}

func (r *Runner) stop(ctx context.Context) bool {
	if r.err != nil || r.exitShell {
		return true
	}
	if err := ctx.Err(); err != nil {
		r.err = err
		return true
	}
	if r.opts[optNoExec] {
		return true
	}
	return false
}

func (r *Runner) stmt(ctx context.Context, st *syntax.Stmt) {
	if r.stop(ctx) {
		return
	}
	if st.Background {
		r2 := r.sub()
		st2 := *st
		st2.Background = false
		r.bgShells.Go(func() error {
			return r2.Run(ctx, &st2)
		})
	} else {
		r.stmtSync(ctx, st)
	}
}

func (r *Runner) stmtSync(ctx context.Context, st *syntax.Stmt) {
	oldIn, oldOut, oldErr := r.stdin, r.stdout, r.stderr
	for _, rd := range st.Redirs {
		cls, err := r.redir(ctx, rd)
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
		r.cmd(ctx, st.Cmd)
	}
	if st.Negated {
		r.exit = oneIf(r.exit == 0)
	} else if _, ok := st.Cmd.(*syntax.CallExpr); !ok {
	} else if r.exit != 0 && !r.noErrExit && r.opts[optErrExit] {
		// If the "errexit" option is set and a simple command failed,
		// exit the shell. Exceptions:
		//
		//   conditions (if <cond>, while <cond>, etc)
		//   part of && or || lists
		//   preceded by !
		r.exitShell = true
	}
	if !r.keepRedirs {
		r.stdin, r.stdout, r.stderr = oldIn, oldOut, oldErr
	}
}

func (r *Runner) sub() *Runner {
	// Keep in sync with the Runner type. Manually copy fields, to not copy
	// sensitive ones like errgroup.Group, and to do deep copies of slices.
	r2 := &Runner{
		Env:         r.Env,
		Dir:         r.Dir,
		Params:      r.Params,
		Funcs:       r.Funcs,
		execHandler: r.execHandler,
		openHandler: r.openHandler,
		stdin:       r.stdin,
		stdout:      r.stdout,
		stderr:      r.stderr,
		filename:    r.filename,
		opts:        r.opts,
	}
	r2.Vars = make(map[string]expand.Variable, len(r.Vars))
	for k, v := range r.Vars {
		r2.Vars[k] = v
	}
	r2.funcVars = make(map[string]expand.Variable, len(r.funcVars))
	for k, v := range r.funcVars {
		r2.funcVars[k] = v
	}
	r2.cmdVars = make(map[string]string, len(r.cmdVars))
	for k, v := range r.cmdVars {
		r2.cmdVars[k] = v
	}
	r2.dirStack = append(r2.dirBootstrap[:0], r.dirStack...)
	r2.fillExpandConfig(r.ectx)
	r2.didReset = true
	return r2
}

func (r *Runner) cmd(ctx context.Context, cm syntax.Command) {
	if r.stop(ctx) {
		return
	}
	switch x := cm.(type) {
	case *syntax.Block:
		r.stmts(ctx, x.Stmts)
	case *syntax.Subshell:
		r2 := r.sub()
		r2.stmts(ctx, x.Stmts)
		r.exit = r2.exit
		r.setErr(r2.err)
	case *syntax.CallExpr:
		fields := r.fields(x.Args...)
		if len(fields) == 0 {
			for _, as := range x.Assigns {
				vr := r.assignVal(as, "")
				r.setVar(as.Name.Value, as.Index, vr)
			}
			break
		}
		for _, as := range x.Assigns {
			vr := r.assignVal(as, "")
			// we know that inline vars must be strings
			r.cmdVars[as.Name.Value] = vr.Str
		}
		r.call(ctx, x.Args[0].Pos(), fields)
		// cmdVars can be nuked here, as they are never useful
		// again once we nest into further levels of inline
		// vars.
		for k := range r.cmdVars {
			delete(r.cmdVars, k)
		}
	case *syntax.BinaryCmd:
		switch x.Op {
		case syntax.AndStmt, syntax.OrStmt:
			oldNoErrExit := r.noErrExit
			r.noErrExit = true
			r.stmt(ctx, x.X)
			r.noErrExit = oldNoErrExit
			if (r.exit == 0) == (x.Op == syntax.AndStmt) {
				r.stmt(ctx, x.Y)
			}
		case syntax.Pipe, syntax.PipeAll:
			pr, pw := io.Pipe()
			r2 := r.sub()
			r2.stdout = pw
			if x.Op == syntax.PipeAll {
				r2.stderr = pw
			} else {
				r2.stderr = r.stderr
			}
			r.bufCopier.Reader = pr
			r.stdin = &r.bufCopier
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				r2.stmt(ctx, x.X)
				pw.Close()
				wg.Done()
			}()
			r.stmt(ctx, x.Y)
			pr.Close()
			wg.Wait()
			if r.opts[optPipeFail] && r2.exit != 0 && r.exit == 0 {
				r.exit = r2.exit
			}
			r.setErr(r2.err)
		}
	case *syntax.IfClause:
		oldNoErrExit := r.noErrExit
		r.noErrExit = true
		r.stmts(ctx, x.Cond)
		r.noErrExit = oldNoErrExit

		if r.exit == 0 {
			r.stmts(ctx, x.Then)
			break
		}
		r.exit = 0
		if x.Else != nil {
			r.cmd(ctx, x.Else)
		}
	case *syntax.WhileClause:
		for !r.stop(ctx) {
			oldNoErrExit := r.noErrExit
			r.noErrExit = true
			r.stmts(ctx, x.Cond)
			r.noErrExit = oldNoErrExit

			stop := (r.exit == 0) == x.Until
			r.exit = 0
			if stop || r.loopStmtsBroken(ctx, x.Do) {
				break
			}
		}
	case *syntax.ForClause:
		switch y := x.Loop.(type) {
		case *syntax.WordIter:
			name := y.Name.Value
			items := r.Params // for i; do ...
			if y.InPos.IsValid() {
				items = r.fields(y.Items...) // for i in ...; do ...
			}
			for _, field := range items {
				r.setVarString(name, field)
				if r.loopStmtsBroken(ctx, x.Do) {
					break
				}
			}
		case *syntax.CStyleLoop:
			r.arithm(y.Init)
			for r.arithm(y.Cond) != 0 {
				if r.exit != 0 || r.loopStmtsBroken(ctx, x.Do) {
					break
				}
				r.arithm(y.Post)
			}
		}
	case *syntax.FuncDecl:
		r.setFunc(x.Name.Value, x.Body)
	case *syntax.ArithmCmd:
		r.exit = oneIf(r.arithm(x.X) == 0)
	case *syntax.LetClause:
		var val int
		for _, expr := range x.Exprs {
			val = r.arithm(expr)
		}
		r.exit = oneIf(val == 0)
	case *syntax.CaseClause:
		str := r.literal(x.Word)
		for _, ci := range x.Items {
			for _, word := range ci.Patterns {
				pattern := r.pattern(word)
				if match(pattern, str) {
					r.stmts(ctx, ci.Stmts)
					return
				}
			}
		}
	case *syntax.TestClause:
		r.exit = 0
		if r.bashTest(ctx, x.X, false) == "" && r.exit == 0 {
			// to preserve exit status code 2 for regex errors, etc
			r.exit = 1
		}
	case *syntax.DeclClause:
		local, global := false, false
		var modes []string
		valType := ""
		switch x.Variant.Value {
		case "declare":
			// When used in a function, "declare" acts as "local"
			// unless the "-g" option is used.
			local = r.inFunc
		case "local":
			if !r.inFunc {
				r.errf("local: can only be used in a function\n")
				r.exit = 1
				return
			}
			local = true
		case "export":
			modes = append(modes, "-x")
		case "readonly":
			modes = append(modes, "-r")
		case "nameref":
			valType = "-n"
		}
		for _, as := range x.Args {
			for _, as := range r.flattenAssign(as) {
				name := as.Name.Value
				if strings.HasPrefix(name, "-") {
					switch name {
					case "-x", "-r":
						modes = append(modes, name)
					case "-a", "-A", "-n":
						valType = name
					case "-g":
						global = true
					default:
						r.errf("declare: invalid option %q\n", name)
						r.exit = 2
						return
					}
					continue
				}
				if !syntax.ValidName(name) {
					r.errf("declare: invalid name %q\n", name)
					r.exit = 1
					return
				}
				vr := r.assignVal(as, valType)
				if global {
					vr.Local = false
				} else if local {
					vr.Local = true
				}
				for _, mode := range modes {
					switch mode {
					case "-x":
						vr.Exported = true
					case "-r":
						vr.ReadOnly = true
					}
				}
				r.setVar(name, as.Index, vr)
			}
		}
	case *syntax.TimeClause:
		start := time.Now()
		if x.Stmt != nil {
			r.stmt(ctx, x.Stmt)
		}
		format := "%s\t%s\n"
		if x.PosixFormat {
			format = "%s %s\n"
		} else {
			r.outf("\n")
		}
		real := time.Since(start)
		r.outf(format, "real", elapsedString(real, x.PosixFormat))
		// TODO: can we do these?
		r.outf(format, "user", elapsedString(0, x.PosixFormat))
		r.outf(format, "sys", elapsedString(0, x.PosixFormat))
	default:
		panic(fmt.Sprintf("unhandled command node: %T", x))
	}
}

func (r *Runner) flattenAssign(as *syntax.Assign) []*syntax.Assign {
	// Convert "declare $x" into "declare value".
	// Don't use syntax.Parser here, as we only want the basic
	// splitting by '='.
	if as.Name != nil {
		return []*syntax.Assign{as} // nothing to do
	}
	var asgns []*syntax.Assign
	for _, field := range r.fields(as.Value) {
		as := &syntax.Assign{}
		parts := strings.SplitN(field, "=", 2)
		as.Name = &syntax.Lit{Value: parts[0]}
		if len(parts) == 1 {
			as.Naked = true
		} else {
			as.Value = &syntax.Word{Parts: []syntax.WordPart{
				&syntax.Lit{Value: parts[1]},
			}}
		}
		asgns = append(asgns, as)
	}
	return asgns
}

func match(pat, name string) bool {
	expr, err := pattern.Regexp(pat, 0)
	if err != nil {
		return false
	}
	rx := regexp.MustCompile("^" + expr + "$")
	return rx.MatchString(name)
}

func elapsedString(d time.Duration, posix bool) string {
	if posix {
		return fmt.Sprintf("%.2f", d.Seconds())
	}
	min := int(d.Minutes())
	sec := math.Remainder(d.Seconds(), 60.0)
	return fmt.Sprintf("%dm%.3fs", min, sec)
}

func (r *Runner) stmts(ctx context.Context, stmts []*syntax.Stmt) {
	for _, stmt := range stmts {
		r.stmt(ctx, stmt)
	}
}

func (r *Runner) hdocReader(rd *syntax.Redirect) io.Reader {
	if rd.Op != syntax.DashHdoc {
		hdoc := r.document(rd.Hdoc)
		return strings.NewReader(hdoc)
	}
	var buf bytes.Buffer
	var cur []syntax.WordPart
	flushLine := func() {
		if buf.Len() > 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString(r.document(&syntax.Word{Parts: cur}))
		cur = cur[:0]
	}
	for _, wp := range rd.Hdoc.Parts {
		lit, ok := wp.(*syntax.Lit)
		if !ok {
			cur = append(cur, wp)
			continue
		}
		for i, part := range strings.Split(lit.Value, "\n") {
			if i > 0 {
				flushLine()
				cur = cur[:0]
			}
			part = strings.TrimLeft(part, "\t")
			cur = append(cur, &syntax.Lit{Value: part})
		}
	}
	flushLine()
	return &buf
}

func (r *Runner) redir(ctx context.Context, rd *syntax.Redirect) (io.Closer, error) {
	if rd.Hdoc != nil {
		r.stdin = r.hdocReader(rd)
		return nil, nil
	}
	orig := &r.stdout
	if rd.N != nil {
		switch rd.N.Value {
		case "1":
		case "2":
			orig = &r.stderr
		}
	}
	arg := r.literal(rd.Word)
	switch rd.Op {
	case syntax.WordHdoc:
		r.stdin = strings.NewReader(arg + "\n")
		return nil, nil
	case syntax.DplOut:
		switch arg {
		case "1":
			*orig = r.stdout
		case "2":
			*orig = r.stderr
		}
		return nil, nil
	case syntax.RdrIn, syntax.RdrOut, syntax.AppOut,
		syntax.RdrAll, syntax.AppAll:
		// done further below
	// case syntax.DplIn:
	default:
		panic(fmt.Sprintf("unhandled redirect op: %v", rd.Op))
	}
	mode := os.O_RDONLY
	switch rd.Op {
	case syntax.AppOut, syntax.AppAll:
		mode = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	case syntax.RdrOut, syntax.RdrAll:
		mode = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	f, err := r.open(ctx, arg, mode, 0644, true)
	if err != nil {
		return nil, err
	}
	switch rd.Op {
	case syntax.RdrIn:
		r.stdin = f
	case syntax.RdrOut, syntax.AppOut:
		*orig = f
	case syntax.RdrAll, syntax.AppAll:
		r.stdout = f
		r.stderr = f
	default:
		panic(fmt.Sprintf("unhandled redirect op: %v", rd.Op))
	}
	return f, nil
}

func (r *Runner) loopStmtsBroken(ctx context.Context, stmts []*syntax.Stmt) bool {
	oldInLoop := r.inLoop
	r.inLoop = true
	defer func() { r.inLoop = oldInLoop }()
	for _, stmt := range stmts {
		r.stmt(ctx, stmt)
		if r.contnEnclosing > 0 {
			r.contnEnclosing--
			return r.contnEnclosing > 0
		}
		if r.breakEnclosing > 0 {
			r.breakEnclosing--
			return true
		}
	}
	return false
}

type returnStatus uint8

func (s returnStatus) Error() string { return fmt.Sprintf("return status %d", s) }

func (r *Runner) call(ctx context.Context, pos syntax.Pos, args []string) {
	if r.stop(ctx) {
		return
	}
	name := args[0]
	if body := r.Funcs[name]; body != nil {
		// stack them to support nested func calls
		oldParams := r.Params
		r.Params = args[1:]
		oldInFunc := r.inFunc
		oldFuncVars := r.funcVars
		r.funcVars = nil
		r.inFunc = true

		r.stmt(ctx, body)

		r.Params = oldParams
		r.funcVars = oldFuncVars
		r.inFunc = oldInFunc
		if code, ok := r.err.(returnStatus); ok {
			r.err = nil
			r.exit = int(code)
		}
		return
	}
	if isBuiltin(name) {
		r.exit = r.builtinCode(ctx, pos, name, args[1:])
		return
	}
	r.exec(ctx, args)
}

func (r *Runner) exec(ctx context.Context, args []string) {
	err := r.execHandler(r.handlerCtx(ctx), args)
	if status, ok := IsExitStatus(err); ok {
		r.exit = int(status)
		return
	}
	if err != nil {
		// handler's custom fatal error
		r.setErr(err)
		return
	}
	r.exit = 0
}

func (r *Runner) open(ctx context.Context, path string, flags int, mode os.FileMode, print bool) (io.ReadWriteCloser, error) {
	f, err := r.openHandler(r.handlerCtx(ctx), path, flags, mode)
	// TODO: support wrapped PathError returned from openHandler.
	switch err.(type) {
	case nil:
	case *os.PathError:
		if print {
			r.errf("%v\n", err)
		}
	default: // handler's custom fatal error
		r.setErr(err)
	}
	return f, err
}

func (r *Runner) stat(name string) (os.FileInfo, error) {
	return os.Stat(r.absPath(name))
}
