// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"fmt"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"mvdan.cc/sh/syntax"
)

func (r *Runner) expandFormat(format string, args []string) (int, string, error) {
	buf := r.strBuilder()
	esc := false
	var fmts []rune
	n := len(args)

	for _, c := range format {
		if esc {
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
			continue
		}
		if len(fmts) > 0 {

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
				var farg interface{}
				arg := ""
				fmts = append(fmts, c)
				if len(args) > 0 {
					arg, args = args[0], args[1:]
				}
				switch c {
				case 's':
					farg = arg
				case 'd', 'i', 'u', 'o', 'x':
					n, _ := strconv.ParseInt(arg, 0, 0)
					if c == 'i' || c == 'u' {
						fmts[len(fmts)-1] = 'd'
					}
					if c == 'i' || c == 'd' {
						farg = int(n)
					} else {
						farg = uint(n)
					}
				}

				fmt.Fprintf(buf, string(fmts), farg)
				fmts = nil
			default:
				return 0, "", fmt.Errorf("invalid format char: %c", c)
			}

			continue
		}
		if c == '\\' {
			esc = true
		} else if args != nil && c == '%' {
			// if args == nil, we are not doing format
			// arguments
			fmts = []rune{c}
		} else {
			buf.WriteRune(c)
		}
	}
	if len(fmts) > 0 {
		return 0, "", fmt.Errorf("missing format char")
	}
	return n - len(args), buf.String(), nil
}

func (r *Runner) fieldJoin(parts []fieldPart) string {
	buf := r.strBuilder()
	for _, part := range parts {
		buf.WriteString(part.val)
	}
	return buf.String()
}

func (r *Runner) escapedGlobStr(val string) string {
	buf := r.strBuilder()
	for _, r := range val {
		switch r {
		case '*', '?', '\\', '[':
			buf.WriteByte('\\')
		}
		buf.WriteRune(r)
	}
	return buf.String()
}

func (r *Runner) escapedGlobField(parts []fieldPart) (escaped string, glob bool) {
	buf := r.strBuilder()
	for _, part := range parts {
		for _, r := range part.val {
			switch r {
			case '*', '?', '\\', '[':
				if part.quote > quoteNone {
					buf.WriteByte('\\')
				} else {
					glob = true
				}
			}
			buf.WriteRune(r)
		}
	}
	if glob {
		escaped = buf.String()
	}
	return escaped, glob
}

// TODO: consider making brace a special syntax Node

type brace struct {
	elems []*braceWord
}

// braceWord is like syntax.Word, but with braceWordPart.
type braceWord struct {
	parts []braceWordPart
}

// braceWordPart contains either syntax.WordPart or brace.
type braceWordPart interface{}

var (
	litLeftBrace  = &syntax.Lit{Value: "{"}
	litComma      = &syntax.Lit{Value: ","}
	litRightBrace = &syntax.Lit{Value: "}"}
)

func (r *Runner) splitBraces(word *syntax.Word) (*braceWord, bool) {
	any := false
	top := &r.braceAlloc
	*top = braceWord{parts: r.bracePartsAlloc[:0]}
	acc := top
	var cur *brace
	open := []*brace{}

	pop := func() *brace {
		old := cur
		open = open[:len(open)-1]
		if len(open) == 0 {
			cur = nil
			acc = top
		} else {
			cur = open[len(open)-1]
			acc = cur.elems[len(cur.elems)-1]
		}
		return old
	}

	for _, wp := range word.Parts {
		lit, ok := wp.(*syntax.Lit)
		if !ok {
			acc.parts = append(acc.parts, wp)
			continue
		}
		last := 0
		for j, r := range lit.Value {
			addlit := func() {
				if last == j {
					return // empty lit
				}
				l2 := *lit
				l2.Value = l2.Value[last:j]
				acc.parts = append(acc.parts, &l2)
			}
			switch r {
			case '{':
				addlit()
				acc = &braceWord{}
				cur = &brace{elems: []*braceWord{acc}}
				open = append(open, cur)
			case ',':
				if cur == nil {
					continue
				}
				addlit()
				acc = &braceWord{}
				cur.elems = append(cur.elems, acc)
			case '}':
				if cur == nil {
					continue
				}
				any = true
				addlit()
				ended := pop()
				if len(ended.elems) > 1 {
					acc.parts = append(acc.parts, ended)
					break
				}
				// return {x} to a non-brace
				acc.parts = append(acc.parts, litLeftBrace)
				acc.parts = append(acc.parts, ended.elems[0].parts...)
				acc.parts = append(acc.parts, litRightBrace)
			default:
				continue
			}
			last = j + 1
		}
		if last == 0 {
			acc.parts = append(acc.parts, lit)
		} else {
			left := *lit
			left.Value = left.Value[last:]
			acc.parts = append(acc.parts, &left)
		}
	}
	// open braces that were never closed fall back to non-braces
	for acc != top {
		ended := pop()
		acc.parts = append(acc.parts, litLeftBrace)
		for i, elem := range ended.elems {
			if i > 0 {
				acc.parts = append(acc.parts, litComma)
			}
			acc.parts = append(acc.parts, elem.parts...)
		}
	}
	return top, any
}

func expandRec(bw *braceWord) []*syntax.Word {
	var all []*syntax.Word
	var left []syntax.WordPart
	for i, wp := range bw.parts {
		br, ok := wp.(*brace)
		if !ok {
			left = append(left, wp.(syntax.WordPart))
			continue
		}
		for _, elem := range br.elems {
			next := *bw
			next.parts = next.parts[i+1:]
			next.parts = append(elem.parts, next.parts...)
			exp := expandRec(&next)
			for _, w := range exp {
				w.Parts = append(left, w.Parts...)
			}
			all = append(all, exp...)
		}
		return all
	}
	return []*syntax.Word{{Parts: left}}
}

func (r *Runner) expandBraces(word *syntax.Word) []*syntax.Word {
	// TODO: be a no-op when not in bash mode
	topBrace, any := r.splitBraces(word)
	if !any {
		r.oneWord[0] = word
		return r.oneWord[:]
	}
	return expandRec(topBrace)
}

func (r *Runner) Fields(words ...*syntax.Word) []string {
	fields := make([]string, 0, len(words))
	baseDir := r.escapedGlobStr(r.Dir)
	for _, word := range words {
		for _, expWord := range r.expandBraces(word) {
			for _, field := range r.wordFields(expWord.Parts, quoteNone) {
				path, glob := r.escapedGlobField(field)
				var matches []string
				abs := filepath.IsAbs(path)
				if glob && !r.shellOpts[optNoGlob] {
					if !abs {
						path = filepath.Join(baseDir, path)
					}
					matches, _ = filepath.Glob(path)
				}
				if len(matches) == 0 {
					fields = append(fields, r.fieldJoin(field))
					continue
				}
				for _, match := range matches {
					if !abs {
						match, _ = filepath.Rel(r.Dir, match)
					}
					fields = append(fields, match)
				}
			}
		}
	}
	return fields
}

func (r *Runner) loneWord(word *syntax.Word) string {
	if word == nil {
		return ""
	}
	fields := r.wordFields(word.Parts, quoteDouble)
	if len(fields) != 1 {
		panic("expected exactly one field for a lone word")
	}
	buf := r.strBuilder()
	for _, part := range fields[0] {
		buf.WriteString(part.val)
	}
	return buf.String()
}

func (r *Runner) lonePattern(word *syntax.Word) string {
	if word == nil {
		return ""
	}
	fields := r.wordFields(word.Parts, quoteNone)
	if len(fields) == 0 {
		return ""
	}
	if len(fields) != 1 {
		panic("expected exactly one field for a pattern")
	}
	buf := r.strBuilder()
	for _, part := range fields[0] {
		if part.quote == quoteNone {
			for _, r := range part.val {
				if r == '\\' {
					buf.WriteString(`\\`)
				} else {
					buf.WriteRune(r)
				}
			}
			continue
		}
		for _, r := range part.val {
			switch r {
			case '*', '?', '[':
				buf.WriteByte('\\')
			}
			buf.WriteRune(r)
		}
	}
	return buf.String()
}

func (r *Runner) expandAssigns(as *syntax.Assign) []*syntax.Assign {
	// Convert "declare $x" into "declare value".
	// Don't use syntax.Parser here, as we only want the basic
	// splitting by '='.
	if as.Name != nil {
		return []*syntax.Assign{as} // nothing to do
	}
	var asgns []*syntax.Assign
	for _, field := range r.Fields(as.Value) {
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

func (r *Runner) wordFields(wps []syntax.WordPart, ql quoteLevel) [][]fieldPart {
	fields := r.fieldsAlloc[:0]
	var curField []fieldPart
	allowEmpty := false
	flush := func() {
		if len(curField) == 0 {
			return
		}
		fields = append(fields, curField)
		curField = nil
	}
	splitAdd := func(val string) {
		for i, field := range strings.FieldsFunc(val, r.ifsRune) {
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
				s = r.expandUser(s)
			}
			buf := r.strBuilder()
			for i := 0; i < len(s); i++ {
				b := s[i]
				switch {
				case ql == quoteSingle:
					// never does anything
				case b != '\\':
					// we want a backslash
				case ql == quoteDouble:
					if i+1 >= len(s) {
						break
					}
					switch s[i+1] {
					case '\n': // remove \\\n
						i++
						continue
					case '\\', '$', '`': // escaped special chars
						continue
					}
				default:
					i++
					b = s[i]
				}
				buf.WriteByte(b)
			}
			s = buf.String()
			curField = append(curField, fieldPart{val: s})
		case *syntax.SglQuoted:
			allowEmpty = true
			fp := fieldPart{quote: quoteSingle, val: x.Value}
			if x.Dollar {
				_, fp.val, _ = r.expandFormat(fp.val, nil)
			}
			curField = append(curField, fp)
		case *syntax.DblQuoted:
			quote := quoteDouble
			if x.Dollar {
				quote = quoteSingle
			}
			allowEmpty = true
			if len(x.Parts) == 1 {
				pe, _ := x.Parts[0].(*syntax.ParamExp)
				if elems := r.quotedElems(pe); elems != nil {
					for i, elem := range elems {
						if i > 0 {
							flush()
						}
						curField = append(curField, fieldPart{
							quote: quote,
							val:   elem,
						})
					}
					continue
				}
			}
			for _, field := range r.wordFields(x.Parts, quote) {
				for _, part := range field {
					curField = append(curField, fieldPart{
						quote: quote,
						val:   part.val,
					})
				}
			}
		case *syntax.ParamExp:
			val := r.paramExp(x)
			if ql > quoteNone {
				curField = append(curField, fieldPart{val: val})
			} else {
				splitAdd(val)
			}
		case *syntax.CmdSubst:
			r2 := r.sub()
			buf := r.strBuilder()
			r2.Stdout = buf
			r2.stmts(x.StmtList)
			val := strings.TrimRight(buf.String(), "\n")
			if ql > quoteNone {
				curField = append(curField, fieldPart{val: val})
			} else {
				splitAdd(val)
			}
			r.setErr(r2.err)
		case *syntax.ArithmExp:
			curField = append(curField, fieldPart{
				val: strconv.Itoa(r.arithm(x.X)),
			})
		default:
			panic(fmt.Sprintf("unhandled word part: %T", x))
		}
	}
	flush()
	if allowEmpty && len(fields) == 0 {
		fields = append(fields, []fieldPart{{}})
	}
	return fields
}

func (r *Runner) expandUser(field string) string {
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
		return r.getVar("HOME") + rest
	}
	u, err := user.Lookup(name)
	if err != nil {
		return field
	}
	return u.HomeDir + rest
}
