// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp_test

import (
	"context"
	"fmt"
	"os"
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

func ExampleExecModule() {
	src := "echo foo; join ! foo bar baz; missing-program bar"
	file, _ := syntax.NewParser().Parse(strings.NewReader(src), "")

	join := func(next interp.ExecModule) interp.ExecModule {
		return func(ctx context.Context, args []string) error {
			if args[0] == "join" {
				mc, _ := interp.FromModuleContext(ctx)
				fmt.Fprintln(mc.Stdout, strings.Join(args[2:], args[1]))
				return nil
			}
			return next(ctx, args)
		}
	}
	notInstalled := func(next interp.ExecModule) interp.ExecModule {
		return func(ctx context.Context, args []string) error {
			mc, _ := interp.FromModuleContext(ctx)
			if _, err := interp.LookPath(mc.Env, args[0]); err != nil {
				fmt.Printf("%s is not installed\n", args[0])
				return interp.ExitStatus(1)
			}
			return next(ctx, args)
		}
	}
	runner, _ := interp.New(
		interp.StdIO(nil, os.Stdout, os.Stdout),
		interp.WithExecModules(join, notInstalled),
	)
	runner.Run(context.TODO(), file)
	// Output:
	// foo
	// foo!bar!baz
	// missing-program is not installed
}
