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
			x.Index = removeParensArithm(x.Index)
			x.Index = inlineSimpleParams(x.Index)
		}
	case *syntax.ParamExp:
		if x.Index != nil {
			x.Index = removeParensArithm(x.Index)
			x.Index = inlineSimpleParams(x.Index)
		}
		if x.Slice == nil {
			break
		}
		if x.Slice.Offset != nil {
			x.Slice.Offset = removeParensArithm(x.Slice.Offset)
			x.Slice.Offset = inlineSimpleParams(x.Slice.Offset)
		}
		if x.Slice.Length != nil {
			x.Slice.Length = removeParensArithm(x.Slice.Length)
			x.Slice.Length = inlineSimpleParams(x.Slice.Length)
		}
	case *syntax.ArithmExp:
		x.X = removeParensArithm(x.X)
		x.X = inlineSimpleParams(x.X)
	case *syntax.ArithmCmd:
		x.X = removeParensArithm(x.X)
		x.X = inlineSimpleParams(x.X)
	case *syntax.ParenArithm:
		x.X = removeParensArithm(x.X)
		x.X = inlineSimpleParams(x.X)
	case *syntax.BinaryArithm:
		x.X = inlineSimpleParams(x.X)
		x.Y = inlineSimpleParams(x.Y)
	case *syntax.CmdSubst:
		x.Stmts = inlineSubshell(x.Stmts)
	case *syntax.Subshell:
		x.Stmts = inlineSubshell(x.Stmts)
	case *syntax.Word:
		x.Parts = simplifyWord(x.Parts)
	case *syntax.TestClause:
		x.X = removeParensTest(x.X)
	case *syntax.BinaryTest:
		x.X = unquoteParams(x.X)
		switch x.Op {
		case syntax.TsMatch, syntax.TsNoMatch:
			// unquoting enables globbing
		default:
			x.Y = unquoteParams(x.Y)
		}
	case *syntax.UnaryTest:
		x.X = unquoteParams(x.X)
	}
	return true
}

func simplifyWord(wps []syntax.WordPart) []syntax.WordPart {
parts:
	for i, wp := range wps {
		dq, _ := wp.(*syntax.DblQuoted)
		if dq == nil || len(dq.Parts) != 1 {
			break
		}
		lit, _ := dq.Parts[0].(*syntax.Lit)
		if lit == nil {
			break
		}
		var buf bytes.Buffer
		escaped := false
		for _, r := range lit.Value {
			switch r {
			case '\\':
				escaped = !escaped
				if escaped {
					continue
				}
			case '\'':
				continue parts
			case '$', '"', '`':
				escaped = false
			default:
				if escaped {
					continue parts
				}
				escaped = false
			}
			buf.WriteRune(r)
		}
		newVal := buf.String()
		if newVal == lit.Value {
			break
		}
		wps[i] = &syntax.SglQuoted{
			Position: dq.Position,
			Dollar:   dq.Dollar,
			Value:    newVal,
		}
	}
	return wps
}

func removeParensArithm(x syntax.ArithmExpr) syntax.ArithmExpr {
	for {
		par, _ := x.(*syntax.ParenArithm)
		if par == nil {
			return x
		}
		x = par.X
	}
}

func inlineSimpleParams(x syntax.ArithmExpr) syntax.ArithmExpr {
	w, _ := x.(*syntax.Word)
	if w == nil || len(w.Parts) != 1 {
		return x
	}
	pe, _ := w.Parts[0].(*syntax.ParamExp)
	if pe == nil || !syntax.ValidName(pe.Param.Value) {
		return x
	}
	if pe.Indirect || pe.Length || pe.Width || pe.Key != nil ||
		pe.Slice != nil || pe.Repl != nil || pe.Exp != nil {
		return x
	}
	if pe.Index != nil {
		pe.Short = true
		return w
	}
	return &syntax.Word{
		Parts: []syntax.WordPart{pe.Param},
	}
}

func inlineSubshell(stmts []*syntax.Stmt) []*syntax.Stmt {
	if len(stmts) != 1 {
		return stmts
	}
	s := stmts[0]
	sub, _ := s.Cmd.(*syntax.Subshell)
	if sub == nil {
		return stmts
	}
	if s.Negated || s.Background || s.Coprocess ||
		len(s.Assigns) > 0 || len(s.Redirs) > 0 {
		return stmts
	}
	return sub.Stmts
}

func unquoteParams(x syntax.TestExpr) syntax.TestExpr {
	w, _ := x.(*syntax.Word)
	if w == nil || len(w.Parts) != 1 {
		return x
	}
	dq, _ := w.Parts[0].(*syntax.DblQuoted)
	if dq == nil || len(dq.Parts) != 1 {
		return x
	}
	if _, ok := dq.Parts[0].(*syntax.ParamExp); !ok {
		return x
	}
	w.Parts = dq.Parts
	return w
}

func removeParensTest(x syntax.TestExpr) syntax.TestExpr {
	for {
		par, _ := x.(*syntax.ParenTest)
		if par == nil {
			return x
		}
		x = par.X
	}
}
