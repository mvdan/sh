// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"

	"github.com/mvdan/sh"
)

var (
	write = flag.Bool("w", false, "write result to file instead of stdout")
	list  = flag.Bool("l", false, "list files whose formatting differs from shfmt's")
)

func main() {
	flag.Parse()
	if flag.NArg() == 0 {
		if err := formatStdin(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	anyErr := false
	for _, path := range flag.Args() {
		if err := work(path); err != nil {
			anyErr = true
			fmt.Fprintln(os.Stderr, err)
		}
	}
	if anyErr {
		os.Exit(1)
	}
}

func formatStdin() error {
	if *write || *list {
		return fmt.Errorf("-w and -l can only be used on files")
	}
	prog, err := sh.Parse(os.Stdin, "", sh.ParseComments)
	if err != nil {
		return err
	}
	return sh.Fprint(os.Stdout, prog)
}

var (
	hidden    = regexp.MustCompile(`^\.[^/.]`)
	shellFile = regexp.MustCompile(`^.*\.(sh|bash)$`)
)

func work(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return formatPath(path)
	}
	return filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if hidden.MatchString(path) {
			return filepath.SkipDir
		}
		if info.IsDir() || !shellFile.MatchString(path) {
			return nil
		}
		return formatPath(path)
	})
}

func empty(f *os.File) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	_, err := f.Seek(0, 0)
	return err
}

func formatPath(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	prog, err := sh.Parse(f, path, sh.ParseComments)
	if err != nil {
		return err
	}
	var orig string
	if *list {
		if _, err := f.Seek(0, 0); err != nil {
			return err
		}
		b, err := ioutil.ReadAll(f)
		if err != nil {
			return err
		}
		orig = string(b)
	}
	switch {
	case *list && *write:
		var buf bytes.Buffer
		if err := sh.Fprint(&buf, prog); err != nil {
			return err
		}
		if buf.String() != orig {
			fmt.Println(path)
		}
		if err := empty(f); err != nil {
			return err
		}
		_, err := io.Copy(f, &buf)
		if err != nil {
			return err
		}
		return f.Close()
	case *write:
		if err := empty(f); err != nil {
			return err
		}
		if err := sh.Fprint(f, prog); err != nil {
			return err
		}
		return f.Close()
	case *list:
		var buf bytes.Buffer
		if err := sh.Fprint(&buf, prog); err != nil {
			return err
		}
		if buf.String() != orig {
			fmt.Println(path)
		}
		f.Close()
	default:
		f.Close()
		return sh.Fprint(os.Stdout, prog)
	}
	return nil
}
