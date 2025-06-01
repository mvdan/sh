// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

// IsBuiltin returns true if the given word is a shell builtin.
func IsBuiltin(name string) bool {
	switch name {
	case ":", "true", "false", "exit", "set", "shift", "unset",
		"echo", "printf", "break", "continue", "pwd", "cd",
		"wait", "builtin", "trap", "type", "source", ".", "command",
		"dirs", "pushd", "popd", "umask", "alias", "unalias",
		"fg", "bg", "getopts", "eval", "test", "[", "exec",
		"return", "read", "mapfile", "readarray", "shopt":
		return true
	}
	return false
}

// TODO: atoi is duplicated in the expand package.

// atoi is like [strconv.ParseInt](s, 10, 64), but it ignores errors and trims whitespace.
func atoi(s string) int64 {
	s = strings.TrimSpace(s)
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

type errBuiltinExitStatus exitStatus

func (e errBuiltinExitStatus) Error() string {
	return fmt.Sprintf("builtin exit status %d", e.code)
}

// Builtin allows [ExecHandlerFunc] implementations to execute any builtin,
// which can be useful for an exec handler to wrap or combine builtin calls.
//
// Note that a non-nil error may be returned in cases where the builtin
// alters the control flow of the runner, even if the builtin did not fail.
// For example, this is the case with `exit 0` or `return`.
func (hc HandlerContext) Builtin(ctx context.Context, args []string) error {
	if hc.kind != handlerKindExec {
		return fmt.Errorf("HandlerContext.Builtin can only be called via an ExecHandlerFunc")
	}
	exit := hc.runner.builtin(ctx, hc.Pos, args[0], args[1:])
	if exit != (exitStatus{}) {
		return errBuiltinExitStatus(exit)
	}
	return nil
}

func (r *Runner) builtin(ctx context.Context, pos syntax.Pos, name string, args []string) (exit exitStatus) {
	failf := func(code uint8, format string, args ...any) exitStatus {
		r.errf(format, args...)
		exit.code = code
		return exit
	}
	switch name {
	case ":", "true":
	case "false":
		exit.code = 1
	case "exit":
		switch len(args) {
		case 0:
			exit = r.lastExit
		case 1:
			n, err := strconv.Atoi(args[0])
			if err != nil {
				return failf(2, "invalid exit status code: %q\n", args[0])
			}
			exit.code = uint8(n)
		default:
			return failf(1, "exit cannot take multiple arguments\n")
		}
		exit.exiting = true
	case "set":
		if err := Params(args...)(r); err != nil {
			return failf(2, "set: %v\n", err)
		}
		r.updateExpandOpts()
	case "shift":
		n := 1
		switch len(args) {
		case 0:
		case 1:
			if n2, err := strconv.Atoi(args[0]); err == nil {
				n = n2
				break
			}
			fallthrough
		default:
			return failf(2, "usage: shift [n]\n")
		}
		if n >= len(r.Params) {
			r.Params = nil
		} else {
			r.Params = r.Params[n:]
		}
	case "unset":
		vars := true
		funcs := true
	unsetOpts:
		for i, arg := range args {
			switch arg {
			case "-v":
				funcs = false
			case "-f":
				vars = false
			default:
				args = args[i:]
				break unsetOpts
			}
		}

		for _, arg := range args {
			if vars && r.lookupVar(arg).IsSet() {
				r.delVar(arg)
			} else if _, ok := r.Funcs[arg]; ok && funcs {
				delete(r.Funcs, arg)
			}
		}
	case "echo":
		newline, doExpand := true, false
	echoOpts:
		for len(args) > 0 {
			switch args[0] {
			case "-n":
				newline = false
			case "-e":
				doExpand = true
			case "-E": // default
			default:
				break echoOpts
			}
			args = args[1:]
		}
		for i, arg := range args {
			if i > 0 {
				r.out(" ")
			}
			if doExpand {
				arg, _, _ = expand.Format(r.ecfg, arg, nil)
			}
			r.out(arg)
		}
		if newline {
			r.out("\n")
		}
	case "printf":
		if len(args) == 0 {
			return failf(2, "usage: printf format [arguments]\n")
		}
		format, args := args[0], args[1:]
		for {
			s, n, err := expand.Format(r.ecfg, format, args)
			if err != nil {
				return failf(1, "%v\n", err)
			}
			r.out(s)
			args = args[n:]
			if n == 0 || len(args) == 0 {
				break
			}
		}
	case "break", "continue":
		if !r.inLoop {
			return failf(0, "%s is only useful in a loop\n", name)
		}
		enclosing := &r.breakEnclosing
		if name == "continue" {
			enclosing = &r.contnEnclosing
		}
		switch len(args) {
		case 0:
			*enclosing = 1
		case 1:
			if n, err := strconv.Atoi(args[0]); err == nil {
				*enclosing = n
				break
			}
			fallthrough
		default:
			return failf(2, "usage: %s [n]\n", name)
		}
	case "pwd":
		evalSymlinks := false
		for len(args) > 0 {
			switch args[0] {
			case "-L":
				evalSymlinks = false
			case "-P":
				evalSymlinks = true
			default:
				return failf(2, "invalid option: %q\n", args[0])
			}
			args = args[1:]
		}
		pwd := r.envGet("PWD")
		if evalSymlinks {
			var err error
			pwd, err = filepath.EvalSymlinks(pwd)
			if err != nil {
				exit.fatal(err) // perhaps overly dramatic?
				return exit
			}
		}
		r.outf("%s\n", pwd)
	case "cd":
		var path string
		switch len(args) {
		case 0:
			path = r.envGet("HOME")
		case 1:
			path = args[0]

			// replicate the commonly implemented behavior of `cd -`
			// ref: https://www.man7.org/linux/man-pages/man1/cd.1p.html#OPERANDS
			if path == "-" {
				path = r.envGet("OLDPWD")
				r.outf("%s\n", path)
			}
		default:
			return failf(2, "usage: cd [dir]\n")
		}
		exit.code = r.changeDir(ctx, path)
	case "wait":
		fp := flagParser{remaining: args}
		for fp.more() {
			switch flag := fp.flag(); flag {
			case "-n", "-p":
				return failf(2, "wait: unsupported option %q\n", flag)
			default:
				return failf(2, "wait: invalid option %q\n", flag)
			}
		}
		if len(args) == 0 {
			// Note that "wait" without arguments always returns exit status zero.
			for _, bg := range r.bgProcs {
				<-bg.done
			}
			break
		}
		for _, arg := range args {
			arg, ok := strings.CutPrefix(arg, "g")
			pid := atoi(arg)
			if !ok || pid <= 0 || pid > int64(len(r.bgProcs)) {
				return failf(1, "wait: pid %s is not a child of this shell\n", arg)
			}
			bg := r.bgProcs[pid-1]
			<-bg.done
			exit = *bg.exit
		}
	case "builtin":
		if len(args) < 1 {
			break
		}
		if !IsBuiltin(args[0]) {
			exit.code = 1
			return exit
		}
		exit = r.builtin(ctx, pos, args[0], args[1:])
	case "type":
		anyNotFound := false
		mode := ""
		fp := flagParser{remaining: args}
		for fp.more() {
			switch flag := fp.flag(); flag {
			case "-a", "-f", "-P", "--help":
				return failf(3, "command: NOT IMPLEMENTED\n")
			case "-p", "-t":
				mode = flag
			default:
				return failf(2, "command: invalid option %q\n", flag)
			}
		}
		args := fp.args()
		for _, arg := range args {
			if mode == "-p" {
				if path, err := LookPathDir(r.Dir, r.writeEnv, arg); err == nil {
					r.outf("%s\n", path)
				} else {
					anyNotFound = true
				}
				continue
			}
			if syntax.IsKeyword(arg) {
				if mode == "-t" {
					r.out("keyword\n")
				} else {
					r.outf("%s is a shell keyword\n", arg)
				}
				continue
			}
			if als, ok := r.alias[arg]; ok && r.opts[optExpandAliases] {
				var buf bytes.Buffer
				if len(als.args) > 0 {
					printer := syntax.NewPrinter()
					printer.Print(&buf, &syntax.CallExpr{
						Args: als.args,
					})
				}
				if als.blank {
					buf.WriteByte(' ')
				}
				if mode == "-t" {
					r.out("alias\n")
				} else {
					r.outf("%s is aliased to `%s'\n", arg, &buf)
				}
				continue
			}
			if _, ok := r.Funcs[arg]; ok {
				if mode == "-t" {
					r.out("function\n")
				} else {
					r.outf("%s is a function\n", arg)
				}
				continue
			}
			if IsBuiltin(arg) {
				if mode == "-t" {
					r.out("builtin\n")
				} else {
					r.outf("%s is a shell builtin\n", arg)
				}
				continue
			}
			if path, err := LookPathDir(r.Dir, r.writeEnv, arg); err == nil {
				if mode == "-t" {
					r.out("file\n")
				} else {
					r.outf("%s is %s\n", arg, path)
				}
				continue
			}
			if mode != "-t" {
				r.errf("type: %s: not found\n", arg)
			}
			anyNotFound = true
		}
		if anyNotFound {
			exit.code = 1
		}
	case "eval":
		src := strings.Join(args, " ")
		p := syntax.NewParser()
		file, err := p.Parse(strings.NewReader(src), "")
		if err != nil {
			return failf(1, "eval: %v\n", err)
		}
		r.stmts(ctx, file.Stmts)
		exit = r.exit
	case "source", ".":
		if len(args) < 1 {
			return failf(2, "%v: source: need filename\n", pos)
		}
		path, err := scriptFromPathDir(r.Dir, r.writeEnv, args[0])
		if err != nil {
			// If the script was not found in PATH or there was any error, pass
			// the source path to the open handler so it has a chance to look
			// at files it manages (eg: virtual filesystem), and also allow
			// it to look for the sourced script in the current directory.
			path = args[0]
		}
		f, err := r.open(ctx, path, os.O_RDONLY, 0, false)
		if err != nil {
			return failf(1, "source: %v\n", err)
		}
		defer f.Close()
		p := syntax.NewParser()
		file, err := p.Parse(f, path)
		if err != nil {
			return failf(1, "source: %v\n", err)
		}

		// Keep the current versions of some fields we might modify.
		oldParams := r.Params
		oldSourceSetParams := r.sourceSetParams
		oldInSource := r.inSource

		// If we run "source file args...", set said args as parameters.
		// Otherwise, keep the current parameters.
		sourceArgs := len(args[1:]) > 0
		if sourceArgs {
			r.Params = args[1:]
			r.sourceSetParams = false
		}
		// We want to track if the sourced file explicitly sets the
		// parameters.
		r.sourceSetParams = false
		r.inSource = true // know that we're inside a sourced script.
		r.stmts(ctx, file.Stmts)

		// If we modified the parameters and the sourced file didn't
		// explicitly set them, we restore the old ones.
		if sourceArgs && !r.sourceSetParams {
			r.Params = oldParams
		}
		r.sourceSetParams = oldSourceSetParams
		r.inSource = oldInSource

		exit = r.exit
		exit.returning = false
	case "[":
		if len(args) == 0 || args[len(args)-1] != "]" {
			return failf(2, "%v: [: missing matching ]\n", pos)
		}
		args = args[:len(args)-1]
		fallthrough
	case "test":
		parseErr := false
		p := testParser{
			rem: args,
			err: func(err error) {
				r.errf("%v: %v\n", pos, err)
				parseErr = true
			},
		}
		p.next()
		expr := p.classicTest("[", false)
		if parseErr {
			exit.code = 2
			return exit
		}
		exit.oneIf(r.bashTest(ctx, expr, true) == "")
	case "exec":
		// TODO: Consider unix.Exec, i.e. actually replacing
		// the process. It's in theory what a shell should do,
		// but in practice it would kill the entire Go process
		// and it's not available on Windows.
		if len(args) == 0 {
			r.keepRedirs = true
			break
		}
		r.exit.exiting = true
		r.exec(ctx, pos, args)
		exit = r.exit
	case "command":
		show := false
		fp := flagParser{remaining: args}
		for fp.more() {
			switch flag := fp.flag(); flag {
			case "-v":
				show = true
			default:
				return failf(2, "command: invalid option %q\n", flag)
			}
		}
		args := fp.args()
		if len(args) == 0 {
			break
		}
		if !show {
			if IsBuiltin(args[0]) {
				return r.builtin(ctx, pos, args[0], args[1:])
			}
			r.exec(ctx, pos, args)
			exit = r.exit
			return exit
		}
		last := uint8(0)
		for _, arg := range args {
			last = 0
			if r.Funcs[arg] != nil || IsBuiltin(arg) {
				r.outf("%s\n", arg)
			} else if path, err := LookPathDir(r.Dir, r.writeEnv, arg); err == nil {
				r.outf("%s\n", path)
			} else {
				last = 1
			}
		}
		exit.code = last
	case "dirs":
		for i, dir := range slices.Backward(r.dirStack) {
			r.outf("%s", dir)
			if i > 0 {
				r.out(" ")
			}
		}
		r.out("\n")
	case "pushd":
		change := true
		if len(args) > 0 && args[0] == "-n" {
			change = false
			args = args[1:]
		}
		swap := func() string {
			oldtop := r.dirStack[len(r.dirStack)-1]
			top := r.dirStack[len(r.dirStack)-2]
			r.dirStack[len(r.dirStack)-1] = top
			r.dirStack[len(r.dirStack)-2] = oldtop
			return top
		}
		switch len(args) {
		case 0:
			if !change {
				break
			}
			if len(r.dirStack) < 2 {
				return failf(1, "pushd: no other directory\n")
			}
			newtop := swap()
			if code := r.changeDir(ctx, newtop); code != 0 {
				exit.code = code
				return exit
			}
			r.builtin(ctx, syntax.Pos{}, "dirs", nil)
		case 1:
			if change {
				if code := r.changeDir(ctx, args[0]); code != 0 {
					exit.code = code
					return exit
				}
				r.dirStack = append(r.dirStack, r.Dir)
			} else {
				r.dirStack = append(r.dirStack, args[0])
				swap()
			}
			r.builtin(ctx, syntax.Pos{}, "dirs", nil)
		default:
			return failf(2, "pushd: too many arguments\n")
		}
	case "popd":
		change := true
		if len(args) > 0 && args[0] == "-n" {
			change = false
			args = args[1:]
		}
		switch len(args) {
		case 0:
			if len(r.dirStack) < 2 {
				return failf(1, "popd: directory stack empty\n")
			}
			oldtop := r.dirStack[len(r.dirStack)-1]
			r.dirStack = r.dirStack[:len(r.dirStack)-1]
			if change {
				newtop := r.dirStack[len(r.dirStack)-1]
				if code := r.changeDir(ctx, newtop); code != 0 {
					exit.code = code
					return exit
				}
			} else {
				r.dirStack[len(r.dirStack)-1] = oldtop
			}
			r.builtin(ctx, syntax.Pos{}, "dirs", nil)
		default:
			return failf(2, "popd: invalid argument\n")
		}
	case "return":
		if !r.inFunc && !r.inSource {
			return failf(1, "return: can only be done from a func or sourced script\n")
		}
		switch len(args) {
		case 0:
		case 1:
			n, err := strconv.Atoi(args[0])
			if err != nil {
				return failf(2, "invalid return status code: %q\n", args[0])
			}
			exit.code = uint8(n)
		default:
			return failf(2, "return: too many arguments\n")
		}
		exit.returning = true
	case "read":
		var prompt string
		raw := false
		silent := false
		fp := flagParser{remaining: args}
		for fp.more() {
			switch flag := fp.flag(); flag {
			case "-s":
				silent = true
			case "-r":
				raw = true
			case "-p":
				prompt = fp.value()
				if prompt == "" {
					return failf(2, "read: -p: option requires an argument\n")
				}
			default:
				return failf(2, "read: invalid option %q\n", flag)
			}
		}

		args := fp.args()
		for _, name := range args {
			if !syntax.ValidName(name) {
				return failf(2, "read: invalid identifier %q\n", name)
			}
		}

		if prompt != "" {
			r.out(prompt)
		}

		var line []byte
		var err error
		if silent {
			// Note that on Windows, syscall.Stdin is of type uintptr.
			line, err = term.ReadPassword(int(syscall.Stdin))
		} else {
			line, err = r.readLine(ctx, raw)
		}
		if len(args) == 0 {
			args = append(args, shellReplyVar)
		}

		values := expand.ReadFields(r.ecfg, string(line), len(args), raw)
		for i, name := range args {
			val := ""
			if i < len(values) {
				val = values[i]
			}
			r.setVarString(name, val)
		}

		// We can get data back from readLine and an error at the same time, so
		// check err after we process the data.
		if err != nil {
			exit.code = 1
			return exit
		}

	case "getopts":
		if len(args) < 2 {
			return failf(2, "getopts: usage: getopts optstring name [arg ...]\n")
		}
		optind, _ := strconv.Atoi(r.envGet("OPTIND"))
		if optind-1 != r.optState.argidx {
			if optind < 1 {
				optind = 1
			}
			r.optState = getopts{argidx: optind - 1}
		}
		optstr := args[0]
		name := args[1]
		if !syntax.ValidName(name) {
			return failf(2, "getopts: invalid identifier: %q\n", name)
		}
		args = args[2:]
		if len(args) == 0 {
			args = r.Params
		}
		diagnostics := !strings.HasPrefix(optstr, ":")

		opt, optarg, done := r.optState.next(optstr, args)

		r.setVarString(name, string(opt))
		r.delVar("OPTARG")
		switch {
		case opt == '?' && diagnostics && !done:
			r.errf("getopts: illegal option -- %q\n", optarg)
		case opt == ':' && diagnostics:
			r.errf("getopts: option requires an argument -- %q\n", optarg)
		default:
			if optarg != "" {
				r.setVarString("OPTARG", optarg)
			}
		}
		if optind-1 != r.optState.argidx {
			r.setVarString("OPTIND", strconv.FormatInt(int64(r.optState.argidx+1), 10))
		}

		exit.oneIf(done)

	case "shopt":
		mode := ""
		posixOpts := false
		fp := flagParser{remaining: args}
		for fp.more() {
			switch flag := fp.flag(); flag {
			case "-s", "-u":
				mode = flag
			case "-o":
				posixOpts = true
			case "-p", "-q":
				panic(fmt.Sprintf("unhandled shopt flag: %s", flag))
			default:
				return failf(2, "shopt: invalid option %q\n", flag)
			}
		}
		args := fp.args()
		bash := !posixOpts
		if len(args) == 0 {
			if bash {
				for i, opt := range bashOptsTable {
					r.printOptLine(opt.name, r.opts[len(shellOptsTable)+i], opt.supported)
				}
				break
			}
			for i, opt := range &shellOptsTable {
				r.printOptLine(opt.name, r.opts[i], true)
			}
			break
		}
		for _, arg := range args {
			i, opt := r.optByName(arg, bash)
			if opt == nil {
				return failf(1, "shopt: invalid option name %q\n", arg)
			}

			var (
				bo        *bashOpt
				supported = true // default for shell options
			)
			if bash {
				bo = &bashOptsTable[i-len(shellOptsTable)]
				supported = bo.supported
			}

			switch mode {
			case "-s", "-u":
				if bash && !supported {
					return failf(1, "shopt: invalid option name %q %q (%q not supported)\n", arg, r.optStatusText(bo.defaultState), r.optStatusText(!bo.defaultState))
				}
				*opt = mode == "-s"
			default: // ""
				r.printOptLine(arg, *opt, supported)
			}
		}
		r.updateExpandOpts()

	case "alias":
		show := func(name string, als alias) {
			var buf bytes.Buffer
			if len(als.args) > 0 {
				printer := syntax.NewPrinter()
				printer.Print(&buf, &syntax.CallExpr{
					Args: als.args,
				})
			}
			if als.blank {
				buf.WriteByte(' ')
			}
			r.outf("alias %s='%s'\n", name, &buf)
		}

		if len(args) == 0 {
			for name, als := range r.alias {
				show(name, als)
			}
		}
	argsLoop:
		for _, arg := range args {
			name, src, ok := strings.Cut(arg, "=")
			if !ok {
				als, ok := r.alias[name]
				if !ok {
					r.errf("alias: %q not found\n", name)
					continue
				}
				show(name, als)
				continue
			}

			// TODO: parse any CallExpr perhaps, or even any Stmt
			parser := syntax.NewParser()
			var words []*syntax.Word
			for w, err := range parser.WordsSeq(strings.NewReader(src)) {
				if err != nil {
					r.errf("alias: could not parse %q: %v\n", src, err)
					continue argsLoop
				}
				words = append(words, w)
			}

			if r.alias == nil {
				r.alias = make(map[string]alias)
			}
			r.alias[name] = alias{
				args:  words,
				blank: strings.TrimRight(src, " \t") != src,
			}
		}
	case "unalias":
		for _, name := range args {
			delete(r.alias, name)
		}

	case "trap":
		fp := flagParser{remaining: args}
		callback := "-"
		for fp.more() {
			switch flag := fp.flag(); flag {
			case "-l", "-p":
				return failf(2, "trap: %q: NOT IMPLEMENTED flag\n", flag)
			case "-":
				// default signal
			default:
				r.errf("trap: %q: invalid option\n", flag)
				r.errf("trap: usage: trap [-lp] [[arg] signal_spec ...]\n")
				exit.code = 2
				return exit
			}
		}
		args := fp.args()
		switch len(args) {
		case 0:
			// Print non-default signals
			if r.callbackExit != "" {
				r.outf("trap -- %q EXIT\n", r.callbackExit)
			}
			if r.callbackErr != "" {
				r.outf("trap -- %q ERR\n", r.callbackErr)
			}
		case 1:
			// assume it's a signal, the default will be restored
		default:
			callback = args[0]
			args = args[1:]
		}
		// For now, treat both empty and - the same since ERR and EXIT have no
		// default callback.
		if callback == "-" {
			callback = ""
		}
		for _, arg := range args {
			switch arg {
			case "ERR":
				r.callbackErr = callback
			case "EXIT":
				r.callbackExit = callback
			default:
				return failf(2, "trap: %s: invalid signal specification\n", arg)
			}
		}

	case "readarray", "mapfile":
		dropDelim := false
		delim := "\n"
		fp := flagParser{remaining: args}
		for fp.more() {
			switch flag := fp.flag(); flag {
			case "-t":
				// Remove the delim from each line read
				dropDelim = true
			case "-d":
				if len(fp.remaining) == 0 {
					return failf(2, "%s: -d: option requires an argument\n", name)
				}
				delim = fp.value()
				if delim == "" {
					// Bash sets the delim to an ASCII NUL if provided with an empty
					// string.
					delim = "\x00"
				}
			default:
				return failf(2, "%s: invalid option %q\n", name, flag)
			}
		}

		args := fp.args()
		var arrayName string
		switch len(args) {
		case 0:
			arrayName = "MAPFILE"
		case 1:
			if !syntax.ValidName(args[0]) {
				return failf(2, "%s: invalid identifier %q\n", name, args[0])
			}
			arrayName = args[0]
		default:
			return failf(2, "%s: Only one array name may be specified, %v\n", name, args)
		}

		var vr expand.Variable
		vr.Kind = expand.Indexed
		scanner := bufio.NewScanner(r.stdin)
		scanner.Split(mapfileSplit(delim[0], dropDelim))
		for scanner.Scan() {
			vr.List = append(vr.List, scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			return failf(2, "%s: unable to read, %v\n", name, err)
		}
		r.setVar(arrayName, vr)

	default:
		// "umask", "fg", "bg",
		return failf(2, "%s: unimplemented builtin\n", name)
	}
	return exit
}

// mapfileSplit returns a suitable Split function for a [bufio.Scanner];
// the code is mostly stolen from [bufio.ScanLines].
func mapfileSplit(delim byte, dropDelim bool) bufio.SplitFunc {
	return func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}
		if i := bytes.IndexByte(data, delim); i >= 0 {
			// We have a full newline-terminated line.
			if dropDelim {
				return i + 1, data[0:i], nil
			} else {
				return i + 1, data[0 : i+1], nil
			}
		}
		// If we're at EOF, we have a final, non-terminated line. Return it.
		if atEOF {
			return len(data), data, nil
		}
		// Request more data.
		return 0, nil, nil
	}
}

func (r *Runner) printOptLine(name string, enabled, supported bool) {
	state := r.optStatusText(enabled)
	if supported {
		r.outf("%s\t%s\n", name, state)
		return
	}
	r.outf("%s\t%s\t(%q not supported)\n", name, state, r.optStatusText(!enabled))
}

func (r *Runner) readLine(ctx context.Context, raw bool) ([]byte, error) {
	if r.stdin == nil {
		return nil, errors.New("interp: can't read, there's no stdin")
	}

	var line []byte
	esc := false

	stopc := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		r.stdin.SetReadDeadline(time.Now())
		close(stopc)
	})
	defer func() {
		if !stop() {
			// The AfterFunc was started.
			// Wait for it to complete, and reset the file's deadline.
			<-stopc
			r.stdin.SetReadDeadline(time.Time{})
		}
	}()
	for {
		var buf [1]byte
		n, err := r.stdin.Read(buf[:])
		if n > 0 {
			b := buf[0]
			switch {
			case !raw && b == '\\':
				line = append(line, b)
				esc = !esc
			case !raw && b == '\n' && esc:
				// line continuation
				line = line[len(line)-1:]
				esc = false
			case b == '\n':
				return line, nil
			default:
				line = append(line, b)
				esc = false
			}
		}
		if err != nil {
			return line, err
		}
	}
}

func (r *Runner) changeDir(ctx context.Context, path string) uint8 {
	path = cmp.Or(path, ".")
	path = r.absPath(path)
	info, err := r.stat(ctx, path)
	if err != nil || !info.IsDir() {
		return 1
	}
	if r.access(ctx, path, access_X_OK) != nil {
		return 1
	}
	r.Dir = path
	r.setVarString("OLDPWD", r.envGet("PWD"))
	r.setVarString("PWD", path)
	return 0
}

func absPath(dir, path string) string {
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	return filepath.Clean(path) // TODO: this clean is likely unnecessary
}

func (r *Runner) absPath(path string) string {
	return absPath(r.Dir, path)
}

// flagParser is used to parse builtin flags.
//
// It's similar to the getopts implementation, but with some key differences.
// First, the API is designed for Go loops, making it easier to use directly.
// Second, it doesn't require the awkward ":ab" syntax that getopts uses.
// Third, it supports "-a" flags as well as "+a".
type flagParser struct {
	current   string
	remaining []string
}

func (p *flagParser) more() bool {
	if p.current != "" {
		// We're still parsing part of "-ab".
		return true
	}
	if len(p.remaining) == 0 {
		// Nothing left.
		p.remaining = nil
		return false
	}
	arg := p.remaining[0]
	if arg == "--" {
		// We explicitly stop parsing flags.
		p.remaining = p.remaining[1:]
		return false
	}
	if len(arg) == 0 || (arg[0] != '-' && arg[0] != '+') {
		// The next argument is not a flag.
		return false
	}
	// More flags to come.
	return true
}

func (p *flagParser) flag() string {
	arg := p.current
	if arg == "" {
		arg = p.remaining[0]
		p.remaining = p.remaining[1:]
	} else {
		p.current = ""
	}
	if len(arg) > 2 {
		// We have "-ab", so return "-a" and keep "-b".
		p.current = arg[:1] + arg[2:]
		arg = arg[:2]
	}
	return arg
}

func (p *flagParser) value() string {
	if len(p.remaining) == 0 {
		return ""
	}
	arg := p.remaining[0]
	p.remaining = p.remaining[1:]
	return arg
}

func (p *flagParser) args() []string { return p.remaining }

type getopts struct {
	argidx  int
	runeidx int
}

func (g *getopts) next(optstr string, args []string) (opt rune, optarg string, done bool) {
	if len(args) == 0 || g.argidx >= len(args) {
		return '?', "", true
	}
	arg := []rune(args[g.argidx])
	if len(arg) < 2 || arg[0] != '-' || arg[1] == '-' {
		return '?', "", true
	}

	opts := arg[1:]
	opt = opts[g.runeidx]
	if g.runeidx+1 < len(opts) {
		g.runeidx++
	} else {
		g.argidx++
		g.runeidx = 0
	}

	i := strings.IndexRune(optstr, opt)
	if i < 0 {
		// invalid option
		return '?', string(opt), false
	}

	if i+1 < len(optstr) && optstr[i+1] == ':' {
		if g.argidx >= len(args) {
			// missing argument
			return ':', string(opt), false
		}
		optarg = args[g.argidx]
		g.argidx++
		g.runeidx = 0
	}

	return opt, optarg, false
}

// optStatusText returns a shell option's status text display
func (r *Runner) optStatusText(status bool) string {
	if status {
		return "on"
	}
	return "off"
}
