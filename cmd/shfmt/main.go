// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime/pprof"
	"strings"

	"github.com/mvdan/sh/parser"
	"github.com/mvdan/sh/printer"
)

var (
	write      = flag.Bool("w", false, "write result to file instead of stdout")
	list       = flag.Bool("l", false, "list files whose formatting differs from shfmt's")
	indent     = flag.Int("i", 0, "indent: 0 for tabs (default), >0 for number of spaces")
	posix      = flag.Bool("p", false, "parse POSIX shell code instead of bash")
	cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")

	printConfig       printer.Config
	readBuf, writeBuf bytes.Buffer
)

func main() {
	flag.Parse()
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	printConfig.Spaces = *indent
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
	readBuf.Reset()
	if _, err := io.Copy(&readBuf, os.Stdin); err != nil {
		return err
	}
	src := readBuf.Bytes()
	prog, err := parser.Parse(src, "", parser.ParseComments)
	if err != nil {
		return err
	}
	return printConfig.Fprint(os.Stdout, prog)
}

var (
	hidden       = regexp.MustCompile(`^\.[^/.]`)
	shellFile    = regexp.MustCompile(`^.*\.(sh|bash)$`)
	validShebang = regexp.MustCompile(`^#!/(usr/)?bin/(env *)?(sh|bash)`)
	vcsDir       = regexp.MustCompile(`^(\.git|\.svn|\.hg)$`)
)

func isShellFile(info os.FileInfo) bool {
	name := info.Name()
	switch {
	case info.IsDir(), hidden.MatchString(name), !info.Mode().IsRegular():
		return false
	case shellFile.MatchString(name):
		return true
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
		if info.IsDir() && vcsDir.MatchString(info.Name()) {
			return filepath.SkipDir
		}
		if err == nil && isShellFile(info) {
			err = formatPath(path, false)
		}
		if err != nil && !os.IsNotExist(err) {
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
	openMode := os.O_RDONLY
	if *write {
		openMode = os.O_RDWR
	}
	f, err := os.OpenFile(path, openMode, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	readBuf.Reset()
	if _, err := io.Copy(&readBuf, f); err != nil {
		return err
	}
	src := readBuf.Bytes()
	if !always && !validShebang.Match(src[:32]) {
		return nil
	}
	parseMode := parser.ParseComments
	if *posix {
		parseMode |= parser.PosixConformant
	}
	prog, err := parser.Parse(src, path, parseMode)
	if err != nil {
		return err
	}
	writeBuf.Reset()
	if err := printConfig.Fprint(&writeBuf, prog); err != nil {
		return err
	}
	res := writeBuf.Bytes()
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
