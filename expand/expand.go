// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package expand

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"mvdan.cc/sh/syntax"
)

type Config struct {
	// Env is used to get and set environment variables when performing
	// shell expansions. Some special parameters are also expanded via this
	// interface, such as:
	//
	//   * "#", "@", "*", "0"-"9" for the shell's parameters
	//   * "?", "$", "PPID" for the shell's status and process
	//   * "HOME foo" to retrieve user foo's home directory (if unset,
	//     os/user.Lookup will be used)
	Env Environ

	// NoGlob corresponds to the shell option that disables globbing.
	NoGlob bool
	// GlobStar corresponds to the shell option that allows globbing with
	// "**".
	GlobStar bool

	// CmdSubst is used to expand command substitutions. Output should be
	// written to the provided io.Writer.
	//
	// If nil, expanding a syntax.CmdSubst node will result in an
	// UnexpectedCommandError error.
	CmdSubst func(io.Writer, *syntax.CmdSubst)

	// TODO: rethink this interface

	// Readdirnames is used for file path globbing. If nil, globbing is
	// disabled. Use Config.SystemReaddirnames to use the filesystem
	// directly.
	Readdirnames func(string) []string

	// OnError is called when an error is encountered. If nil, errors cause
	// a panic.
	OnError func(error)

	bufferAlloc bytes.Buffer
	fieldAlloc  [4]fieldPart
	fieldsAlloc [4][]fieldPart

	ifs string
	// A pointer to a parameter expansion node, if we're inside one.
	// Necessary for ${LINENO}.
	curParam *syntax.ParamExp
}

// UnexpectedCommandError is returned if a command substitution is encountered
// when Config.CmdSubst is nil.
type UnexpectedCommandError struct {
	Node *syntax.CmdSubst
}

func (u UnexpectedCommandError) Error() string {
	return fmt.Sprintf("unexpected command substitution at %s", u.Node.Pos())
}

func prepareConfig(cfg *Config) *Config {
	if cfg == nil {
		panic("TODO: allow nil expand.Config")
	}
	vr := cfg.Env.Get("IFS")
	if !vr.IsSet() {
		cfg.ifs = " \t\n"
	} else {
		cfg.ifs = vr.String()
	}
	return cfg
}

func (cfg *Config) ifsRune(r rune) bool {
	for _, r2 := range cfg.ifs {
		if r == r2 {
			return true
		}
	}
	return false
}

func (cfg *Config) ifsJoin(strs []string) string {
	sep := ""
	if cfg.ifs != "" {
		sep = cfg.ifs[:1]
	}
	return strings.Join(strs, sep)
}

func (cfg *Config) err(err error) {
	if cfg.OnError == nil {
		panic(err)
	}
	cfg.OnError(err)
}

func (cfg *Config) strBuilder() *bytes.Buffer {
	b := &cfg.bufferAlloc
	b.Reset()
	return b
}

func (cfg *Config) envGet(name string) string {
	return cfg.Env.Get(name).String()
}

func (cfg *Config) envSet(name, value string) {
	wenv, ok := cfg.Env.(WriteEnviron)
	if !ok {
		// TODO: we should probably error here
		return
	}
	wenv.Set(name, Variable{Value: value})
}

func Literal(cfg *Config, word *syntax.Word) string {
	if word == nil {
		return ""
	}
	cfg = prepareConfig(cfg)
	field := cfg.wordField(word.Parts, quoteDouble)
	return cfg.fieldJoin(field)
}

func Format(cfg *Config, format string, args []string) (string, int, error) {
	cfg = prepareConfig(cfg)
	buf := cfg.strBuilder()
	esc := false
	var fmts []rune
	initialArgs := len(args)

	for _, c := range format {
		switch {
		case esc:
			esc = false
			switch c {
			case 'n':
				buf.WriteRune('\n')
			case 'r':
				buf.WriteRune('\r')
			case 't':
				buf.WriteRune('\t')
			case '\\':
				buf.WriteRune('\\')
			default:
				buf.WriteRune('\\')
				buf.WriteRune(c)
			}

		case len(fmts) > 0:
			switch c {
			case '%':
				buf.WriteByte('%')
				fmts = nil
			case 'c':
				var b byte
				if len(args) > 0 {
					arg := ""
					arg, args = args[0], args[1:]
					if len(arg) > 0 {
						b = arg[0]
					}
				}
				buf.WriteByte(b)
				fmts = nil
			case '+', '-', ' ':
				if len(fmts) > 1 {
					return "", 0, fmt.Errorf("invalid format char: %c", c)
				}
				fmts = append(fmts, c)
			case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
				fmts = append(fmts, c)
			case 's', 'd', 'i', 'u', 'o', 'x':
				arg := ""
				if len(args) > 0 {
					arg, args = args[0], args[1:]
				}
				var farg interface{} = arg
				if c != 's' {
					n, _ := strconv.ParseInt(arg, 0, 0)
					if c == 'i' || c == 'd' {
						farg = int(n)
					} else {
						farg = uint(n)
					}
					if c == 'i' || c == 'u' {
						c = 'd'
					}
				}
				fmts = append(fmts, c)
				fmt.Fprintf(buf, string(fmts), farg)
				fmts = nil
			default:
				return "", 0, fmt.Errorf("invalid format char: %c", c)
			}
		case c == '\\':
			esc = true
		case args != nil && c == '%':
			// if args == nil, we are not doing format
			// arguments
			fmts = []rune{c}
		default:
			buf.WriteRune(c)
		}
	}
	if len(fmts) > 0 {
		return "", 0, fmt.Errorf("missing format char")
	}
	return buf.String(), initialArgs - len(args), nil
}

func (cfg *Config) fieldJoin(parts []fieldPart) string {
	switch len(parts) {
	case 0:
		return ""
	case 1: // short-cut without a string copy
		return parts[0].val
	}
	buf := cfg.strBuilder()
	for _, part := range parts {
		buf.WriteString(part.val)
	}
	return buf.String()
}

func (cfg *Config) escapedGlobField(parts []fieldPart) (escaped string, glob bool) {
	buf := cfg.strBuilder()
	for _, part := range parts {
		if part.quote > quoteNone {
			buf.WriteString(syntax.QuotePattern(part.val))
			continue
		}
		buf.WriteString(part.val)
		if syntax.HasPattern(part.val) {
			glob = true
		}
	}
	if glob { // only copy the string if it will be used
		escaped = buf.String()
	}
	return escaped, glob
}

func Fields(cfg *Config, words ...*syntax.Word) []string {
	cfg = prepareConfig(cfg)
	fields := make([]string, 0, len(words))
	dir := cfg.envGet("PWD")
	baseDir := syntax.QuotePattern(dir)
	for _, expWord := range Braces(words...) {
		for _, field := range cfg.wordFields(expWord.Parts) {
			path, doGlob := cfg.escapedGlobField(field)
			var matches []string
			abs := filepath.IsAbs(path)
			if doGlob && !cfg.NoGlob {
				if !abs {
					path = filepath.Join(baseDir, path)
				}
				matches = cfg.glob(path)
			}
			if len(matches) == 0 {
				fields = append(fields, cfg.fieldJoin(field))
				continue
			}
			for _, match := range matches {
				if !abs {
					endSeparator := strings.HasSuffix(match, string(filepath.Separator))
					match, _ = filepath.Rel(dir, match)
					if endSeparator {
						match += string(filepath.Separator)
					}
				}
				fields = append(fields, match)
			}
		}
	}
	return fields
}

func Pattern(cfg *Config, word *syntax.Word) string {
	cfg = prepareConfig(cfg)
	field := cfg.wordField(word.Parts, quoteSingle)
	buf := cfg.strBuilder()
	for _, part := range field {
		if part.quote > quoteNone {
			buf.WriteString(syntax.QuotePattern(part.val))
		} else {
			buf.WriteString(part.val)
		}
	}
	return buf.String()
}

type fieldPart struct {
	val   string
	quote quoteLevel
}

type quoteLevel uint

const (
	quoteNone quoteLevel = iota
	quoteDouble
	quoteSingle
)

func (cfg *Config) wordField(wps []syntax.WordPart, ql quoteLevel) []fieldPart {
	var field []fieldPart
	for i, wp := range wps {
		switch x := wp.(type) {
		case *syntax.Lit:
			s := x.Value
			if i == 0 {
				s = cfg.expandUser(s)
			}
			if ql == quoteDouble && strings.Contains(s, "\\") {
				buf := cfg.strBuilder()
				for i := 0; i < len(s); i++ {
					b := s[i]
					if b == '\\' && i+1 < len(s) {
						switch s[i+1] {
						case '\n': // remove \\\n
							i++
							continue
						case '"', '\\', '$', '`': // special chars
							continue
						}
					}
					buf.WriteByte(b)
				}
				s = buf.String()
			}
			field = append(field, fieldPart{val: s})
		case *syntax.SglQuoted:
			fp := fieldPart{quote: quoteSingle, val: x.Value}
			if x.Dollar {
				fp.val, _, _ = Format(cfg, fp.val, nil)
			}
			field = append(field, fp)
		case *syntax.DblQuoted:
			for _, part := range cfg.wordField(x.Parts, quoteDouble) {
				part.quote = quoteDouble
				field = append(field, part)
			}
		case *syntax.ParamExp:
			field = append(field, fieldPart{val: cfg.paramExp(x)})
		case *syntax.CmdSubst:
			field = append(field, fieldPart{val: cfg.cmdSubst(x)})
		case *syntax.ArithmExp:
			field = append(field, fieldPart{
				val: strconv.Itoa(Arithm(cfg, x.X)),
			})
		default:
			panic(fmt.Sprintf("unhandled word part: %T", x))
		}
	}
	return field
}

func (cfg *Config) cmdSubst(cs *syntax.CmdSubst) string {
	if cfg.CmdSubst == nil {
		cfg.err(UnexpectedCommandError{Node: cs})
		return ""
	}
	buf := cfg.strBuilder()
	cfg.CmdSubst(buf, cs)
	return strings.TrimRight(buf.String(), "\n")
}

func (cfg *Config) wordFields(wps []syntax.WordPart) [][]fieldPart {
	fields := cfg.fieldsAlloc[:0]
	curField := cfg.fieldAlloc[:0]
	allowEmpty := false
	flush := func() {
		if len(curField) == 0 {
			return
		}
		fields = append(fields, curField)
		curField = nil
	}
	splitAdd := func(val string) {
		for i, field := range strings.FieldsFunc(val, cfg.ifsRune) {
			if i > 0 {
				flush()
			}
			curField = append(curField, fieldPart{val: field})
		}
	}
	for i, wp := range wps {
		switch x := wp.(type) {
		case *syntax.Lit:
			s := x.Value
			if i == 0 {
				s = cfg.expandUser(s)
			}
			if strings.Contains(s, "\\") {
				buf := cfg.strBuilder()
				for i := 0; i < len(s); i++ {
					b := s[i]
					if b == '\\' {
						i++
						b = s[i]
					}
					buf.WriteByte(b)
				}
				s = buf.String()
			}
			curField = append(curField, fieldPart{val: s})
		case *syntax.SglQuoted:
			allowEmpty = true
			fp := fieldPart{quote: quoteSingle, val: x.Value}
			if x.Dollar {
				fp.val, _, _ = Format(cfg, fp.val, nil)
			}
			curField = append(curField, fp)
		case *syntax.DblQuoted:
			allowEmpty = true
			if len(x.Parts) == 1 {
				pe, _ := x.Parts[0].(*syntax.ParamExp)
				if elems := cfg.quotedElems(pe); elems != nil {
					for i, elem := range elems {
						if i > 0 {
							flush()
						}
						curField = append(curField, fieldPart{
							quote: quoteDouble,
							val:   elem,
						})
					}
					continue
				}
			}
			for _, part := range cfg.wordField(x.Parts, quoteDouble) {
				part.quote = quoteDouble
				curField = append(curField, part)
			}
		case *syntax.ParamExp:
			splitAdd(cfg.paramExp(x))
		case *syntax.CmdSubst:
			splitAdd(cfg.cmdSubst(x))
		case *syntax.ArithmExp:
			curField = append(curField, fieldPart{
				val: strconv.Itoa(Arithm(cfg, x.X)),
			})
		default:
			panic(fmt.Sprintf("unhandled word part: %T", x))
		}
	}
	flush()
	if allowEmpty && len(fields) == 0 {
		fields = append(fields, curField)
	}
	return fields
}

// quotedElems checks if a parameter expansion is exactly ${@} or ${foo[@]}
func (cfg *Config) quotedElems(pe *syntax.ParamExp) []string {
	if pe == nil || pe.Excl || pe.Length || pe.Width {
		return nil
	}
	if pe.Param.Value == "@" {
		return cfg.Env.Get("@").Value.([]string)
	}
	if nodeLit(pe.Index) != "@" {
		return nil
	}
	val := cfg.Env.Get(pe.Param.Value).Value
	if x, ok := val.([]string); ok {
		return x
	}
	return nil
}

func (cfg *Config) expandUser(field string) string {
	if len(field) == 0 || field[0] != '~' {
		return field
	}
	name := field[1:]
	rest := ""
	if i := strings.Index(name, "/"); i >= 0 {
		rest = name[i:]
		name = name[:i]
	}
	if name == "" {
		return cfg.Env.Get("HOME").String() + rest
	}
	if vr := cfg.Env.Get("HOME " + name); vr.IsSet() {
		return vr.String() + rest
	}

	u, err := user.Lookup(name)
	if err != nil {
		return field
	}
	return u.HomeDir + rest
}

func findAllIndex(pattern, name string, n int) [][]int {
	expr, err := syntax.TranslatePattern(pattern, true)
	if err != nil {
		return nil
	}
	rx := regexp.MustCompile(expr)
	return rx.FindAllStringIndex(name, n)
}

// TODO: use this again to optimize globbing; see
// https://github.com/mvdan/sh/issues/213
func hasGlob(path string) bool {
	magicChars := `*?[`
	if runtime.GOOS != "windows" {
		magicChars = `*?[\`
	}
	return strings.ContainsAny(path, magicChars)
}

var rxGlobStar = regexp.MustCompile(".*")

func (cfg *Config) glob(pattern string) []string {
	parts := strings.Split(pattern, string(filepath.Separator))
	matches := []string{"."}
	if filepath.IsAbs(pattern) {
		if parts[0] == "" {
			// unix-like
			matches[0] = string(filepath.Separator)
		} else {
			// windows (for some reason it won't work without the
			// trailing separator)
			matches[0] = parts[0] + string(filepath.Separator)
		}
		parts = parts[1:]
	}
	for _, part := range parts {
		if part == "**" && cfg.GlobStar {
			for i := range matches {
				// "a/**" should match "a/ a/b a/b/cfg ..."; note
				// how the zero-match case has a trailing
				// separator.
				matches[i] += string(filepath.Separator)
			}
			// expand all the possible levels of **
			latest := matches
			for {
				var newMatches []string
				for _, dir := range latest {
					newMatches = cfg.globDir(dir, rxGlobStar, newMatches)
				}
				if len(newMatches) == 0 {
					// not another level of directories to
					// try; stop
					break
				}
				matches = append(matches, newMatches...)
				latest = newMatches
			}
			continue
		}
		expr, err := syntax.TranslatePattern(part, true)
		if err != nil {
			return nil
		}
		rx := regexp.MustCompile("^" + expr + "$")
		var newMatches []string
		for _, dir := range matches {
			newMatches = cfg.globDir(dir, rx, newMatches)
		}
		matches = newMatches
	}
	return matches
}

// SystemReaddirnames uses os.Open and File.Readdirnames to retrieve the names
// of the files within a directoy on the system's filesystem. Any error is
// reported via Config.OnError.
func (cfg *Config) SystemReaddirnames(dir string) []string {
	d, err := os.Open(dir)
	if err != nil {
		cfg.err(err)
		return nil
	}
	defer d.Close()

	names, err := d.Readdirnames(-1)
	if err != nil {
		cfg.err(err)
		return nil
	}
	sort.Strings(names)
	return names
}

func (cfg *Config) globDir(dir string, rx *regexp.Regexp, matches []string) []string {
	if cfg.Readdirnames == nil {
		return nil
	}
	names := cfg.Readdirnames(dir)
	for _, name := range names {
		if !strings.HasPrefix(rx.String(), `^\.`) && name[0] == '.' {
			continue
		}
		if rx.MatchString(name) {
			matches = append(matches, filepath.Join(dir, name))
		}
	}
	return matches
}

func ReadFields(cfg *Config, s string, n int, raw bool) []string {
	cfg = prepareConfig(cfg)
	type pos struct {
		start, end int
	}
	var fpos []pos

	runes := make([]rune, 0, len(s))
	infield := false
	esc := false
	for _, r := range s {
		if infield {
			if cfg.ifsRune(r) && (raw || !esc) {
				fpos[len(fpos)-1].end = len(runes)
				infield = false
			}
		} else {
			if !cfg.ifsRune(r) && (raw || !esc) {
				fpos = append(fpos, pos{start: len(runes), end: -1})
				infield = true
			}
		}
		if r == '\\' {
			if raw || esc {
				runes = append(runes, r)
			}
			esc = !esc
			continue
		}
		runes = append(runes, r)
		esc = false
	}
	if len(fpos) == 0 {
		return nil
	}
	if infield {
		fpos[len(fpos)-1].end = len(runes)
	}

	switch {
	case n == 1:
		// include heading/trailing IFSs
		fpos[0].start, fpos[0].end = 0, len(runes)
		fpos = fpos[:1]
	case n != -1 && n < len(fpos):
		// combine to max n fields
		fpos[n-1].end = fpos[len(fpos)-1].end
		fpos = fpos[:n]
	}

	var fields = make([]string, len(fpos))
	for i, p := range fpos {
		fields[i] = string(runes[p.start:p.end])
	}
	return fields
}
