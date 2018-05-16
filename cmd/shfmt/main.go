// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main // import "mvdan.cc/sh/cmd/shfmt"

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"mvdan.cc/sh/fileutil"
	"mvdan.cc/sh/syntax"
)

var (
	showVersion = flag.Bool("version", false, "")

	list   = flag.Bool("l", false, "")
	write  = flag.Bool("w", false, "")
	simple = flag.Bool("s", false, "")
	find   = flag.Bool("f", false, "")
	diff   = flag.Bool("d", false, "")

	langStr = flag.String("ln", "", "")
	posix   = flag.Bool("p", false, "")

	indent      = flag.Uint("i", 0, "")
	binNext     = flag.Bool("bn", false, "")
	caseIndent  = flag.Bool("ci", false, "")
	keepPadding = flag.Bool("kp", false, "")
	minify      = flag.Bool("mn", false, "")

	toJSON = flag.Bool("tojson", false, "")

	parser            *syntax.Parser
	printer           *syntax.Printer
	readBuf, writeBuf bytes.Buffer

	copyBuf = make([]byte, 32*1024)

	in  io.Reader = os.Stdin
	out io.Writer = os.Stdout

	version = "v2.4.0"
)

func main() {
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, `usage: shfmt [flags] [path ...]

If no arguments are given, standard input will be used. If a given path
is a directory, it will be recursively searched for shell files - both
by filename extension and by shebang.

  -version  show version and exit

  -l        list files whose formatting differs from shfmt's
  -w        write result to file instead of stdout
  -d        display diffs when formatting differs
  -s        simplify the code

Parser options:

  -ln str   language variant to parse (bash/posix/mksh, default "bash")
  -p        shorthand for -ln=posix

Printer options:

  -i uint   indent: 0 for tabs (default), >0 for number of spaces
  -bn       binary ops like && and | may start a line
  -ci       switch cases will be indented
  -kp       keep column alignment paddings
  -mn       minify program to reduce its size (implies -s)

Utilities:

  -f        recursively find all shell files and print the paths
  -tojson   print syntax tree to stdout as a typed JSON
`)
	}
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}
	if *posix && *langStr != "" {
		fmt.Fprintf(os.Stderr, "-p and -ln=lang cannot coexist\n")
		os.Exit(1)
	}
	lang := syntax.LangBash
	switch *langStr {
	case "bash", "":
	case "posix":
		lang = syntax.LangPOSIX
	case "mksh":
		lang = syntax.LangMirBSDKorn
	default:
		fmt.Fprintf(os.Stderr, "unknown shell language: %s\n", *langStr)
		os.Exit(1)
	}
	if *posix {
		lang = syntax.LangPOSIX
	}
	if *minify {
		*simple = true
	}
	parser = syntax.NewParser(syntax.KeepComments, syntax.Variant(lang))
	printer = syntax.NewPrinter(func(p *syntax.Printer) {
		syntax.Indent(*indent)(p)
		if *binNext {
			syntax.BinaryNextLine(p)
		}
		if *caseIndent {
			syntax.SwitchCaseIndent(p)
		}
		if *keepPadding {
			syntax.KeepPadding(p)
		}
		if *minify {
			syntax.Minify(p)
		}
	})
	if flag.NArg() == 0 {
		if err := formatStdin(); err != nil {
			if err != errChangedWithDiff {
				fmt.Fprintln(os.Stderr, err)
			}
			os.Exit(1)
		}
		return
	}
	if *toJSON {
		fmt.Fprintln(os.Stderr, "-tojson can only be used with stdin/out")
		os.Exit(1)
	}
	anyErr := false
	for _, path := range flag.Args() {
		walk(path, func(err error) {
			if err != errChangedWithDiff {
				fmt.Fprintln(os.Stderr, err)
			}
			anyErr = true
		})
	}
	if anyErr {
		os.Exit(1)
	}
}

var errChangedWithDiff = fmt.Errorf("")

func formatStdin() error {
	if *write {
		return fmt.Errorf("-w cannot be used on standard input")
	}
	src, err := ioutil.ReadAll(in)
	if err != nil {
		return err
	}
	return formatBytes(src, "<standard input>")
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
	f, err := os.Open(path)
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
	if *find {
		fmt.Fprintln(out, path)
		return nil
	}
	if _, err := io.CopyBuffer(&readBuf, f, copyBuf); err != nil {
		return err
	}
	f.Close()
	return formatBytes(readBuf.Bytes(), path)
}

func formatBytes(src []byte, path string) error {
	prog, err := parser.Parse(bytes.NewReader(src), path)
	if err != nil {
		return err
	}
	if *simple {
		syntax.Simplify(prog)
	}
	if *toJSON {
		// must be standard input; fine to return
		return writeJSON(out, prog, true)
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
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
			if err != nil {
				return err
			}
			if _, err := f.Write(res); err != nil {
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		}
		if *diff {
			data, err := diffBytes(src, res, path)
			if err != nil {
				return fmt.Errorf("computing diff: %s", err)
			}
			out.Write(data)
			return errChangedWithDiff
		}
	}
	if !*list && !*write && !*diff {
		if _, err := out.Write(res); err != nil {
			return err
		}
	}
	return nil
}

func writeTempFile(dir, prefix string, data []byte) (string, error) {
	file, err := ioutil.TempFile(dir, prefix)
	if err != nil {
		return "", err
	}
	_, err = file.Write(data)
	if err1 := file.Close(); err == nil {
		err = err1
	}
	if err != nil {
		os.Remove(file.Name())
		return "", err
	}
	return file.Name(), nil
}

func diffBytes(b1, b2 []byte, path string) ([]byte, error) {
	fmt.Fprintf(out, "diff -u %s %s\n",
		filepath.ToSlash(path+".orig"),
		filepath.ToSlash(path))
	f1, err := writeTempFile("", "shfmt", b1)
	if err != nil {
		return nil, err
	}
	defer os.Remove(f1)

	f2, err := writeTempFile("", "shfmt", b2)
	if err != nil {
		return nil, err
	}
	defer os.Remove(f2)

	data, err := exec.Command("diff", "-u", f1, f2).Output()
	if len(data) == 0 {
		// No diff, or something went wrong; don't check for err
		// as diff will return non-zero if the files differ.
		return nil, err
	}
	// We already print the filename, so remove the
	// temporary filenames printed by diff.
	lines := bytes.Split(data, []byte("\n"))
	for i, line := range lines {
		switch {
		case bytes.HasPrefix(line, []byte("---")):
		case bytes.HasPrefix(line, []byte("+++")):
		default:
			return bytes.Join(lines[i:], []byte("\n")), nil
		}
	}
	return data, nil
}
