// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	mathrand "math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

// HandlerCtx returns the [HandlerContext] value stored in ctx,
// which is used when calling handler functions.
// It panics if ctx has no HandlerContext stored.
func HandlerCtx(ctx context.Context) HandlerContext {
	hc, ok := ctx.Value(handlerCtxKey{}).(HandlerContext)
	if !ok {
		panic("interp.HandlerCtx: no HandlerContext in ctx")
	}
	return hc
}

type handlerCtxKey struct{}

type handlerKind int

const (
	_                    handlerKind = iota
	handlerKindExec                  // [ExecHandlerFunc]
	handlerKindCall                  // [CallHandlerFunc]
	handlerKindOpen                  // [OpenHandlerFunc]
	handlerKindReadDir               // [ReadDirHandlerFunc2]
	handlerKindStat                  // [StatHandlerFunc]
	handlerKindAccess                // [AccessHandlerFunc]
	handlerKindProcSubst             // [ProcSubstHandlerFunc]
)

// HandlerContext is the data passed to all the handler functions via [context.WithValue].
// It contains some of the current state of the [Runner].
type HandlerContext struct {
	runner *Runner // for internal use only, e.g. [HandlerContext.Builtin]

	// kind records which type of handler this context was built for.
	kind handlerKind

	// Env is a read-only version of the interpreter's environment,
	// including environment variables, global variables, and local function
	// variables.
	Env expand.Environ

	// Dir is the interpreter's current directory.
	Dir string

	// Pos is the source position which relates to the operation,
	// such as a [syntax.CallExpr] when calling an [ExecHandlerFunc].
	// It may be invalid if the operation has no relevant position information.
	Pos syntax.Pos

	// TODO(v4): use an os.File for stdin below directly.

	// Stdin is the interpreter's current standard input reader.
	// It is always an [*os.File], except on js/wasm, where it may be
	// any reader; the type here remains an [io.Reader]
	// due to backwards compatibility.
	Stdin io.Reader
	// Stdout is the interpreter's current standard output writer.
	Stdout io.Writer
	// Stderr is the interpreter's current standard error writer.
	Stderr io.Writer

	// LastExitStatus is the value that "$?" would hold when the handler is called.
	// A [CallHandlerFunc] or [ExecHandlerFunc] runs as part of its own command,
	// so this refers to the command before it.
	// At the start of a trap callback, it is the status of the command which triggered the trap.
	LastExitStatus int
}

// CallHandlerFunc is a handler which runs on every [syntax.CallExpr].
// It is called once variable assignments and field expansion have occurred.
// The context includes a [HandlerContext] value.
//
// The call's arguments are replaced by what the handler returns,
// and then the call is executed by the Runner as usual.
// The args slice is never empty.
// At this time, returning an empty slice without an error is not supported.
//
// This handler is similar to [ExecHandlerFunc], but has two major differences:
//
// First, it runs for all simple commands, including function calls and builtins.
//
// Second, it is not expected to execute the simple command, but instead to
// allow running custom code which allows replacing the argument list.
// Shell builtins touch on many internals of the Runner, after all.
//
// Returning a non-nil error will halt the [Runner] and will be returned via the API.
type CallHandlerFunc func(ctx context.Context, args []string) ([]string, error)

// TODO: consistently treat handler errors as non-fatal by default,
// but have an interface or API to specify fatal errors which should make
// the shell exit with a particular status code.

// ExecHandlerFunc is a handler which executes simple commands.
// It is called for all [syntax.CallExpr] nodes
// where the first argument is neither a declared function nor a builtin.
// The args slice is never empty.
// The context includes a [HandlerContext] value.
//
// Returning a nil error means a zero exit status.
// Other exit statuses can be set by returning or wrapping a [NewExitStatus] error,
// and such an error is returned via the API if it is the last statement executed.
// Any other error will halt the [Runner] and will be returned via the API.
type ExecHandlerFunc func(ctx context.Context, args []string) error

// DefaultExecHandler returns the [ExecHandlerFunc] used by default.
// It finds binaries in PATH and executes them.
// When context is cancelled, an interrupt signal is sent to running processes.
// killTimeout is a duration to wait before sending the kill signal.
// A negative value means that a kill signal will be sent immediately.
//
// On Windows, the kill signal is always sent immediately,
// because Go doesn't currently support sending Interrupt on Windows.
// [Runner] defaults to a killTimeout of 2 seconds.
//
// On Unix, a file which fails to execute with ENOEXEC, such as a script
// without a shebang line, is run as a shell script with a new [Runner]
// using default options and handlers, like other shells do.
//
// TODO: perhaps intercept ENOEXEC scripts as well as shell shebangs
// such as "#!/bin/sh" so that they reuse the runner's configured handlers.
func DefaultExecHandler(killTimeout time.Duration) ExecHandlerFunc {
	return func(ctx context.Context, args []string) error {
		hc := HandlerCtx(ctx)
		path, err := LookPathDir(hc.Dir, hc.Env, args[0])
		if err != nil {
			fmt.Fprintln(hc.Stderr, err)
			return ExitStatus(127)
		}
		newCmd := func() *exec.Cmd {
			cmd := exec.CommandContext(ctx, path)
			cmd.Args = args
			cmd.Env = execEnv(hc.Env)
			cmd.Dir = hc.Dir
			cmd.Stdin = hc.Stdin
			cmd.Stdout = hc.Stdout
			cmd.Stderr = hc.Stderr
			if killTimeout > 0 && runtime.GOOS != "windows" {
				// On cancellation, send an interrupt signal first, and let
				// WaitDelay escalate to a kill signal if the process does not
				// exit in time. Otherwise, keep the default of killing right away.
				cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
				cmd.WaitDelay = killTimeout
			}
			return cmd
		}
		cmd := newCmd()
		err = cmd.Start()
		// On Unix, a concurrently forked process may briefly hold a write
		// file descriptor for the file inherited before its exec, making
		// our exec fail with ETXTBSY; see https://go.dev/issue/22315.
		// The window is short-lived, so retry with backoff. A failed Start
		// closes the command's pipes, so build a fresh one for each attempt.
		for delay := time.Millisecond; isETXTBSY(err) && delay < 300*time.Millisecond; delay *= 2 {
			time.Sleep(delay)
			cmd = newCmd()
			err = cmd.Start()
		}
		if isENOEXEC(err) {
			// Like other shells, run a file which the kernel refuses to
			// execute with ENOEXEC, such as a script without a shebang line,
			// as a shell script with a new copy of the shell.
			hc.runner.reportBgStart(0) // the nested shell isn't one program
			return runScriptENOEXEC(ctx, hc, killTimeout, path, args)
		}
		if err == nil {
			hc.runner.reportBgStart(cmd.Process.Pid)
			err = cmd.Wait()
		}

		switch err := err.(type) {
		case *exec.ExitError:
			// Windows and Plan9 do not have support for [syscall.WaitStatus]
			// with methods like Signaled and Signal, so for those, [waitStatus] is a no-op.
			// Note: [waitStatus] is an alias [syscall.WaitStatus]
			if status, ok := err.Sys().(waitStatus); ok && status.Signaled() {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return ExitStatus(128 + status.Signal())
			}
			return ExitStatus(err.ExitCode())
		case *exec.Error:
			// did not start
			fmt.Fprintf(hc.Stderr, "%v\n", err)
			return ExitStatus(127)
		default:
			// The command exited with success, but WaitDelay elapsed with its
			// I/O pipes still open, e.g. due to an orphaned subprocess.
			if errors.Is(err, exec.ErrWaitDelay) {
				return nil
			}
			return err
		}
	}
}

// runScriptENOEXEC runs a file as a shell script with a new shell,
// as POSIX requires when the kernel fails to execute it with ENOEXEC.
// The new shell does not inherit functions or unexported variables.
func runScriptENOEXEC(ctx context.Context, hc HandlerContext, killTimeout time.Duration, path string, args []string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(hc.Stderr, err)
		return ExitStatus(126)
	}
	// Like Bash, refuse to run the file as a script
	// if a null byte appears before the first newline.
	line, _, _ := bytes.Cut(b, []byte("\n"))
	if bytes.IndexByte(line, 0) >= 0 {
		fmt.Fprintf(hc.Stderr, "%s: cannot execute binary file\n", args[0])
		return ExitStatus(126)
	}
	file, err := syntax.NewParser().Parse(bytes.NewReader(b), args[0])
	if err != nil {
		fmt.Fprintln(hc.Stderr, err)
		return ExitStatus(2) // like Bash with a syntax error
	}
	r, err := New(
		Dir(hc.Dir),
		Env(expand.ListEnviron(execEnv(hc.Env)...)),
		StdIO(hc.Stdin, hc.Stdout, hc.Stderr),
		ExecHandler(DefaultExecHandler(killTimeout)),
	)
	if err != nil {
		return err
	}
	r.Params = args[1:]
	return r.Run(ctx, file)
}

func checkStat(dir, file string, checkExec bool) (string, error) {
	if !filepath.IsAbs(file) {
		file = filepath.Join(dir, file)
	}
	info, err := os.Stat(file)
	if err != nil {
		return "", err
	}
	m := info.Mode()
	if m.IsDir() {
		return "", fmt.Errorf("is a directory")
	}
	if checkExec && runtime.GOOS != "windows" && m&0o111 == 0 {
		return "", fmt.Errorf("permission denied")
	}
	return file, nil
}

func winHasExt(file string) bool {
	i := strings.LastIndex(file, ".")
	if i < 0 {
		return false
	}
	return strings.LastIndexAny(file, `:\/`) < i
}

// findExecutable returns the path to an existing executable file.
func findExecutable(dir, file string, exts []string) (string, error) {
	if len(exts) == 0 {
		// non-windows
		return checkStat(dir, file, true)
	}
	if winHasExt(file) {
		if file, err := checkStat(dir, file, true); err == nil {
			return file, nil
		}
	}
	for _, e := range exts {
		f := file + e
		if f, err := checkStat(dir, f, true); err == nil {
			return f, nil
		}
	}
	return "", fmt.Errorf("not found")
}

// findFile returns the path to an existing file.
func findFile(dir, file string, _ []string) (string, error) {
	return checkStat(dir, file, false)
}

// LookPath is deprecated; see [LookPathDir].
func LookPath(env expand.Environ, file string) (string, error) {
	return LookPathDir(env.Get("PWD").String(), env, file)
}

// LookPathDir is similar to [os/exec.LookPath], with the difference that it uses the
// provided environment. env is used to fetch relevant environment variables
// such as PWD and PATH.
//
// If no error is returned, the returned path must be valid.
func LookPathDir(cwd string, env expand.Environ, file string) (string, error) {
	return lookPathDir(cwd, env, file, findExecutable)
}

// findAny defines a function to pass to [lookPathDir].
type findAny = func(dir string, file string, exts []string) (string, error)

func lookPathDir(cwd string, env expand.Environ, file string, find findAny) (string, error) {
	if find == nil {
		panic("no find function found")
	}

	pathList := filepath.SplitList(env.Get("PATH").String())
	if len(pathList) == 0 {
		pathList = []string{""}
	}
	chars := `/`
	if runtime.GOOS == "windows" {
		chars = `:\/`
	}
	exts := pathExts(env)
	if strings.ContainsAny(file, chars) {
		return find(cwd, file, exts)
	}
	for _, elem := range pathList {
		var path string
		switch elem {
		case "", ".":
			// otherwise "foo" won't be "./foo"
			path = "." + string(filepath.Separator) + file
		default:
			path = filepath.Join(elem, file)
		}
		if f, err := find(cwd, path, exts); err == nil {
			return f, nil
		}
	}
	return "", fmt.Errorf("%q: executable file not found in $PATH", file)
}

// scriptFromPathDir is similar to [LookPathDir], with the difference that it looks
// for both executable and non-executable files.
func scriptFromPathDir(cwd string, env expand.Environ, file string) (string, error) {
	return lookPathDir(cwd, env, file, findFile)
}

func pathExts(env expand.Environ) []string {
	if runtime.GOOS != "windows" {
		return nil
	}
	pathext := env.Get("PATHEXT").String()
	if pathext == "" {
		return []string{".com", ".exe", ".bat", ".cmd"}
	}
	var exts []string
	for e := range strings.SplitSeq(strings.ToLower(pathext), `;`) {
		if e == "" {
			continue
		}
		if e[0] != '.' {
			e = "." + e
		}
		exts = append(exts, e)
	}
	return exts
}

// OpenHandlerFunc is a handler which opens files.
// It is called for all files that are opened directly by the shell,
// such as in redirects, except for the paths of active process substitutions,
// which are opened via [ProcSubstFile.OpenConsumer].
// The context includes a [HandlerContext] value.
// Files opened by executed programs are not included.
//
// The path parameter may be relative to the current directory,
// which can be fetched via [HandlerCtx].
//
// Use a return error of type [*os.PathError] to have the error printed to
// stderr and the exit status set to 1.
// Any other error will halt the [Runner] and will be returned via the API.
//
// Note that implementations which do not return [os.File] will cause
// extra files and goroutines for input redirections; see [StdIO].
type OpenHandlerFunc func(ctx context.Context, path string, flag int, perm os.FileMode) (io.ReadWriteCloser, error)

// TODO: paths passed to [OpenHandlerFunc] should be cleaned.

// DefaultOpenHandler returns the [OpenHandlerFunc] used by default.
// It uses [os.OpenFile] to open files.
//
// For the sake of portability, /dev/null opens NUL on Windows.
func DefaultOpenHandler() OpenHandlerFunc {
	return func(ctx context.Context, path string, flag int, perm os.FileMode) (io.ReadWriteCloser, error) {
		mc := HandlerCtx(ctx)
		if runtime.GOOS == "windows" && path == "/dev/null" {
			path = "NUL"
			// Note that even though https://go.dev/issue/71752 was resolved for Windows,
			// the workaround here seems to still be required for Wine as of 10.14.
			// TODO(mvdan): Why? Is this Wine's fault?
			flag &^= os.O_TRUNC
		} else if path != "" && !filepath.IsAbs(path) {
			path = filepath.Join(mc.Dir, path)
		}
		return os.OpenFile(path, flag, perm)
	}
}

// ReadDirHandlerFunc is a handler which reads directories. It is called during
// shell globbing, if enabled.
//
// Deprecated: use [ReadDirHandlerFunc2], which uses [fs.DirEntry].
type ReadDirHandlerFunc func(ctx context.Context, path string) ([]fs.FileInfo, error)

// ReadDirHandlerFunc2 is a handler which reads directories. It is called during
// shell globbing, if enabled.
// The context includes a [HandlerContext] value.
type ReadDirHandlerFunc2 func(ctx context.Context, path string) ([]fs.DirEntry, error)

// DefaultReadDirHandler returns the [ReadDirHandlerFunc] used by default.
// It uses [os.ReadDir].
//
// Deprecated: use [DefaultReadDirHandler2], which uses [fs.DirEntry].
func DefaultReadDirHandler() ReadDirHandlerFunc {
	return func(ctx context.Context, path string) ([]fs.FileInfo, error) {
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		infos := make([]fs.FileInfo, 0, len(entries))
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				return nil, err
			}
			infos = append(infos, info)
		}
		return infos, nil
	}
}

// DefaultReadDirHandler2 returns the [ReadDirHandlerFunc2] used by default.
// It uses [os.ReadDir].
func DefaultReadDirHandler2() ReadDirHandlerFunc2 {
	return func(ctx context.Context, path string) ([]fs.DirEntry, error) {
		return os.ReadDir(path)
	}
}

// StatHandlerFunc is a handler which gets a file's information.
// The context includes a [HandlerContext] value.
type StatHandlerFunc func(ctx context.Context, name string, followSymlinks bool) (fs.FileInfo, error)

// DefaultStatHandler returns the [StatHandlerFunc] used by default.
// It makes use of [os.Stat] and [os.Lstat], depending on followSymlinks.
func DefaultStatHandler() StatHandlerFunc {
	return func(ctx context.Context, path string, followSymlinks bool) (fs.FileInfo, error) {
		if !followSymlinks {
			return os.Lstat(path)
		} else {
			return os.Stat(path)
		}
	}
}

// AccessMode is a bitmask of file access checks, used by [AccessHandlerFunc].
type AccessMode uint32

// The values match access(2)'s R_OK, W_OK, and X_OK.
const (
	AccessRead  AccessMode = 0b100
	AccessWrite AccessMode = 0b010
	AccessExec  AccessMode = 0b001
)

// TODO(v4): fold AccessHandlerFunc into StatHandlerFunc.

// AccessHandlerFunc is a handler which checks whether the current user can
// access a file. It is called by the unary test operators -r, -w, and -x,
// and by builtins such as cd. The path is always absolute.
// A nil error means access is allowed.
// The context includes a [HandlerContext] value.
type AccessHandlerFunc func(ctx context.Context, path string, mode AccessMode) error

// DefaultAccessHandler returns the [AccessHandlerFunc] used by default.
// On Unix it uses access(2), which consults the real filesystem and the
// current user's role. On other platforms, which lack access(2), it
// approximates the check via the stat handler and the file's permission bits.
func DefaultAccessHandler() AccessHandlerFunc {
	return defaultAccess
}

// ProcSubstHandlerFunc is a handler which sets up process substitutions,
// that is, `<(cmd)` with [syntax.CmdIn] and `>(cmd)` with [syntax.CmdOut];
// op is always one of those two operators.
// The context includes a [HandlerContext] value.
//
// The default handler creates real named pipes via [DefaultProcSubstHandler];
// a custom handler can implement process substitutions entirely in memory,
// for example via [io.Pipe]. Note that [ProcSubstFile.Path] can also be
// opened directly by executed programs, so in-memory implementations are
// only useful when programs run via a custom [ExecHandlerFunc] as well.
type ProcSubstHandlerFunc func(ctx context.Context, op syntax.ProcOperator) (*ProcSubstFile, error)

// ProcSubstFile describes one process substitution
// set up by a [ProcSubstHandlerFunc].
type ProcSubstFile struct {
	// Path substitutes the process substitution word in the command line.
	// It must not equal the path of another active process substitution.
	Path string

	// OpenSubshell is called exactly once, from the background subshell
	// running the process substitution's statements, to obtain the file
	// used as the subshell's standard output for [syntax.CmdIn],
	// or its standard input for [syntax.CmdOut];
	// the opposite direction is never used.
	// It may block until the other end of Path is opened,
	// just like [os.OpenFile] on a named pipe.
	//
	// Note that returning a file which is not an [os.File] causes an
	// extra file and goroutine for [syntax.CmdOut]; see [StdIO].
	OpenSubshell func(ctx context.Context) (io.ReadWriteCloser, error)

	// OpenConsumer is called in place of [OpenHandlerFunc] whenever the
	// shell itself opens Path while the process substitution is active,
	// such as in a redirection. If nil, [OpenHandlerFunc] is used.
	OpenConsumer func(ctx context.Context, flag int) (io.ReadWriteCloser, error)

	// Cleanup, if not nil, is called once the subshell has finished
	// and its file has been closed.
	// A non-nil error is printed to the shell's standard error.
	Cleanup func() error
}

const fifoNamePrefix = "sh-interp-"

// DefaultProcSubstHandler returns the [ProcSubstHandlerFunc] used by default.
// It creates a named pipe (FIFO) inside the runner's temporary directory
// via mkfifo(3), to be opened with [os.OpenFile] and removed by cleanup.
// It is not supported on Windows.
func DefaultProcSubstHandler() ProcSubstHandlerFunc {
	return func(ctx context.Context, op syntax.ProcOperator) (*ProcSubstFile, error) {
		if runtime.GOOS == "windows" {
			return nil, fmt.Errorf("TODO: support process substitution on Windows")
		}
		var flag int
		switch op {
		case syntax.CmdIn: // the subshell writes to the fifo
			flag = os.O_WRONLY
		case syntax.CmdOut: // the subshell reads from the fifo
			flag = os.O_RDONLY
		default:
			return nil, fmt.Errorf("unexpected process substitution operator: %v", op)
		}
		r := HandlerCtx(ctx).runner

		// We can't atomically create a random unused temporary FIFO.
		// Similar to [os.CreateTemp],
		// keep trying new random paths until one does not exist.
		// We use a uint64 because a uint32 easily runs into retries.
		var path string
		try := 0
		for {
			path = filepath.Join(r.tempDir, fifoNamePrefix+strconv.FormatUint(mathrand.Uint64(), 16))
			err := mkfifo(path, 0o666)
			if err == nil {
				break
			}
			if !os.IsExist(err) {
				return nil, fmt.Errorf("cannot create fifo: %v", err)
			}
			if try++; try > 100 {
				return nil, fmt.Errorf("giving up at creating fifo: %v", err)
			}
		}
		return &ProcSubstFile{
			Path: path,
			OpenSubshell: func(ctx context.Context) (io.ReadWriteCloser, error) {
				// Blocks until the consumer opens the other end.
				return os.OpenFile(path, flag, 0)
			},
			OpenConsumer: func(ctx context.Context, flag int) (io.ReadWriteCloser, error) {
				// A named pipe can only be opened via [os.OpenFile];
				// a custom [OpenHandlerFunc] would not work with it.
				return os.OpenFile(path, flag, 0)
			},
			Cleanup: func() error { return os.Remove(path) },
		}, nil
	}
}

// procSubstRegistry tracks active process substitutions so that [Runner.open]
// can route the opening of their paths to [ProcSubstFile.OpenConsumer].
// It is shared by a runner and all of its subshells.
type procSubstRegistry struct {
	mu    sync.Mutex
	files map[string]*ProcSubstFile
}

func (reg *procSubstRegistry) add(psf *ProcSubstFile) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.files == nil {
		reg.files = make(map[string]*ProcSubstFile)
	}
	reg.files[psf.Path] = psf
}

// remove unregisters a process substitution.
func (reg *procSubstRegistry) remove(psf *ProcSubstFile) {
	reg.mu.Lock()
	delete(reg.files, psf.Path)
	reg.mu.Unlock()
}

// lookup returns the consumer open function for an active process
// substitution path, if there is one.
func (reg *procSubstRegistry) lookup(path string) func(ctx context.Context, flag int) (io.ReadWriteCloser, error) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if psf := reg.files[path]; psf != nil {
		return psf.OpenConsumer
	}
	return nil
}
