// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package expand

import (
	"bytes"
	"context"
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

type Context struct {
	Env Environ

	NoGlob   bool
	GlobStar bool

	Subshell func(context.Context, io.Writer, syntax.StmtList)

	// if nil, errors cause a panic.
	OnError func(error)

	bufferAlloc bytes.Buffer
	fieldAlloc  [4]fieldPart
	fieldsAlloc [4][]fieldPart

	ifs string
	// A pointer to a parameter expansion node, if we're inside one.
	// Necessary for ${LINENO}.
	curParam *syntax.ParamExp
}

func (c *Context) prepareIFS() {
	vr := c.Env.Get("IFS")
	if vr == (Variable{}) {
		c.ifs = " \t\n"
	} else {
		c.ifs = vr.String()
	}
}

func (c *Context) ifsRune(r rune) bool {
	for _, r2 := range c.ifs {
		if r == r2 {
			return true
		}
	}
	return false
}

func (c *Context) ifsJoin(strs []string) string {
	sep := ""
	if c.ifs != "" {
		sep = c.ifs[:1]
	}
	return strings.Join(strs, sep)
}

func (c *Context) err(err error) {
	if c.OnError == nil {
		panic(err)
	}
	c.OnError(err)
}

func (c *Context) strBuilder() *bytes.Buffer {
	b := &c.bufferAlloc
	b.Reset()
	return b
}

func (c *Context) envGet(name string) string {
	return c.Env.Get(name).String()
}

func (c *Context) envSet(name, value string) {
	c.Env.Set(name, Variable{Value: value})
}

func (c *Context) ExpandLiteral(ctx context.Context, word *syntax.Word) string {
	if word == nil {
		return ""
	}
	field := c.wordField(ctx, word.Parts, quoteDouble)
	return c.fieldJoin(field)
}

func (c *Context) ExpandFormat(format string, args []string) (string, int, error) {
	buf := c.strBuilder()
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

func (c *Context) fieldJoin(parts []fieldPart) string {
	switch len(parts) {
	case 0:
		return ""
	case 1: // short-cut without a string copy
		return parts[0].val
	}
	buf := c.strBuilder()
	for _, part := range parts {
		buf.WriteString(part.val)
	}
	return buf.String()
}

func (c *Context) escapedGlobField(parts []fieldPart) (escaped string, glob bool) {
	buf := c.strBuilder()
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

func (c *Context) ExpandFields(ctx context.Context, words ...*syntax.Word) []string {
	c.prepareIFS()

	fields := make([]string, 0, len(words))
	dir := c.envGet("PWD")
	baseDir := syntax.QuotePattern(dir)
	for _, expWord := range Braces(words...) {
		for _, field := range c.wordFields(ctx, expWord.Parts) {
			path, doGlob := c.escapedGlobField(field)
			var matches []string
			abs := filepath.IsAbs(path)
			if doGlob && !c.NoGlob {
				if !abs {
					path = filepath.Join(baseDir, path)
				}
				matches = glob(path, c.GlobStar)
			}
			if len(matches) == 0 {
				fields = append(fields, c.fieldJoin(field))
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

func (c *Context) ExpandPattern(ctx context.Context, word *syntax.Word) string {
	field := c.wordField(ctx, word.Parts, quoteSingle)
	buf := c.strBuilder()
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

func (c *Context) wordField(ctx context.Context, wps []syntax.WordPart, ql quoteLevel) []fieldPart {
	var field []fieldPart
	for i, wp := range wps {
		switch x := wp.(type) {
		case *syntax.Lit:
			s := x.Value
			if i == 0 {
				s = c.expandUser(s)
			}
			if ql == quoteDouble && strings.Contains(s, "\\") {
				buf := c.strBuilder()
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
				fp.val, _, _ = c.ExpandFormat(fp.val, nil)
			}
			field = append(field, fp)
		case *syntax.DblQuoted:
			for _, part := range c.wordField(ctx, x.Parts, quoteDouble) {
				part.quote = quoteDouble
				field = append(field, part)
			}
		case *syntax.ParamExp:
			field = append(field, fieldPart{val: c.paramExp(ctx, x)})
		case *syntax.CmdSubst:
			field = append(field, fieldPart{val: c.cmdSubst(ctx, x)})
		case *syntax.ArithmExp:
			field = append(field, fieldPart{
				val: strconv.Itoa(c.ExpandArithm(ctx, x.X)),
			})
		default:
			panic(fmt.Sprintf("unhandled word part: %T", x))
		}
	}
	return field
}

func (c *Context) cmdSubst(ctx context.Context, cs *syntax.CmdSubst) string {
	buf := c.strBuilder()
	c.Subshell(ctx, buf, cs.StmtList)
	return strings.TrimRight(buf.String(), "\n")
}

func (c *Context) wordFields(ctx context.Context, wps []syntax.WordPart) [][]fieldPart {
	fields := c.fieldsAlloc[:0]
	curField := c.fieldAlloc[:0]
	allowEmpty := false
	flush := func() {
		if len(curField) == 0 {
			return
		}
		fields = append(fields, curField)
		curField = nil
	}
	splitAdd := func(val string) {
		for i, field := range strings.FieldsFunc(val, c.ifsRune) {
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
				s = c.expandUser(s)
			}
			if strings.Contains(s, "\\") {
				buf := c.strBuilder()
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
				fp.val, _, _ = c.ExpandFormat(fp.val, nil)
			}
			curField = append(curField, fp)
		case *syntax.DblQuoted:
			allowEmpty = true
			if len(x.Parts) == 1 {
				pe, _ := x.Parts[0].(*syntax.ParamExp)
				if elems := c.quotedElems(pe); elems != nil {
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
			for _, part := range c.wordField(ctx, x.Parts, quoteDouble) {
				part.quote = quoteDouble
				curField = append(curField, part)
			}
		case *syntax.ParamExp:
			splitAdd(c.paramExp(ctx, x))
		case *syntax.CmdSubst:
			splitAdd(c.cmdSubst(ctx, x))
		case *syntax.ArithmExp:
			curField = append(curField, fieldPart{
				val: strconv.Itoa(c.ExpandArithm(ctx, x.X)),
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
func (c *Context) quotedElems(pe *syntax.ParamExp) []string {
	if pe == nil || pe.Excl || pe.Length || pe.Width {
		return nil
	}
	if pe.Param.Value == "@" {
		return c.Env.Get("@").Value.([]string)
	}
	if anyOfLit(pe.Index, "@") == "" {
		return nil
	}
	val := c.Env.Get(pe.Param.Value).Value
	if x, ok := val.([]string); ok {
		return x
	}
	return nil
}

func (c *Context) expandUser(field string) string {
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
		return c.Env.Get("HOME").String() + rest
	}
	// TODO: don't hard-code os/user into the expansion package
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

func glob(pattern string, globStar bool) []string {
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
		if part == "**" && globStar {
			for i := range matches {
				// "a/**" should match "a/ a/b a/b/c ..."; note
				// how the zero-match case has a trailing
				// separator.
				matches[i] += string(filepath.Separator)
			}
			// expand all the possible levels of **
			latest := matches
			for {
				var newMatches []string
				for _, dir := range latest {
					newMatches = globDir(dir, rxGlobStar, newMatches)
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
			newMatches = globDir(dir, rx, newMatches)
		}
		matches = newMatches
	}
	return matches
}

func globDir(dir string, rx *regexp.Regexp, matches []string) []string {
	d, err := os.Open(dir)
	if err != nil {
		return nil
	}
	defer d.Close()

	names, _ := d.Readdirnames(-1)
	sort.Strings(names)

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

func (c *Context) ReadFields(s string, n int, raw bool) []string {
	c.prepareIFS()
	type pos struct {
		start, end int
	}
	var fpos []pos

	runes := make([]rune, 0, len(s))
	infield := false
	esc := false
	for _, r := range s {
		if infield {
			if c.ifsRune(r) && (raw || !esc) {
				fpos[len(fpos)-1].end = len(runes)
				infield = false
			}
		} else {
			if !c.ifsRune(r) && (raw || !esc) {
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
