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
	for _, path := range flag.Args() {
		if err := format(path); err != nil {
			errExit(err)
		}
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
	fmt.Printf("Parsed.\nTODO: fmt\n")
	return nil
}

func errExit(err error) {
	fmt.Fprintf(os.Stderr, "%v\n", err)
	os.Exit(1)
}
