// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

// gosh is a proof of concept shell built on top of [interp].
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/term"

	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

var command = flag.String("c", "", "command to be executed")

func main() {
	flag.Parse()
	err := runAll()
	var es interp.ExitStatus
	if errors.As(err, &es) {
		os.Exit(int(es))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runAll() error {
	ctx, _ := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	r, err := interp.New(interp.Interactive(true), interp.StdIO(os.Stdin, os.Stdout, os.Stderr))
	if err != nil {
		return err
	}

	if *command != "" {
		return run(ctx, r, strings.NewReader(*command), "")
	}
	if flag.NArg() == 0 {
		if term.IsTerminal(int(os.Stdin.Fd())) {
			return runInteractive(ctx, r, os.Stdin, os.Stdout, os.Stderr)
		}
		return run(ctx, r, os.Stdin, "")
	}
	for _, path := range flag.Args() {
		if err := runPath(ctx, r, path); err != nil {
			return err
		}
	}
	return nil
}

func run(ctx context.Context, r *interp.Runner, reader io.Reader, name string) error {
	prog, err := syntax.NewParser().Parse(reader, name)
	if err != nil {
		return err
	}
	r.Reset()
	return r.Run(ctx, prog)
}

func runPath(ctx context.Context, r *interp.Runner, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return run(ctx, r, f, path)
}

func runInteractive(ctx context.Context, r *interp.Runner, stdin io.Reader, stdout, stderr io.Writer) error {
	parser := syntax.NewParser()
	fmt.Fprintf(stdout, "$ ")
	var runErr error
	fn := func(stmts []*syntax.Stmt) bool {
		if parser.Incomplete() {
			fmt.Fprintf(stdout, "> ")
			return true
		}
		for _, stmt := range stmts {
			runErr = r.Run(ctx, stmt)
			if r.Exited() {
				return false
			}
		}
		fmt.Fprintf(stdout, "$ ")
		return true
	}
	if err := parser.Interactive(stdin, fn); err != nil {
		return err
	}
	return runErr
}
