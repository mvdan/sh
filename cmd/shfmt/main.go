// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/mvdan/sh/fileutil"
	"github.com/mvdan/sh/syntax"
)

var (
	write       = flag.Bool("w", false, "write result to file instead of stdout")
	list        = flag.Bool("l", false, "list files whose formatting differs from shfmt's")
	simple      = flag.Bool("s", false, "simplify the code")
	posix       = flag.Bool("p", false, "parse POSIX shell code instead of bash")
	mksh        = flag.Bool("m", false, "parse MirBSD Korn shell code instead of bash")
	indent      = flag.Int("i", 0, "indent: 0 for tabs (default), >0 for number of spaces")
	binNext     = flag.Bool("bn", false, "binary ops like && and | may start a line")
	showVersion = flag.Bool("version", false, "show version and exit")

	parser            *syntax.Parser
	printer           *syntax.Printer
	readBuf, writeBuf bytes.Buffer

	copyBuf = make([]byte, 32*1024)

	out io.Writer = os.Stdout

	version = "v2-devel"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: shfmt [flags] [path ...]")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	lang := syntax.LangBash
	switch {
	case *mksh && *posix:
		fmt.Fprintln(os.Stderr, "cannot mix parser language flags")
		os.Exit(1)
	case *posix:
		lang = syntax.LangPOSIX
	case *mksh:
		lang = syntax.LangMirBSDKorn
	}
	parser = syntax.NewParser(syntax.KeepComments, syntax.Variant(lang))
	printer = syntax.NewPrinter(func(p *syntax.Printer) {
		syntax.Indent(*indent)(p)
		if *binNext {
			syntax.BinaryNextLine(p)
		}
	})
	if flag.NArg() == 0 {
		if err := formatStdin(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	anyErr := false
	for _, path := range flag.Args() {
		walk(path, func(err error) {
			anyErr = true
			fmt.Fprintln(os.Stderr, err)
		})
	}
	if anyErr {
		os.Exit(1)
	}
}

func formatStdin() error {
	if *write || *list {
		return fmt.Errorf("-w and -l can only be used on files")
	}
	prog, err := parser.Parse(os.Stdin, "")
	if err != nil {
		return err
	}
	if *simple {
		simplify(prog)
	}
	return printer.Print(out, prog)
}

var vcsDir = regexp.MustCompile(`^\.(git|svn|hg)$`)

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
		conf := fileutil.CouldBeScript(info)
		if conf == fileutil.ConfNotScript {
			return nil
		}
		err = formatPath(path, conf == fileutil.ConfIfShebang)
		if err != nil && !os.IsNotExist(err) {
			onError(err)
		}
		return nil
	})
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
	if checkShebang {
		n, err := f.Read(copyBuf[:32])
		if err != nil {
			return err
		}
		if !fileutil.HasShebang(copyBuf[:n]) {
			return nil
		}
		readBuf.Write(copyBuf[:n])
	}
	if _, err := io.CopyBuffer(&readBuf, f, copyBuf); err != nil {
		return err
	}
	src := readBuf.Bytes()
	prog, err := parser.Parse(&readBuf, path)
	if err != nil {
		return err
	}
	if *simple {
		simplify(prog)
	}
	writeBuf.Reset()
	printer.Print(&writeBuf, prog)
	res := writeBuf.Bytes()
	if !bytes.Equal(src, res) {
		if *list {
			if _, err := fmt.Fprintln(out, path); err != nil {
				return err
			}
		}
		if *write {
			if err := f.Truncate(0); err != nil {
				return err
			}
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				return err
			}
			if _, err := f.Write(res); err != nil {
				return err
			}
			if err := f.Close(); err != nil {
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

func simplify(f *syntax.File) {
	syntax.Walk(f, simpleVisit)
}

func simpleVisit(node syntax.Node) bool {
	switch x := node.(type) {
	case *syntax.Assign:
		if x.Index != nil {
			x.Index = removeParens(x.Index)
		}
	case *syntax.ParamExp:
		if x.Index != nil {
			x.Index = removeParens(x.Index)
		}
		if x.Slice == nil {
			break
		}
		if x.Slice.Offset != nil {
			x.Slice.Offset = removeParens(x.Slice.Offset)
		}
		if x.Slice.Length != nil {
			x.Slice.Length = removeParens(x.Slice.Length)
		}
		w, _ := x.Slice.Offset.(*syntax.Word)
		if !isLitWord(w, "0") {
			break
		}
		if x.Slice.Length == nil {
			x.Slice = nil
		} else {
			x.Slice.Offset = nil
		}
	case *syntax.ArithmExp:
		x.X = removeParens(x.X)
	case *syntax.ArithmCmd:
		x.X = removeParens(x.X)
	case *syntax.ParenArithm:
		x.X = removeParens(x.X)
	}
	return true
}

func isLitWord(w *syntax.Word, s string) bool {
	if w == nil || len(w.Parts) != 1 {
		return false
	}
	lit, ok := w.Parts[0].(*syntax.Lit)
	return ok && lit.Value == s
}

func removeParens(x syntax.ArithmExpr) syntax.ArithmExpr {
	for {
		par, _ := x.(*syntax.ParenArithm)
		if par == nil {
			return x
		}
		x = par.X
	}
}
