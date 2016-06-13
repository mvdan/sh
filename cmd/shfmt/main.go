// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mvdan/sh"
)

var (
	write  = flag.Bool("w", false, "write result to file instead of stdout")
	list   = flag.Bool("l", false, "list files whose formatting differs from shfmt's")
	indent = flag.Int("i", 0, "indent: 0 for tabs (default), >0 for number of spaces")
)

var config sh.PrintConfig

func main() {
	flag.Parse()
	config.Spaces = *indent
	if flag.NArg() == 0 {
		if err := formatStdin(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	anyErr := false
	onError := func(err error) {
		anyErr = true
		fmt.Fprintln(os.Stderr, err)
	}
	for _, path := range flag.Args() {
		if err := walk(path, onError); err != nil {
			onError(err)
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
	return config.Fprint(os.Stdout, prog)
}

var (
	hidden    = regexp.MustCompile(`^\.[^/.]`)
	shellFile = regexp.MustCompile(`^.*\.(sh|bash)$`)
	shebang   = regexp.MustCompile(`^#!/(usr/)?bin/(env *)?(sh|bash)`)
)

func walk(path string, onError func(error)) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return formatPath(path, 0, true)
	}
	return filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if hidden.MatchString(path) {
			return filepath.SkipDir
		}
		if info.IsDir() {
			return nil
		}
		if err := formatPath(path, info.Size(), false); err != nil {
			onError(err)
		}
		return nil
	})
}

func empty(f *os.File) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	_, err := f.Seek(0, 0)
	return err
}

func validShebang(r io.Reader) (bool, error) {
	b := make([]byte, 32)
	n, err := r.Read(b)
	if err != nil {
		return false, err
	}
	return shebang.Match(b[:n]), nil
}

func formatPath(path string, size int64, always bool) error {
	shellExt := always || shellFile.MatchString(path)
	if !shellExt && strings.Contains(path, ".") {
		// has an unwanted extension
		return nil
	}
	if !shellExt && size < 8 {
		// cannot possibly hold valid shebang
		return nil
	}
	mode := os.O_RDONLY
	if *write {
		mode = os.O_RDWR
	}
	f, err := os.OpenFile(path, mode, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	if !shellExt {
		valid, err := validShebang(f)
		if !valid || err != nil {
			return err
		}
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
		if err := config.Fprint(&buf, prog); err != nil {
			return err
		}
		if buf.String() != orig {
			fmt.Println(path)
		}
		if err := empty(f); err != nil {
			return err
		}
		_, err := io.Copy(f, &buf)
		return err
	case *write:
		if err := empty(f); err != nil {
			return err
		}
		w := bufio.NewWriter(f)
		if err := config.Fprint(w, prog); err != nil {
			return err
		}
		return w.Flush()
	case *list:
		var buf bytes.Buffer
		if err := config.Fprint(&buf, prog); err != nil {
			return err
		}
		if buf.String() != orig {
			fmt.Println(path)
		}
	default:
		return config.Fprint(os.Stdout, prog)
	}
	return nil
}
