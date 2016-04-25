// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/mvdan/sh"
)

func main() {
	flag.Parse()
	anyErr := false
	for _, path := range flag.Args() {
		if err := format(path); err != nil {
			anyErr = true
			fmt.Fprintln(os.Stderr, err)
		}
	}
	if anyErr {
		os.Exit(1)
	}
}

func format(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	prog, err := sh.Parse(f, path)
	if err != nil {
		return err
	}
	_ = prog
	return nil
}
