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

	"github.com/mvdan/sh/syntax"
)

var (
	write      = flag.Bool("w", false, "write result to file instead of stdout")
	list       = flag.Bool("l", false, "list files whose formatting differs from shfmt's")
	indent     = flag.Int("i", 0, "indent: 0 for tabs (default), >0 for number of spaces")
	posix      = flag.Bool("p", false, "parse POSIX shell code instead of bash")
	cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")

	parseMode         syntax.ParseMode
	printConfig       syntax.PrintConfig
	readBuf, writeBuf bytes.Buffer

	out io.Writer
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

	out = os.Stdout
	printConfig.Spaces = *indent
	parseMode |= syntax.ParseComments
	if *posix {
		parseMode |= syntax.PosixConformant
	}
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
		walk(path, onError)
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
	prog, err := syntax.Parse(src, "", parseMode)
	if err != nil {
		return err
	}
	return printConfig.Fprint(out, prog)
}

var (
	shellFile    = regexp.MustCompile(`\.(sh|bash)$`)
	validShebang = regexp.MustCompile(`^#!\s?/(usr/)?bin/(env *)?(sh|bash)`)
	vcsDir       = regexp.MustCompile(`^\.(git|svn|hg)$`)
)

type shellConfidence int

const (
	notShellFile shellConfidence = iota
	ifValidShebang
	isShellFile
)

func getConfidence(info os.FileInfo) shellConfidence {
	name := info.Name()
	switch {
	case info.IsDir(), name[0] == '.', !info.Mode().IsRegular():
		return notShellFile
	case shellFile.MatchString(name):
		return isShellFile
	case strings.Contains(name, "."):
		return notShellFile // different extension
	case info.Size() < 8:
		return notShellFile // cannot possibly hold valid shebang
	default:
		return ifValidShebang
	}
}

func walk(path string, onError func(error)) {
	info, err := os.Stat(path)
	if err != nil {
		onError(err)
		return
	}
	if !info.IsDir() {
		if err := formatPath(path, false); err != nil {
			onError(err)
		}
		return
	}
	filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if info.IsDir() && vcsDir.MatchString(info.Name()) {
			return filepath.SkipDir
		}
		if err != nil {
			onError(err)
			return nil
		}
		conf := getConfidence(info)
		if conf == notShellFile {
			return nil
		}
		err = formatPath(path, conf == ifValidShebang)
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

func formatPath(path string, checkShebang bool) error {
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
	if checkShebang && !validShebang.Match(src[:32]) {
		return nil
	}
	prog, err := syntax.Parse(src, path, parseMode)
	if err != nil {
		return err
	}
	writeBuf.Reset()
	printConfig.Fprint(&writeBuf, prog)
	res := writeBuf.Bytes()
	if !bytes.Equal(src, res) {
		if *list {
			fmt.Fprintln(out, path)
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
		if _, err := out.Write(res); err != nil {
			return err
		}
	}
	return nil
}
