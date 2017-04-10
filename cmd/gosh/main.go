// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/mvdan/sh/interp"
	"github.com/mvdan/sh/syntax"
)

var (
	posix = flag.Bool("p", false, "parse POSIX shell code instead of bash")

	parseMode syntax.ParseMode
)

func main() {
	flag.Parse()

	if *posix {
		parseMode |= syntax.PosixConformant
	}
	for _, path := range flag.Args() {
		if err := runPath(path); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

func runPath(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	prog, err := syntax.Parse(f, path, parseMode)
	if err != nil {
		return err
	}
	r := interp.Runner{
		File:   prog,
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	return r.Run()
}
