// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

func Example() {
	src := `
		foo=abc
		for i in 1 2 3; do
			foo+=$i
		done
		let bar=(2 + 3)
		echo $foo $bar
		echo $GLOBAL
	`
	file, _ := syntax.NewParser().Parse(strings.NewReader(src), "")
	runner, _ := interp.New(
		interp.Env(expand.ListEnviron("GLOBAL=global_value")),
		interp.StdIO(nil, os.Stdout, os.Stdout),
	)
	runner.Run(context.TODO(), file)
	// Output:
	// abc123 5
	// global_value
}

func ExampleExecHandlers() {
	src := "echo foo; join ! foo bar baz; missing-program bar"
	file, _ := syntax.NewParser().Parse(strings.NewReader(src), "")

	execJoin := func(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
		return func(ctx context.Context, args []string) error {
			hc := interp.HandlerCtx(ctx)
			if args[0] == "join" {
				fmt.Fprintln(hc.Stdout, strings.Join(args[2:], args[1]))
				return nil
			}
			return next(ctx, args)
		}
	}
	execNotInstalled := func(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
		return func(ctx context.Context, args []string) error {
			hc := interp.HandlerCtx(ctx)
			if _, err := interp.LookPathDir(hc.Dir, hc.Env, args[0]); err != nil {
				fmt.Printf("%s is not installed\n", args[0])
				return interp.NewExitStatus(1)
			}
			return next(ctx, args)
		}
	}
	runner, _ := interp.New(
		interp.StdIO(nil, os.Stdout, os.Stdout),
		interp.ExecHandlers(execJoin, execNotInstalled),
	)
	runner.Run(context.TODO(), file)
	// Output:
	// foo
	// foo!bar!baz
	// missing-program is not installed
}

func ExampleOpenHandler() {
	src := "echo foo; echo bar >/dev/null"
	file, _ := syntax.NewParser().Parse(strings.NewReader(src), "")

	open := func(ctx context.Context, path string, flag int, perm os.FileMode) (io.ReadWriteCloser, error) {
		if runtime.GOOS == "windows" && path == "/dev/null" {
			path = "NUL"
		}
		return interp.DefaultOpenHandler()(ctx, path, flag, perm)
	}
	runner, _ := interp.New(
		interp.StdIO(nil, os.Stdout, os.Stdout),
		interp.OpenHandler(open),
	)
	runner.Run(context.TODO(), file)
	// Output:
	// foo
}
