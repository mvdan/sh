// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main

import (
	"bytes"
	"flag"
	"fmt"
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

var (
	config sh.PrintConfig
	buf    bytes.Buffer
)

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
	src, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	prog, err := sh.Parse(src, "", sh.ParseComments)
	if err != nil {
		return err
	}
	return config.Fprint(os.Stdout, prog)
}

var (
	hidden       = regexp.MustCompile(`^\.[^/.]`)
	shellFile    = regexp.MustCompile(`^.*\.(sh|bash)$`)
	validShebang = regexp.MustCompile(`^#!/(usr/)?bin/(env *)?(sh|bash)`)
)

func isShellFile(info os.FileInfo) bool {
	name := info.Name()
	switch {
	case info.IsDir(), hidden.MatchString(name):
		return false
	case shellFile.MatchString(name):
		return true
	case !info.Mode().IsRegular():
		return false
	case strings.Contains(name, "."):
		return false // different extension
	case info.Size() < 8:
		return false // cannot possibly hold valid shebang
	default:
		return true
	}
}

func walk(path string, onError func(error)) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return formatPath(path, true)
	}
	return filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if err == nil && isShellFile(info) {
			err = formatPath(path, false)
		}
		if err != nil {
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

func formatPath(path string, always bool) error {
	mode := os.O_RDONLY
	if *write {
		mode = os.O_RDWR
	}
	f, err := os.OpenFile(path, mode, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	src, err := ioutil.ReadAll(f)
	if err != nil {
		return err
	}
	if !always && !validShebang.Match(src[:32]) {
		return nil
	}
	prog, err := sh.Parse(src, path, sh.ParseComments)
	if err != nil {
		return err
	}
	buf.Reset()
	if err := config.Fprint(&buf, prog); err != nil {
		return err
	}
	res := buf.Bytes()
	if !bytes.Equal(src, res) {
		if *list {
			fmt.Println(path)
		}
		if *write {
			if err := empty(f); err != nil {
				return err
			}
			if _, err := f.Write(res); err != nil {
				return err
			}
		}
	}
	if !*list && !*write {
		if _, err := os.Stdout.Write(res); err != nil {
			return err
		}
	}
	return nil
}
