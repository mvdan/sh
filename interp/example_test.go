// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp_test

import (
	"context"
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

func ExampleModuleExec() {
	src := `
		ls example_test.* || echo "ls failed"
		rm example_test.* || echo "rm failed"
	`
	file, _ := syntax.NewParser().Parse(strings.NewReader(src), "")
	exec := func(ctx context.Context, path string, args []string) error {
		switch args[0] {
		case "ls":
			// whitelist the "ls" program
			return interp.DefaultExec(ctx, path, args)
		default:
			// refuse to run any other program
			return interp.ExitStatus(1)
		}
	}
	runner, _ := interp.New(
		interp.StdIO(nil, os.Stdout, os.Stdout),
		interp.Module(interp.ModuleExec(exec)),
	)
	runner.Run(context.TODO(), file)
	// Output:
	// example_test.go
	// rm failed
}
