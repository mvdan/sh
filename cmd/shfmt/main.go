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
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	prog, err := sh.Parse(f, path)
	if err != nil {
		return err
	}
	f.Truncate(0)
	f.Seek(0, 0)
	return sh.Fprint(f, prog)
}
