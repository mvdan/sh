// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"mvdan.cc/sh/expand"
	"mvdan.cc/sh/syntax"
)

type ExpandContext struct {
	Env Environ

	NoGlob   bool
	GlobStar bool

	// if nil, errors cause a panic.
	OnError func(error)

	bufferAlloc bytes.Buffer
	fieldAlloc  [4]fieldPart
	fieldsAlloc [4][]fieldPart

	// TODO: port these too
	sub func(context.Context, syntax.StmtList) string

	ifs string
	// A pointer to a parameter expansion node, if we're inside one.
	// Necessary for ${LINENO}.
	curParam *syntax.ParamExp
}

func (e *ExpandContext) ifsRune(r rune) bool {
	for _, r2 := range e.ifs {
		if r == r2 {
			return true
		}
	}
	return false
}

func (e *ExpandContext) ifsJoin(strs []string) string {
	sep := ""
	if e.ifs != "" {
		sep = e.ifs[:1]
	}
	return strings.Join(strs, sep)
}

func (e *ExpandContext) err(err error) {
	if e.OnError == nil {
		panic(err)
	}
	e.OnError(err)
}

func (e *ExpandContext) strBuilder() *bytes.Buffer {
	b := &e.bufferAlloc
	b.Reset()
	return b
}

func (e *ExpandContext) envGet(name string) string {
	val := e.Env.Get(name).Value
	if val == nil {
		return ""
	}
	return val.String()
}

func (e *ExpandContext) envSet(name, value string) {
	e.Env.Set(name, Variable{Value: StringVal(value)})
}

func (e *ExpandContext) loneWord(ctx context.Context, word *syntax.Word) string {
	if word == nil {
		return ""
	}
	field := e.wordField(ctx, word.Parts, quoteDouble)
	return e.fieldJoin(field)
}

func (e *ExpandContext) expandFormat(format string, args []string) (int, string, error) {
	buf := e.strBuilder()
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
					return 0, "", fmt.Errorf("invalid format char: %c", c)
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
				return 0, "", fmt.Errorf("invalid format char: %c", c)
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
		return 0, "", fmt.Errorf("missing format char")
	}
	return initialArgs - len(args), buf.String(), nil
}

func (e *ExpandContext) fieldJoin(parts []fieldPart) string {
	switch len(parts) {
	case 0:
		return ""
	case 1: // short-cut without a string copy
		return parts[0].val
	}
	buf := e.strBuilder()
	for _, part := range parts {
		buf.WriteString(part.val)
	}
	return buf.String()
}

func (e *ExpandContext) escapedGlobField(parts []fieldPart) (escaped string, glob bool) {
	buf := e.strBuilder()
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

func (e *ExpandContext) Fields(ctx context.Context, words ...*syntax.Word) []string {
	e.ifs = e.envGet("IFS")

	fields := make([]string, 0, len(words))
	dir := e.envGet("PWD")
	baseDir := syntax.QuotePattern(dir)
	for _, word := range words {
		for _, expWord := range expand.Braces(word) {
			for _, field := range e.wordFields(ctx, expWord.Parts) {
				path, doGlob := e.escapedGlobField(field)
				var matches []string
				abs := filepath.IsAbs(path)
				if doGlob && !e.NoGlob {
					if !abs {
						path = filepath.Join(baseDir, path)
					}
					matches = glob(path, e.GlobStar)
				}
				if len(matches) == 0 {
					fields = append(fields, e.fieldJoin(field))
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
	}
	return fields
}

func (e *ExpandContext) lonePattern(ctx context.Context, word *syntax.Word) string {
	field := e.wordField(ctx, word.Parts, quoteSingle)
	buf := e.strBuilder()
	for _, part := range field {
		if part.quote > quoteNone {
			buf.WriteString(syntax.QuotePattern(part.val))
		} else {
			buf.WriteString(part.val)
		}
	}
	return buf.String()
}

func (e *ExpandContext) expandAssigns(ctx context.Context, as *syntax.Assign) []*syntax.Assign {
	// Convert "declare $x" into "declare value".
	// Don't use syntax.Parser here, as we only want the basic
	// splitting by '='.
	if as.Name != nil {
		return []*syntax.Assign{as} // nothing to do
	}
	var asgns []*syntax.Assign
	for _, field := range e.Fields(ctx, as.Value) {
		as := &syntax.Assign{}
		parts := strings.SplitN(field, "=", 2)
		as.Name = &syntax.Lit{Value: parts[0]}
		if len(parts) == 1 {
			as.Naked = true
		} else {
			as.Value = &syntax.Word{Parts: []syntax.WordPart{
				&syntax.Lit{Value: parts[1]},
			}}
		}
		asgns = append(asgns, as)
	}
	return asgns
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

func (e *ExpandContext) wordField(ctx context.Context, wps []syntax.WordPart, ql quoteLevel) []fieldPart {
	var field []fieldPart
	for i, wp := range wps {
		switch x := wp.(type) {
		case *syntax.Lit:
			s := x.Value
			if i == 0 {
				s = e.expandUser(s)
			}
			if ql == quoteDouble && strings.Contains(s, "\\") {
				buf := e.strBuilder()
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
				_, fp.val, _ = e.expandFormat(fp.val, nil)
			}
			field = append(field, fp)
		case *syntax.DblQuoted:
			for _, part := range e.wordField(ctx, x.Parts, quoteDouble) {
				part.quote = quoteDouble
				field = append(field, part)
			}
		case *syntax.ParamExp:
			field = append(field, fieldPart{val: e.paramExp(ctx, x)})
		case *syntax.CmdSubst:
			field = append(field, fieldPart{val: e.cmdSubst(ctx, x)})
		case *syntax.ArithmExp:
			field = append(field, fieldPart{
				val: strconv.Itoa(e.arithm(ctx, x.X)),
			})
		default:
			panic(fmt.Sprintf("unhandled word part: %T", x))
		}
	}
	return field
}

func (e *ExpandContext) cmdSubst(ctx context.Context, cs *syntax.CmdSubst) string {
	out := e.sub(ctx, cs.StmtList)
	return strings.TrimRight(out, "\n")
}

func (e *ExpandContext) wordFields(ctx context.Context, wps []syntax.WordPart) [][]fieldPart {
	fields := e.fieldsAlloc[:0]
	curField := e.fieldAlloc[:0]
	allowEmpty := false
	flush := func() {
		if len(curField) == 0 {
			return
		}
		fields = append(fields, curField)
		curField = nil
	}
	splitAdd := func(val string) {
		for i, field := range strings.FieldsFunc(val, e.ifsRune) {
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
				s = e.expandUser(s)
			}
			if strings.Contains(s, "\\") {
				buf := e.strBuilder()
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
				_, fp.val, _ = e.expandFormat(fp.val, nil)
			}
			curField = append(curField, fp)
		case *syntax.DblQuoted:
			allowEmpty = true
			if len(x.Parts) == 1 {
				pe, _ := x.Parts[0].(*syntax.ParamExp)
				if elems := e.quotedElems(pe); elems != nil {
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
			for _, part := range e.wordField(ctx, x.Parts, quoteDouble) {
				part.quote = quoteDouble
				curField = append(curField, part)
			}
		case *syntax.ParamExp:
			splitAdd(e.paramExp(ctx, x))
		case *syntax.CmdSubst:
			splitAdd(e.cmdSubst(ctx, x))
		case *syntax.ArithmExp:
			curField = append(curField, fieldPart{
				val: strconv.Itoa(e.arithm(ctx, x.X)),
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
func (e *ExpandContext) quotedElems(pe *syntax.ParamExp) []string {
	if pe == nil || pe.Excl || pe.Length || pe.Width {
		return nil
	}
	if pe.Param.Value == "@" {
		return e.Env.Get("@").Value.(IndexArray)
	}
	if anyOfLit(pe.Index, "@") == "" {
		return nil
	}
	val := e.Env.Get(pe.Param.Value).Value
	if x, ok := val.(IndexArray); ok {
		return x
	}
	return nil
}

func (e *ExpandContext) expandUser(field string) string {
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
		return e.Env.Get("HOME").Value.String() + rest
	}
	// TODO: don't hard-code os/user into the expansion package
	u, err := user.Lookup(name)
	if err != nil {
		return field
	}
	return u.HomeDir + rest
}

func match(pattern, name string) bool {
	expr, err := syntax.TranslatePattern(pattern, true)
	if err != nil {
		return false
	}
	rx := regexp.MustCompile("^" + expr + "$")
	return rx.MatchString(name)
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

func (e *ExpandContext) ifsFields(s string, n int, raw bool) []string {
	e.ifs = e.envGet("IFS")
	type pos struct {
		start, end int
	}
	var fpos []pos

	runes := make([]rune, 0, len(s))
	infield := false
	esc := false
	for _, c := range s {
		if infield {
			if e.ifsRune(c) && (raw || !esc) {
				fpos[len(fpos)-1].end = len(runes)
				infield = false
			}
		} else {
			if !e.ifsRune(c) && (raw || !esc) {
				fpos = append(fpos, pos{start: len(runes), end: -1})
				infield = true
			}
		}
		if c == '\\' {
			if raw || esc {
				runes = append(runes, c)
			}
			esc = !esc
			continue
		}
		runes = append(runes, c)
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
