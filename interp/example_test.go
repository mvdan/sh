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
	src := "echo foo; missing-program bar"
	file, _ := syntax.NewParser().Parse(strings.NewReader(src), "")
	exec := func(next interp.ExecModule) interp.ExecModule {
		return func(ctx context.Context, path string, args []string) error {
			if path == "" {
				fmt.Printf("%s is not installed\n", args[0])
				return interp.ExitStatus(1)
			}
			return next(ctx, path, args)
		}
	}
	runner, _ := interp.New(
		interp.StdIO(nil, os.Stdout, os.Stdout),
		interp.WithExecModules(exec),
	)
	runner.Run(context.TODO(), file)
	// Output:
	// foo
	// missing-program is not installed
}
