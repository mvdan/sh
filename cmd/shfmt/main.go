// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"

	maybeio "github.com/google/renameio/maybe"
	"github.com/pkg/diff"
	diffwrite "github.com/pkg/diff/write"
	"golang.org/x/term"
	"mvdan.cc/editorconfig"

	"mvdan.cc/sh/v3/fileutil"
	"mvdan.cc/sh/v3/syntax"
)

var (
	showVersion = flag.Bool("version", false, "")

	list    = flag.Bool("l", false, "")
	write   = flag.Bool("w", false, "")
	simple  = flag.Bool("s", false, "")
	minify  = flag.Bool("mn", false, "")
	find    = flag.Bool("f", false, "")
	diffOut = flag.Bool("d", false, "")

	// useEditorConfig will be false if any parser or printer flags were used.
	useEditorConfig = true

	langFlag = syntax.LangAuto
	posix    = flag.Bool("p", false, "")
	filename = flag.String("filename", "", "")

	indent      = flag.Uint("i", 0, "")
	binNext     = flag.Bool("bn", false, "")
	caseIndent  = flag.Bool("ci", false, "")
	spaceRedirs = flag.Bool("sr", false, "")
	keepPadding = flag.Bool("kp", false, "")
	funcNext    = flag.Bool("fn", false, "")

	toJSON = flag.Bool("tojson", false, "")

	parser            *syntax.Parser
	printer           *syntax.Printer
	readBuf, writeBuf bytes.Buffer

	copyBuf = make([]byte, 32*1024)

	in    io.Reader = os.Stdin
	out   io.Writer = os.Stdout
	color bool

	version = "(devel)" // to match the default from runtime/debug
)

func init() { flag.Var(&langFlag, "ln", "") }

func main() {
	os.Exit(main1())
}

func main1() int {
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, `usage: shfmt [flags] [path ...]

shfmt formats shell programs. If the only argument is a dash ('-') or no
arguments are given, standard input will be used. If a given path is a
directory, all shell scripts found under that directory will be used.

  -version  show version and exit

  -l        list files whose formatting differs from shfmt's
  -w        write result to file instead of stdout
  -d        error with a diff when the formatting differs
  -s        simplify the code
  -mn       minify the code to reduce its size (implies -s)

Parser options:

  -ln str        language dialect (bash/posix/mksh/bats, default "auto")
  -p             shorthand for -ln=posix
  -filename str  provide a name for the standard input file

Printer options:

  -i uint   indent: 0 for tabs (default), >0 for number of spaces
  -bn       binary ops like && and | may start a line
  -ci       switch cases will be indented
  -sr       redirect operators will be followed by a space
  -kp       keep column alignment paddings
  -fn       function opening braces are placed on a separate line

Utilities:

  -f        recursively find all shell files and print the paths
  -tojson   print syntax tree to stdout as a typed JSON

For more information, see 'man shfmt' and https://github.com/mvdan/sh.
`)
	}
	flag.Parse()

	if *showVersion {
		// don't overwrite the version if it was set by -ldflags=-X
		if info, ok := debug.ReadBuildInfo(); ok && version == "(devel)" {
			mod := &info.Main
			if mod.Replace != nil {
				mod = mod.Replace
			}
			version = mod.Version
		}
		fmt.Println(version)
		return 0
	}
	if *posix && langFlag != syntax.LangAuto {
		fmt.Fprintf(os.Stderr, "-p and -ln=lang cannot coexist\n")
		return 1
	}
	if *minify {
		*simple = true
	}
	if os.Getenv("SHFMT_NO_EDITORCONFIG") == "true" {
		useEditorConfig = false
	}
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "ln", "p", "i", "bn", "ci", "sr", "kp", "fn":
			useEditorConfig = false
		}
	})
	parser = syntax.NewParser(syntax.KeepComments(true))
	printer = syntax.NewPrinter(syntax.Minify(*minify))

	if !useEditorConfig {
		if *posix {
			// -p equals -ln=posix
			langFlag = syntax.LangPOSIX
		}

		syntax.Indent(*indent)(printer)
		syntax.BinaryNextLine(*binNext)(printer)
		syntax.SwitchCaseIndent(*caseIndent)(printer)
		syntax.SpaceRedirects(*spaceRedirs)(printer)
		syntax.KeepPadding(*keepPadding)(printer)
		syntax.FunctionNextLine(*funcNext)(printer)
	}

	// Decide whether or not to use color for the diff output,
	// as described in shfmt.1.scd.
	if os.Getenv("FORCE_COLOR") != "" {
		color = true
	} else if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
	} else if f, ok := out.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		color = true
	}
	if flag.NArg() == 0 || (flag.NArg() == 1 && flag.Arg(0) == "-") {
		name := "<standard input>"
		if *filename != "" {
			name = *filename
		}
		if err := formatStdin(name); err != nil {
			if err != errChangedWithDiff {
				fmt.Fprintln(os.Stderr, err)
			}
			return 1
		}
		return 0
	}
	if *filename != "" {
		fmt.Fprintln(os.Stderr, "-filename can only be used with stdin")
		return 1
	}
	if *toJSON {
		fmt.Fprintln(os.Stderr, "-tojson can only be used with stdin")
		return 1
	}
	status := 0
	for _, path := range flag.Args() {
		if info, err := os.Stat(path); err == nil && !info.IsDir() && !*find {
			// When given paths to files directly, always format
			// them, no matter their extension or shebang.
			//
			// The only exception is the -f flag; in that case, we
			// do want to report whether the file is a shell script.
			if err := formatPath(path, false); err != nil {
				if err != errChangedWithDiff {
					fmt.Fprintln(os.Stderr, err)
				}
				status = 1
			}
			continue
		}
		if err := filepath.WalkDir(path, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			switch err := walkPath(path, entry); err {
			case nil:
			case filepath.SkipDir:
				return err
			case errChangedWithDiff:
				status = 1
			default:
				fmt.Fprintln(os.Stderr, err)
				status = 1
			}
			return nil
		}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			status = 1
		}
	}
	return status
}

var errChangedWithDiff = fmt.Errorf("")

func formatStdin(name string) error {
	if *write {
		return fmt.Errorf("-w cannot be used on standard input")
	}
	src, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	lang := langFlag
	if lang == syntax.LangAuto {
		extensionLang := strings.TrimPrefix(filepath.Ext(name), ".")
		if err := lang.Set(extensionLang); err != nil {
			shebangLang := fileutil.Shebang(src)
			if err := lang.Set(shebangLang); err != nil {
				// Fall back to bash.
				lang = syntax.LangBash
			}
		}
	}
	return formatBytes(src, name, lang)
}

var vcsDir = regexp.MustCompile(`^\.(git|svn|hg)$`)

func walkPath(path string, entry fs.DirEntry) error {
	if entry.IsDir() && vcsDir.MatchString(entry.Name()) {
		return filepath.SkipDir
	}
	if useEditorConfig {
		props, err := ecQuery.Find(path)
		if err != nil {
			return err
		}
		if props.Get("ignore") == "true" {
			if entry.IsDir() {
				return filepath.SkipDir
			} else {
				return nil
			}
		}
	}
	conf := fileutil.CouldBeScript2(entry)
	if conf == fileutil.ConfNotScript {
		return nil
	}
	err := formatPath(path, conf == fileutil.ConfIfShebang)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

var ecQuery = editorconfig.Query{
	FileCache:   make(map[string]*editorconfig.File),
	RegexpCache: make(map[string]*regexp.Regexp),
}

func propsOptions(lang syntax.LangVariant, props editorconfig.Section) {
	// if shell_variant is set to a valid string, it will take precedence
	lang.Set(props.Get("shell_variant"))
	syntax.Variant(lang)(parser)

	size := uint(0)
	if props.Get("indent_style") == "space" {
		size = 8
		if n := props.IndentSize(); n > 0 {
			size = uint(n)
		}
	}
	syntax.Indent(size)(printer)

	syntax.BinaryNextLine(props.Get("binary_next_line") == "true")(printer)
	syntax.SwitchCaseIndent(props.Get("switch_case_indent") == "true")(printer)
	syntax.SpaceRedirects(props.Get("space_redirects") == "true")(printer)
	syntax.KeepPadding(props.Get("keep_padding") == "true")(printer)
	syntax.FunctionNextLine(props.Get("function_next_line") == "true")(printer)
}

func formatPath(path string, checkShebang bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	lang := langFlag
	shebangForAuto := false
	if lang == syntax.LangAuto {
		extensionLang := strings.TrimPrefix(filepath.Ext(path), ".")
		if err := lang.Set(extensionLang); err != nil {
			shebangForAuto = true
		}
	}
	readBuf.Reset()
	if checkShebang || shebangForAuto {
		n, err := io.ReadAtLeast(f, copyBuf[:32], len("#/bin/sh\n"))
		switch {
		case !checkShebang:
			// only wanted the shebang for LangAuto
		case err == io.EOF, errors.Is(err, io.ErrUnexpectedEOF):
			return nil // too short to have a shebang
		case err != nil:
			return err // some other read error
		}
		shebangLang := fileutil.Shebang(copyBuf[:n])
		if checkShebang && shebangLang == "" {
			return nil // not a shell script
		}
		if shebangForAuto {
			if err := lang.Set(shebangLang); err != nil {
				// Fall back to bash.
				lang = syntax.LangBash
			}
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
	return formatBytes(readBuf.Bytes(), path, lang)
}

func formatBytes(src []byte, path string, lang syntax.LangVariant) error {
	if useEditorConfig {
		props, err := ecQuery.Find(path)
		if err != nil {
			return err
		}
		propsOptions(lang, props)
	} else {
		syntax.Variant(lang)(parser)
	}
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
			info, err := os.Lstat(path)
			if err != nil {
				return err
			}
			perm := info.Mode().Perm()
			// TODO: support atomic writes on Windows?
			if err := maybeio.WriteFile(path, res, perm); err != nil {
				return err
			}
		}
		if *diffOut {
			opts := []diffwrite.Option{}
			if color {
				opts = append(opts, diffwrite.TerminalColor())
			}
			if err := diff.Text(path+".orig", path, src, res, out, opts...); err != nil {
				return fmt.Errorf("computing diff: %s", err)
			}
			return errChangedWithDiff
		}
	}
	if !*list && !*write && !*diffOut {
		if _, err := out.Write(res); err != nil {
			return err
		}
	}
	return nil
}
