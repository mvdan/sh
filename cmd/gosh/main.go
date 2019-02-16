// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/crypto/ssh/terminal"

	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

var (
	command = flag.String("c", "", "command to be executed")

	parser = syntax.NewParser()

	mainRunner, _ = interp.New(interp.StdIO(os.Stdin, os.Stdout, os.Stderr))
)

func main() {
	flag.Parse()
	switch err := runAll().(type) {
	case nil:
	case interp.ShellExitStatus:
		os.Exit(int(err))
	case interp.ExitStatus:
		os.Exit(int(err))
	default:
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runAll() error {
	if *command != "" {
		return run(strings.NewReader(*command), "")
	}
	if flag.NArg() == 0 {
		if terminal.IsTerminal(int(os.Stdin.Fd())) {
			return interactive(mainRunner)
		}
		return run(os.Stdin, "")
	}
	for _, path := range flag.Args() {
		if err := runPath(path); err != nil {
			return err
		}
	}
	return nil
}

func runPath(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return run(f, path)
}

func run(reader io.Reader, name string) error {
	prog, err := parser.Parse(reader, name)
	if err != nil {
		return err
	}
	mainRunner.Reset()
	ctx := context.Background()
	return mainRunner.Run(ctx, prog)
}

func interactive(runner *interp.Runner) error {
	fmt.Fprintf(runner.Stdout, "$ ")
	fn := func(stmts []*syntax.Stmt) bool {
		if parser.Incomplete() {
			fmt.Fprintf(runner.Stdout, "> ")
			return true
		}
		ctx := context.Background()
		for _, stmt := range stmts {
			switch err := runner.Run(ctx, stmt).(type) {
			case nil:
			case interp.ShellExitStatus:
				os.Exit(int(err))
			case interp.ExitStatus:
			default:
				fmt.Fprintln(runner.Stderr, err)
				os.Exit(1)
			}
		}
		fmt.Fprintf(runner.Stdout, "$ ")
		return true
	}
	return parser.Interactive(runner.Stdin, fn)
}
