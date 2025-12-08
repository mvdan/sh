// Copyright (c) 2025, Andrey Nering <andrey@nering.com.br>
// See LICENSE for licensing information

// Package coreutils provides a middleware for the interpreter that handles
// core utils commands like cat, chmod, cp, find, ls, mkdir, mv, rm, touch
// and xargs.
//
// This is particularly useful to keep the max compability on Windows where
// these core utils are not available, unless when installed manually by the
// user.
package coreutils

import (
	"context"

	"github.com/u-root/u-root/pkg/core"
	"github.com/u-root/u-root/pkg/core/base64"
	"github.com/u-root/u-root/pkg/core/cat"
	"github.com/u-root/u-root/pkg/core/chmod"
	"github.com/u-root/u-root/pkg/core/cp"
	"github.com/u-root/u-root/pkg/core/find"
	"github.com/u-root/u-root/pkg/core/gzip"
	"github.com/u-root/u-root/pkg/core/ls"
	"github.com/u-root/u-root/pkg/core/mkdir"
	"github.com/u-root/u-root/pkg/core/mktemp"
	"github.com/u-root/u-root/pkg/core/mv"
	"github.com/u-root/u-root/pkg/core/rm"
	"github.com/u-root/u-root/pkg/core/shasum"
	"github.com/u-root/u-root/pkg/core/tar"
	"github.com/u-root/u-root/pkg/core/touch"
	"github.com/u-root/u-root/pkg/core/xargs"
	"mvdan.cc/sh/v3/interp"
)

var commandBuilders = map[string]func() core.Command{
	"cat":    func() core.Command { return cat.New() },
	"chmod":  func() core.Command { return chmod.New() },
	"cp":     func() core.Command { return cp.New() },
	"find":   func() core.Command { return find.New() },
	"ls":     func() core.Command { return ls.New() },
	"mkdir":  func() core.Command { return mkdir.New() },
	"mv":     func() core.Command { return mv.New() },
	"rm":     func() core.Command { return rm.New() },
	"touch":  func() core.Command { return touch.New() },
	"xargs":  func() core.Command { return xargs.New() },
	"base64": func() core.Command { return base64.New() },
	"gzcat":  func() core.Command { return gzip.New("gzcat") },
	"gzip":   func() core.Command { return gzip.New("gzip") },
	"gunzip": func() core.Command { return gzip.New("gunzip") },
	"mktemp": func() core.Command { return mktemp.New() },
	"shasum": func() core.Command { return shasum.New() },
	"tar":    func() core.Command { return tar.New() },
}

// ExecHandler returns an [interp.ExecHandlerFunc] middleware that handles core
// utils commands.
//
// Keep in mind that this middleware has priority over the core utils available
// by the system. You may want to use only on Windows to ensure that the system
// core utils are used on other platforms, like macOS and Linux.
func ExecHandler(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(ctx context.Context, args []string) error {
		program, programArgs := args[0], args[1:]

		newCoreUtil, ok := commandBuilders[program]
		if !ok {
			return next(ctx, args)
		}

		c := interp.HandlerCtx(ctx)

		cmd := newCoreUtil()
		cmd.SetIO(c.Stdin, c.Stdout, c.Stderr)
		cmd.SetWorkingDir(c.Dir)
		cmd.SetLookupEnv(func(key string) (string, bool) {
			v := c.Env.Get(key)
			return v.Str, v.Set
		})
		return cmd.RunContext(ctx, programArgs...)
	}
}
