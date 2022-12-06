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

	maybeio "github.com/google/renameio/v2/maybe"
	diffpkg "github.com/pkg/diff"
	diffwrite "github.com/pkg/diff/write"
	"golang.org/x/term"
	"mvdan.cc/editorconfig"

	"mvdan.cc/sh/v3/fileutil"
	"mvdan.cc/sh/v3/syntax"
	"mvdan.cc/sh/v3/syntax/typedjson"
)

// TODO: this flag business screams generics. try again with Go 1.18+.

type boolFlag struct {
	short, long string
	val         bool
}

type stringFlag struct {
	short, long string
	val         string
}

type uintFlag struct {
	short, long string
	val         uint
}

type langFlag struct {
	short, long string
	val         syntax.LangVariant
}

var (
	versionFlag = &boolFlag{"", "version", false}
	list        = &boolFlag{"l", "list", false}

	write    = &boolFlag{"w", "write", false}
	simplify = &boolFlag{"s", "simplify", false}
	minify   = &boolFlag{"mn", "minify", false}
	find     = &boolFlag{"f", "find", false}
	diff     = &boolFlag{"d", "diff", false}

	lang     = &langFlag{"ln", "language-dialect", syntax.LangAuto}
	posix    = &boolFlag{"p", "posix", false}
	filename = &stringFlag{"", "filename", ""}

	indent      = &uintFlag{"i", "indent", 0}
	binNext     = &boolFlag{"bn", "binary-next-line", false}
	caseIndent  = &boolFlag{"ci", "case-indent", false}
	spaceRedirs = &boolFlag{"sr", "space-redirects", false}
	keepPadding = &boolFlag{"kp", "keep-padding", false}
	funcNext    = &boolFlag{"fn", "func-next-line", false}

	toJSON   = &boolFlag{"tojson", "to-json", false} // TODO(v4): remove "tojson" for consistency
	fromJSON = &boolFlag{"", "from-json", false}

	// useEditorConfig will be false if any parser or printer flags were used.
	useEditorConfig = true

	parser            *syntax.Parser
	printer           *syntax.Printer
	readBuf, writeBuf bytes.Buffer

	copyBuf = make([]byte, 32*1024)

	in    io.Reader = os.Stdin
	out   io.Writer = os.Stdout
	color bool

	version = "(devel)" // to match the default from runtime/debug

	allFlags = []interface{}{
		versionFlag, list, write, simplify, minify, find, diff,
		lang, posix, filename,
		indent, binNext, caseIndent, spaceRedirs, keepPadding, funcNext, toJSON, fromJSON,
	}
)

func init() {
	for _, f := range allFlags {
		switch f := f.(type) {
		case *boolFlag:
			if name := f.short; name != "" {
				flag.BoolVar(&f.val, name, f.val, "")
			}
			if name := f.long; name != "" {
				flag.BoolVar(&f.val, name, f.val, "")
			}
		case *stringFlag:
			if name := f.short; name != "" {
				flag.StringVar(&f.val, name, f.val, "")
			}
			if name := f.long; name != "" {
				flag.StringVar(&f.val, name, f.val, "")
			}
		case *uintFlag:
			if name := f.short; name != "" {
				flag.UintVar(&f.val, name, f.val, "")
			}
			if name := f.long; name != "" {
				flag.UintVar(&f.val, name, f.val, "")
			}
		case *langFlag:
			if name := f.short; name != "" {
				flag.Var(&f.val, name, "")
			}
			if name := f.long; name != "" {
				flag.Var(&f.val, name, "")
			}
		default:
			panic(fmt.Sprintf("%T", f))
		}
	}
}

func main() {
	os.Exit(main1())
}

func main1() int {
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, `usage: shfmt [flags] [path ...]

shfmt formats shell programs. If the only argument is a dash ('-') or no
arguments are given, standard input will be used. If a given path is a
directory, all shell scripts found under that directory will be used.

  --version  show version and exit

  -l,  --list      list files whose formatting differs from shfmt's
  -w,  --write     write result to file instead of stdout
  -d,  --diff      error with a diff when the formatting differs
  -s,  --simplify  simplify the code
  -mn, --minify    minify the code to reduce its size (implies -s)

Parser options:

  -ln, --language-dialect str  bash/posix/mksh/bats, default "auto"
  -p,  --posix                 shorthand for -ln=posix
  --filename str               provide a name for the standard input file

Printer options:

  -i,  --indent uint       0 for tabs (default), >0 for number of spaces
  -bn, --binary-next-line  binary ops like && and | may start a line
  -ci, --case-indent       switch cases will be indented
  -sr, --space-redirects   redirect operators will be followed by a space
  -kp, --keep-padding      keep column alignment paddings
  -fn, --func-next-line    function opening braces are placed on a separate line

Utilities:

  -f, --find   recursively find all shell files and print the paths
  --to-json    print syntax tree to stdout as a typed JSON
  --from-json  read syntax tree from stdin as a typed JSON

For more information, see 'man shfmt' and https://github.com/mvdan/sh.
`)
	}
	flag.Parse()

	if versionFlag.val {
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
	if posix.val && lang.val != syntax.LangAuto {
		fmt.Fprintf(os.Stderr, "-p and -ln=lang cannot coexist\n")
		return 1
	}
	if minify.val {
		simplify.val = true
	}
	if os.Getenv("SHFMT_NO_EDITORCONFIG") == "true" {
		useEditorConfig = false
	}
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case lang.short, lang.long,
			posix.short, posix.long,
			indent.short, indent.long,
			binNext.short, binNext.long,
			caseIndent.short, caseIndent.long,
			spaceRedirs.short, spaceRedirs.long,
			keepPadding.short, keepPadding.long,
			funcNext.short, funcNext.long:
			useEditorConfig = false
		}
	})
	parser = syntax.NewParser(syntax.KeepComments(true))
	printer = syntax.NewPrinter(syntax.Minify(minify.val))

	if !useEditorConfig {
		if posix.val {
			// -p equals -ln=posix
			lang.val = syntax.LangPOSIX
		}

		syntax.Indent(indent.val)(printer)
		syntax.BinaryNextLine(binNext.val)(printer)
		syntax.SwitchCaseIndent(caseIndent.val)(printer)
		syntax.SpaceRedirects(spaceRedirs.val)(printer)
		syntax.KeepPadding(keepPadding.val)(printer)
		syntax.FunctionNextLine(funcNext.val)(printer)
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
		if toJSON.val {
			name = "" // the default is not useful there
		}
		if filename.val != "" {
			name = filename.val
		}
		if err := formatStdin(name); err != nil {
			if err != errChangedWithDiff {
				fmt.Fprintln(os.Stderr, err)
			}
			return 1
		}
		return 0
	}
	if filename.val != "" {
		fmt.Fprintln(os.Stderr, "-filename can only be used with stdin")
		return 1
	}
	if toJSON.val {
		fmt.Fprintln(os.Stderr, "--to-json can only be used with stdin")
		return 1
	}
	status := 0
	for _, path := range flag.Args() {
		if info, err := os.Stat(path); err == nil && !info.IsDir() && !find.val {
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
	if write.val {
		return fmt.Errorf("-w cannot be used on standard input")
	}
	src, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	fileLang := lang.val
	if fileLang == syntax.LangAuto {
		extensionLang := strings.TrimPrefix(filepath.Ext(name), ".")
		if err := fileLang.Set(extensionLang); err != nil || fileLang == syntax.LangPOSIX {
			shebangLang := fileutil.Shebang(src)
			if err := fileLang.Set(shebangLang); err != nil {
				// Fall back to bash.
				fileLang = syntax.LangBash
			}
		}
	}
	return formatBytes(src, name, fileLang)
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
	// TODO(v4): rename to case_indent for consistency with flags
	syntax.SwitchCaseIndent(props.Get("switch_case_indent") == "true")(printer)
	syntax.SpaceRedirects(props.Get("space_redirects") == "true")(printer)
	syntax.KeepPadding(props.Get("keep_padding") == "true")(printer)
	// TODO(v4): rename to func_next_line for consistency with flags
	syntax.FunctionNextLine(props.Get("function_next_line") == "true")(printer)
}

func formatPath(path string, checkShebang bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	fileLang := lang.val
	shebangForAuto := false
	if fileLang == syntax.LangAuto {
		extensionLang := strings.TrimPrefix(filepath.Ext(path), ".")
		if err := fileLang.Set(extensionLang); err != nil || fileLang == syntax.LangPOSIX {
			shebangForAuto = true
		}
	}
	readBuf.Reset()
	if checkShebang || shebangForAuto {
		n, err := io.ReadAtLeast(f, copyBuf[:32], len("#!/bin/sh\n"))
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
			if err := fileLang.Set(shebangLang); err != nil {
				// Fall back to bash.
				fileLang = syntax.LangBash
			}
		}
		readBuf.Write(copyBuf[:n])
	}
	if find.val {
		fmt.Fprintln(out, path)
		return nil
	}
	if _, err := io.CopyBuffer(&readBuf, f, copyBuf); err != nil {
		return err
	}
	f.Close()
	return formatBytes(readBuf.Bytes(), path, fileLang)
}

func formatBytes(src []byte, path string, fileLang syntax.LangVariant) error {
	if useEditorConfig {
		props, err := ecQuery.Find(path)
		if err != nil {
			return err
		}
		propsOptions(fileLang, props)
	} else {
		syntax.Variant(fileLang)(parser)
	}
	var node syntax.Node
	var err error
	if fromJSON.val {
		node, err = typedjson.Decode(bytes.NewReader(src))
		if err != nil {
			return err
		}
	} else {
		node, err = parser.Parse(bytes.NewReader(src), path)
		if err != nil {
			if s, ok := err.(syntax.LangError); ok && lang.val == syntax.LangAuto {
				return fmt.Errorf("%w (parsed as %s via -%s=%s)", s, fileLang, lang.short, lang.val)
			}
			return err
		}
	}
	if simplify.val {
		syntax.Simplify(node)
	}
	if toJSON.val {
		// must be standard input; fine to return
		// TODO: change the default behavior to be compact,
		// and allow using --to-json=pretty or --to-json=indent.
		return typedjson.EncodeOptions{Indent: "\t"}.Encode(out, node)
	}
	writeBuf.Reset()
	printer.Print(&writeBuf, node)
	res := writeBuf.Bytes()
	if !bytes.Equal(src, res) {
		if list.val {
			if _, err := fmt.Fprintln(out, path); err != nil {
				return err
			}
		}
		if write.val {
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
		if diff.val {
			opts := []diffwrite.Option{}
			if color {
				opts = append(opts, diffwrite.TerminalColor())
			}
			if err := diffpkg.Text(path+".orig", path, src, res, out, opts...); err != nil {
				return fmt.Errorf("computing diff: %s", err)
			}
			return errChangedWithDiff
		}
	}
	if !list.val && !write.val && !diff.val {
		if _, err := out.Write(res); err != nil {
			return err
		}
	}
	return nil
}
