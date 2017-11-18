// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main // import "mvdan.cc/sh/cmd/gosh"

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/crypto/ssh/terminal"

	"mvdan.cc/sh/interp"
	"mvdan.cc/sh/syntax"
)

var (
	command = flag.String("c", "", "command to be executed")

	parser *syntax.Parser

	runner = interp.Runner{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
)

func main() {
	flag.Parse()
	if err := runAll(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runAll() error {
	parser = syntax.NewParser()
	if *command != "" {
		return run(strings.NewReader(*command), "")
	}
	if flag.NArg() == 0 {
		if terminal.IsTerminal(int(os.Stdin.Fd())) {
			return interactive()
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
	runner.Reset()
	return runner.Run(prog)
}

type promptReader struct {
	io.Reader
	first bool
}

func (pr *promptReader) Read(p []byte) (int, error) {
	if pr.first {
		fmt.Printf("$ ")
		pr.first = false
	} else {
		fmt.Printf("> ")
	}
	return pr.Reader.Read(p)
}

func interactive() error {
	r := &promptReader{os.Stdin, true}
	runner.Reset()
	fn := func(s *syntax.Stmt) bool {
		if err := runner.Stmt(s); err != nil {
			code, ok := err.(interp.ExitCode)
			if ok {
				os.Exit(int(code))
			}
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		r.first = true
		return true
	}
	return parser.Stmts(r, fn)
}
