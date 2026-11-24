// Copyright (c) 2025, Andrey Nering <andrey@nering.com.br>
// See LICENSE for licensing information

// Package coreutils provides a middleware for the interpreter that handles
// core utils commands with charm.land/turtle/coreutils.
//
// This is particularly useful to keep the max compability on Windows where
// these core utils are not available, unless when installed manually by the
// user.
package coreutils

import (
	"context"
	"errors"
	"io"
	"path/filepath"

	turtle "charm.land/turtle/coreutils"
	"charm.land/turtle/coreutils/programs/base32"
	"charm.land/turtle/coreutils/programs/base64"
	"charm.land/turtle/coreutils/programs/basename"
	"charm.land/turtle/coreutils/programs/cat"
	"charm.land/turtle/coreutils/programs/chgrp"
	"charm.land/turtle/coreutils/programs/chmod"
	"charm.land/turtle/coreutils/programs/chown"
	"charm.land/turtle/coreutils/programs/cksum"
	"charm.land/turtle/coreutils/programs/cmp"
	"charm.land/turtle/coreutils/programs/comm"
	"charm.land/turtle/coreutils/programs/compress"
	"charm.land/turtle/coreutils/programs/cp"
	"charm.land/turtle/coreutils/programs/csplit"
	"charm.land/turtle/coreutils/programs/cut"
	"charm.land/turtle/coreutils/programs/date"
	"charm.land/turtle/coreutils/programs/dd"
	"charm.land/turtle/coreutils/programs/diff"
	"charm.land/turtle/coreutils/programs/dirname"
	"charm.land/turtle/coreutils/programs/du"
	"charm.land/turtle/coreutils/programs/env"
	"charm.land/turtle/coreutils/programs/expand"
	"charm.land/turtle/coreutils/programs/expr"
	"charm.land/turtle/coreutils/programs/find"
	"charm.land/turtle/coreutils/programs/fold"
	"charm.land/turtle/coreutils/programs/grep"
	"charm.land/turtle/coreutils/programs/gunzip"
	"charm.land/turtle/coreutils/programs/gzcat"
	"charm.land/turtle/coreutils/programs/gzip"
	"charm.land/turtle/coreutils/programs/head"
	"charm.land/turtle/coreutils/programs/id"
	"charm.land/turtle/coreutils/programs/install"
	"charm.land/turtle/coreutils/programs/join"
	"charm.land/turtle/coreutils/programs/link"
	"charm.land/turtle/coreutils/programs/ln"
	"charm.land/turtle/coreutils/programs/logname"
	"charm.land/turtle/coreutils/programs/ls"
	"charm.land/turtle/coreutils/programs/md5sum"
	"charm.land/turtle/coreutils/programs/mkdir"
	"charm.land/turtle/coreutils/programs/mktemp"
	"charm.land/turtle/coreutils/programs/mv"
	"charm.land/turtle/coreutils/programs/nice"
	"charm.land/turtle/coreutils/programs/nl"
	"charm.land/turtle/coreutils/programs/nohup"
	"charm.land/turtle/coreutils/programs/nproc"
	"charm.land/turtle/coreutils/programs/od"
	"charm.land/turtle/coreutils/programs/paste"
	"charm.land/turtle/coreutils/programs/patch"
	"charm.land/turtle/coreutils/programs/pr"
	"charm.land/turtle/coreutils/programs/printenv"
	"charm.land/turtle/coreutils/programs/readlink"
	"charm.land/turtle/coreutils/programs/realpath"
	"charm.land/turtle/coreutils/programs/rm"
	"charm.land/turtle/coreutils/programs/rmdir"
	"charm.land/turtle/coreutils/programs/sed"
	"charm.land/turtle/coreutils/programs/seq"
	"charm.land/turtle/coreutils/programs/sha1sum"
	"charm.land/turtle/coreutils/programs/sha224sum"
	"charm.land/turtle/coreutils/programs/sha256sum"
	"charm.land/turtle/coreutils/programs/sha384sum"
	"charm.land/turtle/coreutils/programs/sha512sum"
	"charm.land/turtle/coreutils/programs/shasum"
	"charm.land/turtle/coreutils/programs/shuf"
	"charm.land/turtle/coreutils/programs/sleep"
	"charm.land/turtle/coreutils/programs/sort"
	"charm.land/turtle/coreutils/programs/split"
	"charm.land/turtle/coreutils/programs/stat"
	stringsprog "charm.land/turtle/coreutils/programs/strings"
	"charm.land/turtle/coreutils/programs/sync"
	"charm.land/turtle/coreutils/programs/tac"
	"charm.land/turtle/coreutils/programs/tail"
	"charm.land/turtle/coreutils/programs/tar"
	"charm.land/turtle/coreutils/programs/tee"
	timeprog "charm.land/turtle/coreutils/programs/time"
	"charm.land/turtle/coreutils/programs/timeout"
	"charm.land/turtle/coreutils/programs/touch"
	"charm.land/turtle/coreutils/programs/tr"
	"charm.land/turtle/coreutils/programs/truncate"
	"charm.land/turtle/coreutils/programs/uname"
	"charm.land/turtle/coreutils/programs/uncompress"
	"charm.land/turtle/coreutils/programs/unexpand"
	"charm.land/turtle/coreutils/programs/uniq"
	"charm.land/turtle/coreutils/programs/unlink"
	"charm.land/turtle/coreutils/programs/uudecode"
	"charm.land/turtle/coreutils/programs/uuencode"
	"charm.land/turtle/coreutils/programs/wc"
	"charm.land/turtle/coreutils/programs/which"
	"charm.land/turtle/coreutils/programs/whoami"
	"charm.land/turtle/coreutils/programs/xargs"
	"charm.land/turtle/coreutils/programs/yes"
	"charm.land/turtle/coreutils/programs/zcat"

	"charm.land/turtle/coreutils/programs/echo"
	falseprog "charm.land/turtle/coreutils/programs/false"
	"charm.land/turtle/coreutils/programs/printf"
	"charm.land/turtle/coreutils/programs/pwd"
	"charm.land/turtle/coreutils/programs/test"
	trueprog "charm.land/turtle/coreutils/programs/true"

	shexpand "mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/interp"
)

var commandBuilders = map[string]turtle.NewFunc{
	"base32":     base32.New,
	"base64":     base64.New,
	"basename":   basename.New,
	"cat":        cat.New,
	"chgrp":      chgrp.New,
	"chmod":      chmod.New,
	"chown":      chown.New,
	"cksum":      cksum.New,
	"cmp":        cmp.New,
	"comm":       comm.New,
	"compress":   compress.New,
	"cp":         cp.New,
	"csplit":     csplit.New,
	"cut":        cut.New,
	"date":       date.New,
	"dd":         dd.New,
	"diff":       diff.New,
	"dirname":    dirname.New,
	"du":         du.New,
	"env":        env.New,
	"expand":     expand.New,
	"expr":       expr.New,
	"find":       find.New,
	"fold":       fold.New,
	"grep":       grep.New,
	"gunzip":     gunzip.New,
	"gzcat":      gzcat.New,
	"gzip":       gzip.New,
	"head":       head.New,
	"id":         id.New,
	"install":    install.New,
	"join":       join.New,
	"link":       link.New,
	"ln":         ln.New,
	"logname":    logname.New,
	"ls":         ls.New,
	"md5sum":     md5sum.New,
	"mkdir":      mkdir.New,
	"mktemp":     mktemp.New,
	"mv":         mv.New,
	"nice":       nice.New,
	"nl":         nl.New,
	"nohup":      nohup.New,
	"nproc":      nproc.New,
	"od":         od.New,
	"paste":      paste.New,
	"patch":      patch.New,
	"pr":         pr.New,
	"printenv":   printenv.New,
	"readlink":   readlink.New,
	"realpath":   realpath.New,
	"rm":         rm.New,
	"rmdir":      rmdir.New,
	"sed":        sed.New,
	"seq":        seq.New,
	"sha1sum":    sha1sum.New,
	"sha224sum":  sha224sum.New,
	"sha256sum":  sha256sum.New,
	"sha384sum":  sha384sum.New,
	"sha512sum":  sha512sum.New,
	"shasum":     shasum.New,
	"shuf":       shuf.New,
	"sleep":      sleep.New,
	"sort":       sort.New,
	"split":      split.New,
	"stat":       stat.New,
	"strings":    stringsprog.New,
	"sync":       sync.New,
	"tac":        tac.New,
	"tail":       tail.New,
	"tar":        tar.New,
	"tee":        tee.New,
	"time":       timeprog.New,
	"timeout":    timeout.New,
	"touch":      touch.New,
	"tr":         tr.New,
	"truncate":   truncate.New,
	"uname":      uname.New,
	"uncompress": uncompress.New,
	"unexpand":   unexpand.New,
	"uniq":       uniq.New,
	"unlink":     unlink.New,
	"uudecode":   uudecode.New,
	"uuencode":   uuencode.New,
	"wc":         wc.New,
	"which":      which.New,
	"whoami":     whoami.New,
	"xargs":      xargs.New,
	"yes":        yes.New,
	"zcat":       zcat.New,
}

// nestedBuilders also includes utilities shadowed by interpreter builtins
// at the top level. Utilities invoking other programs go through Runtime.Exec
// rather than the interpreter's builtin dispatch, so they still need them.
var nestedBuilders = func() map[string]turtle.NewFunc {
	builders := make(map[string]turtle.NewFunc, len(commandBuilders)+6)
	for name, build := range commandBuilders {
		builders[name] = build
	}
	builders["echo"] = echo.New
	builders["false"] = falseprog.New
	builders["printf"] = printf.New
	builders["pwd"] = pwd.New
	builders["test"] = test.New
	builders["true"] = trueprog.New
	return builders
}()

type handler struct {
	next interp.ExecHandlerFunc
	hc   interp.HandlerContext
}

// ExecHandler returns an [interp.ExecHandlerFunc] middleware that handles core
// utils commands. Utilities shadowed by interpreter builtins are excluded.
//
// Keep in mind that this middleware has priority over the core utils available
// by the system. You may want to use only on Windows to ensure that the system
// core utils are used on other platforms, like macOS and Linux.
//
// Utilities which invoke other programs resolve those names through the same
// table, and run external programs through the remaining handler chain with
// the requested directory, environment, and streams.
func ExecHandler(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(ctx context.Context, args []string) error {
		program, programArgs := args[0], args[1:]
		build, ok := commandBuilders[program]
		if !ok {
			return next(ctx, args)
		}

		hc := interp.HandlerCtx(ctx)
		h := &handler{next: next, hc: hc}
		rt := newRuntime(hc.Dir, environ(hc.Env), hc.Stdin, hc.Stdout, hc.Stderr, h.exec)
		if err := build(rt).Run(ctx, programArgs...); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			status := 1
			if exitErr, ok := errors.AsType[*turtle.ExitError](err); ok {
				status = exitErr.Code
			}
			return utilityError(err, status)
		}
		return nil
	}
}

func newRuntime(dir string, env []string, stdin io.Reader, stdout, stderr io.Writer, exec turtle.ExecFunc) *turtle.Runtime {
	rt := turtle.SystemRuntime(dir)
	rt.Stdin, rt.Stdout, rt.Stderr = stdin, stdout, stderr
	rt.Environ = func() []string { return env }
	rt.Exec = exec
	return rt
}

func (h *handler) exec(ctx context.Context, opts turtle.ExecOpts) (int, error) {
	if build, ok := nestedBuilders[opts.Name]; ok {
		return h.runNested(ctx, build, opts)
	}

	hc := h.hc
	hc.Dir = h.resolveDir(opts.Dir)
	hc.Env = shexpand.ListEnviron(opts.Env...)
	hc.Stdin = opts.Stdin
	if hc.Stdin == nil {
		hc.Stdin = nullReader{}
	}
	if opts.Stdout != nil {
		hc.Stdout = opts.Stdout
	}
	if opts.Stderr != nil {
		hc.Stderr = opts.Stderr
	}
	ctx = interp.WithHandlerContext(ctx, hc)
	args := append([]string{opts.Name}, opts.Args...)
	if err := h.next(ctx, args); err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		if code, ok := interp.IsExitStatus(err); ok {
			return int(code), nil
		}
		return 126, err
	}
	return 0, nil
}

func (h *handler) runNested(ctx context.Context, build turtle.NewFunc, opts turtle.ExecOpts) (int, error) {
	dir := h.resolveDir(opts.Dir)
	env := opts.Env
	if env == nil {
		env = []string{}
	}
	stdout, stderr := opts.Stdout, opts.Stderr
	if stdout == nil {
		stdout = h.hc.Stdout
	}
	if stderr == nil {
		stderr = h.hc.Stderr
	}
	rt := newRuntime(dir, env, opts.Stdin, stdout, stderr, h.exec)
	err := build(rt).Run(ctx, opts.Args...)
	if err == nil {
		return 0, nil
	}
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	if exitErr, ok := errors.AsType[*turtle.ExitError](err); ok {
		return exitErr.Code, exitErr.Err
	}
	return 1, err
}

func (h *handler) resolveDir(dir string) string {
	if dir == "" {
		return h.hc.Dir
	}
	if !filepath.IsAbs(dir) {
		return filepath.Join(h.hc.Dir, dir)
	}
	return dir
}

type nullReader struct{}

func (nullReader) Read([]byte) (int, error) { return 0, io.EOF }

func environ(environ shexpand.Environ) []string {
	var entries []string
	environ.Each(func(name string, vr shexpand.Variable) bool {
		if vr.Set && vr.Exported && vr.Kind == shexpand.String {
			entries = append(entries, name+"="+vr.Str)
		}
		return true
	})
	return entries
}
