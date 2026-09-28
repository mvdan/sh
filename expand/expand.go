// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package expand

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"maps"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unicode/utf8"

	"mvdan.cc/sh/v3/internal"
	"mvdan.cc/sh/v3/pattern"
	"mvdan.cc/sh/v3/syntax"
)

// A Config specifies details about how shell expansion should be performed. The
// zero value is a valid configuration.
//
// Expanding never modifies a Config, so it can be reused and shared by
// concurrent calls, as long as fields like Env are safe for concurrent use.
type Config struct {
	// Env is used to get and set environment variables when performing
	// shell expansions. Some special parameters are also expanded via this
	// interface, such as:
	//
	//   * "#", "@", "*", "0"-"9" for the shell's parameters
	//   * "?", "$", "PPID" for the shell's status and process
	//   * "HOME foo" to retrieve user foo's home directory (if unset,
	//     os/user.Lookup will be used)
	//
	// If nil, there are no environment variables set. Use
	// ListEnviron(os.Environ()...) to use the system's environment
	// variables.
	Env Environ

	// CmdSubst expands a command substitution node, writing its standard
	// output to the provided [io.Writer].
	//
	// If nil, encountering a command substitution will result in an
	// UnexpectedCommandError.
	CmdSubst func(io.Writer, *syntax.CmdSubst) error

	// ProcSubst expands a process substitution node.
	//
	// If nil, encountering a process substitution will result in an error.
	ProcSubst func(*syntax.ProcSubst) (string, error)

	// TODO(v4): replace ReadDir with ReadDir2.

	// ReadDir is the older form of [ReadDir2], before io/fs.
	//
	// Deprecated: use ReadDir2 instead.
	ReadDir func(string) ([]fs.FileInfo, error)

	// ReadDir2 is used for file path globbing.
	// If nil, and [ReadDir] is nil as well, globbing is disabled.
	// Use [os.ReadDir] to use the filesystem directly.
	//
	// TODO(v4): globbing uses OS-specific paths, which is inconsistent and
	// buggy on Windows: backslashes are treated as separators even when they
	// escape pattern metacharacters, such as in '[a]'/*.go, a rooted pattern
	// like /foo/* is treated as relative, and results use backslashes.
	// Switch to io/fs semantics, with slash-separated paths relative to a root,
	// and let the caller map volumes and absolute paths onto that root.
	ReadDir2 func(string) ([]fs.DirEntry, error)

	// GlobStar corresponds to the shell option which allows globbing with "**".
	GlobStar bool

	// DotGlob corresponds to the shell option which allows filenames beginning
	// with a dot to be matched by a pattern which does not begin with a dot.
	DotGlob bool

	// NoCaseGlob corresponds to the shell option which causes case-insensitive
	// pattern matching in pathname expansion.
	NoCaseGlob bool

	// NullGlob corresponds to the shell option which allows globbing
	// patterns which match nothing to result in zero fields.
	NullGlob bool

	// NoUnset corresponds to the shell option which treats unset variables
	// as errors.
	NoUnset bool

	// ExtGlob corresponds to the shell option which allows using extended
	// pattern matching features when performing pathname expansion (globbing).
	ExtGlob bool
}

// UnexpectedCommandError is returned if a command substitution is encountered
// when [Config.CmdSubst] is nil.
type UnexpectedCommandError struct {
	Node *syntax.CmdSubst
}

func (u UnexpectedCommandError) Error() string {
	return fmt.Sprintf("unexpected command substitution at %s", u.Node.Pos())
}

// expander holds the state for a single expansion call with a copy of
// a [Config], so that the caller's can be reused and shared by concurrent calls.
type expander struct {
	Config

	ifs string

	bufferAlloc strings.Builder
	fieldAlloc  [4]fieldPart
	fieldsAlloc [4][]fieldPart

	// A pointer to a parameter expansion node, if we're inside one.
	// Necessary for ${LINENO}.
	curParam *syntax.ParamExp
}

// expanderPool avoids allocating an expander for each expansion call,
// as the interpreter makes many of them.
//
// TODO(v4): expose a reusable Expander instead; see doc/plan-v4.md.
var expanderPool = sync.Pool{New: func() any { return new(expander) }}

// newExpander returns an expander for cfg,
// which must be released once its results are no longer used.
func newExpander(cfg *Config) *expander {
	e := expanderPool.Get().(*expander)
	if cfg != nil {
		e.Config = *cfg
	}
	if e.Env == nil {
		e.Env = FuncEnviron(func(string) string { return "" })
	}
	e.updateIFS()
	return e
}

func (e *expander) release() {
	*e = expander{}
	expanderPool.Put(e)
}

// readDir2 uses [Config.ReadDir2], falling back to [Config.ReadDir].
func (e *expander) readDir2(path string) ([]fs.DirEntry, error) {
	if e.ReadDir2 != nil {
		return e.ReadDir2(path)
	}
	infos, err := e.ReadDir(path)
	if err != nil {
		return nil, err
	}
	entries := make([]fs.DirEntry, len(infos))
	for i, info := range infos {
		entries[i] = fs.FileInfoToDirEntry(info)
	}
	return entries, nil
}

func (e *expander) updateIFS() {
	e.ifs = " \t\n"
	if vr := e.Env.Get("IFS"); vr.IsSet() {
		e.ifs = vr.String()
	}
}

func (e *expander) ifsRune(r rune) bool {
	return strings.ContainsRune(e.ifs, r)
}

// ifsWhitespace reports whether r is a space, tab, or newline present in IFS.
func (e *expander) ifsWhitespace(r rune) bool {
	return (r == ' ' || r == '\t' || r == '\n') && e.ifsRune(r)
}

func (e *expander) ifsJoin(strs []string) string {
	sep := ""
	if e.ifs != "" {
		// The separator is the first character of IFS, not the first byte.
		_, size := utf8.DecodeRuneInString(e.ifs)
		sep = e.ifs[:size]
	}
	return strings.Join(strs, sep)
}

func (e *expander) strBuilder() *strings.Builder {
	b := &e.bufferAlloc
	b.Reset()
	return b
}

func (e *expander) envGet(name string) string {
	return e.Env.Get(name).String()
}

func (e *expander) envSet(name, value string) error {
	wenv, ok := e.Env.(WriteEnviron)
	if !ok {
		return fmt.Errorf("environment is read-only")
	}
	prev := wenv.Get(name)
	if err := wenv.Set(name, Variable{Set: true, Exported: prev.Exported, Kind: String, Str: value}); err != nil {
		return err
	}
	if name == "IFS" {
		// Assignments like ${IFS=:} affect the rest of the expansion.
		e.updateIFS()
	}
	return nil
}

// Literal expands a single shell word. It is similar to [Fields], but the result
// is a single string. This is the behavior when a word is used as the value in
// a shell variable assignment, for example.
//
// The config specifies shell expansion options; nil behaves the same as an
// empty config.
func Literal(cfg *Config, word *syntax.Word) (string, error) {
	e := newExpander(cfg)
	defer e.release()
	return e.literal(word)
}

func (e *expander) literal(word *syntax.Word) (string, error) {
	if word == nil {
		return "", nil
	}
	field, err := e.wordField(word.Parts, quoteNone)
	if err != nil {
		return "", err
	}
	return e.fieldJoin(field), nil
}

// Document expands a single shell word as if it were a here-document body.
// It is similar to [Literal], but without brace expansion, tilde expansion, and
// globbing.
//
// The config specifies shell expansion options; nil behaves the same as an
// empty config.
func Document(cfg *Config, word *syntax.Word) (string, error) {
	if word == nil {
		return "", nil
	}
	e := newExpander(cfg)
	defer e.release()
	field, err := e.wordField(word.Parts, quoteHeredoc)
	if err != nil {
		return "", err
	}
	return e.fieldJoin(field), nil
}

// Pattern expands a single shell word as a pattern, using [pattern.QuoteMeta]
// on any non-quoted parts of the input word. The result can be used on
// [pattern.Regexp] directly.
//
// The config specifies shell expansion options; nil behaves the same as an
// empty config.
func Pattern(cfg *Config, word *syntax.Word) (string, error) {
	e := newExpander(cfg)
	defer e.release()
	return e.pattern(word)
}

func (e *expander) pattern(word *syntax.Word) (string, error) {
	if word == nil {
		return "", nil
	}
	field, err := e.wordField(word.Parts, quoteNone)
	if err != nil {
		return "", err
	}
	sb := e.strBuilder()
	for _, part := range field {
		if part.quote > quoteNone {
			sb.WriteString(pattern.QuoteMeta(part.val, 0))
		} else {
			sb.WriteString(part.val)
		}
	}
	return sb.String(), nil
}

// Format expands a format string with a number of arguments, following the
// shell's format specifications. These include printf(1), among others.
//
// The resulting string is returned, along with the number of arguments used.
// Note that the resulting string may contain null bytes, for example
// if the format string used `\x00`. The caller should terminate the string
// at the first null byte if needed, such as when expanding for `$'foo\x00bar'`.
//
// The config specifies shell expansion options; nil behaves the same as an
// empty config.
func Format(cfg *Config, format string, args []string) (string, int, error) {
	e := newExpander(cfg)
	defer e.release()
	return e.format(format, args)
}

func (e *expander) format(format string, args []string) (string, int, error) {
	sb := e.strBuilder()

	consumed, err := formatInto(sb, format, args)
	if err != nil {
		return "", 0, err
	}

	return sb.String(), consumed, err
}

func formatInto(sb *strings.Builder, format string, args []string) (int, error) {
	var fmts []byte
	initialArgs := len(args)

	for i := 0; i < len(format); i++ {
		// readDigits reads from 0 to max digits, either octal or
		// hexadecimal.
		readDigits := func(max int, hex bool) string {
			j := 0
			for ; j < max && i+j < len(format); j++ {
				c := format[i+j]
				if (c >= '0' && c <= '9') ||
					(hex && c >= 'a' && c <= 'f') ||
					(hex && c >= 'A' && c <= 'F') {
					// valid octal or hex char
				} else {
					break
				}
			}
			digits := format[i : i+j]
			i += j - 1 // -1 since the outer loop does i++
			return digits
		}
		c := format[i]
		switch {
		case c == '\\': // escaped
			i++
			if i >= len(format) {
				sb.WriteByte('\\')
				break
			}
			switch c = format[i]; c {
			case 'a': // bell
				sb.WriteByte('\a')
			case 'b': // backspace
				sb.WriteByte('\b')
			case 'e', 'E': // escape
				sb.WriteByte('\x1b')
			case 'f': // form feed
				sb.WriteByte('\f')
			case 'n': // new line
				sb.WriteByte('\n')
			case 'r': // carriage return
				sb.WriteByte('\r')
			case 't': // horizontal tab
				sb.WriteByte('\t')
			case 'v': // vertical tab
				sb.WriteByte('\v')
			case '\\', '\'', '"', '?': // just the character
				sb.WriteByte(c)
			case '0', '1', '2', '3', '4', '5', '6', '7':
				digits := readDigits(3, false)
				// if digits don't fit in 8 bits, 0xff via strconv
				n, _ := strconv.ParseUint(digits, 8, 8)
				sb.WriteByte(byte(n))
			case 'x', 'u', 'U':
				i++
				max := 2
				switch c {
				case 'u':
					max = 4
				case 'U':
					max = 8
				}
				digits := readDigits(max, true)
				if len(digits) > 0 {
					// can't error
					n, _ := strconv.ParseUint(digits, 16, 32)
					if c == 'x' {
						// always as a single byte
						sb.WriteByte(byte(n))
					} else {
						sb.WriteRune(rune(n))
					}
					break
				}
				fallthrough
			default: // no escape sequence
				sb.WriteByte('\\')
				sb.WriteByte(c)
			}
		case len(fmts) > 0:
			switch c {
			case '%':
				sb.WriteByte('%')
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
				sb.WriteByte(b)
				fmts = nil
			case '+', '-', ' ':
				if len(fmts) > 1 {
					return 0, fmt.Errorf("invalid format char: %c", c)
				}
				fmts = append(fmts, c)
			case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
				fmts = append(fmts, c)
			case 's', 'b', 'd', 'i', 'u', 'o', 'x':
				arg := ""
				if len(args) > 0 {
					arg, args = args[0], args[1:]
				}
				var farg any
				if c == 'b' {
					// Passing in nil for args ensures that % format
					// strings aren't processed; only escape sequences
					// will be handled.
					_, err := formatInto(sb, arg, nil)
					if err != nil {
						return 0, err
					}
				} else if c != 's' {
					n, _ := strconv.ParseInt(arg, 0, 0)
					if c == 'i' || c == 'd' {
						farg = int(n)
					} else {
						farg = uint(n)
					}
					if c == 'i' || c == 'u' {
						c = 'd'
					}
				} else {
					farg = arg
				}
				if farg != nil {
					fmts = append(fmts, c)
					fmt.Fprintf(sb, string(fmts), farg)
				}
				fmts = nil
			default:
				return 0, fmt.Errorf("invalid format char: %c", c)
			}
		case args != nil && c == '%':
			// if args == nil, we are not doing format
			// arguments
			fmts = []byte{c}
		default:
			sb.WriteByte(c)
		}
	}
	if len(fmts) > 0 {
		return 0, fmt.Errorf("missing format char")
	}
	return initialArgs - len(args), nil
}

func (e *expander) fieldJoin(parts []fieldPart) string {
	switch len(parts) {
	case 0:
		return ""
	case 1: // short-cut without a string copy
		return parts[0].val
	}
	sb := e.strBuilder()
	for _, part := range parts {
		sb.WriteString(part.val)
	}
	return sb.String()
}

func (e *expander) escapedGlobField(parts []fieldPart) (escaped string, glob bool) {
	candidate := false
	for _, part := range parts {
		if part.quote == quoteNone && strings.ContainsAny(part.val, "*?[") {
			candidate = true
			break
		}
	}
	if !candidate {
		return "", false
	}
	sb := e.strBuilder()
	for _, part := range parts {
		if part.quote > quoteNone {
			sb.WriteString(pattern.QuoteMeta(part.val, 0))
		} else {
			sb.WriteString(part.val)
		}
	}
	// Check the entire escaped word, as a bracket expression could span
	// multiple unquoted parts, such as `[a$x` where x holds "]".
	escaped = sb.String()
	if pattern.HasMeta(escaped, 0) {
		return escaped, true
	}
	return "", false
}

// Fields is a pre-iterators API which now wraps [FieldsSeq].
func Fields(cfg *Config, words ...*syntax.Word) ([]string, error) {
	var fields []string
	for s, err := range FieldsSeq(cfg, words...) {
		if err != nil {
			return nil, err
		}
		fields = append(fields, s)
	}
	return fields, nil
}

// FieldsSeq expands a number of words as if they were arguments in a shell
// command. This includes brace expansion, tilde expansion, parameter expansion,
// command substitution, arithmetic expansion, quote removal, and globbing.
func FieldsSeq(cfg *Config, words ...*syntax.Word) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		e := newExpander(cfg)
		defer e.release()
		e.fieldsSeq(words, yield)
	}
}

// fieldsSeq implements [FieldsSeq]. It is a separate method as a defer
// alongside range-over-func loops would move more variables to the heap.
func (e *expander) fieldsSeq(words []*syntax.Word, yield func(string, error) bool) {
	dir := e.envGet("PWD")
	expandWord := func(w *syntax.Word) (stop bool) {
		wfields, err := e.wordFields(w.Parts)
		if err != nil {
			yield("", err)
			return true
		}
		for _, field := range wfields {
			path, doGlob := e.escapedGlobField(field)
			if doGlob && (e.ReadDir2 != nil || e.ReadDir != nil) {
				// Note that globbing requires keeping a slice state, so it doesn't
				// really benefit from using an iterator.
				matches, err := e.glob(dir, path)
				if err != nil {
					// We avoid [errors.As] as it allocates,
					// and we know that [expander.glob] returns [pattern.Regexp] errors without wrapping.
					if _, ok := err.(*pattern.SyntaxError); !ok {
						yield("", err)
						return true
					}
				} else if len(matches) > 0 || e.NullGlob {
					for _, m := range matches {
						if !yield(m, nil) {
							return true
						}
					}
					continue
				}
			}
			if !yield(e.fieldJoin(field), nil) {
				return true
			}
		}
		return false
	}
	for _, word := range words {
		word := *word // make a copy, since SplitBraces replaces the Parts slice
		if !syntax.SplitBraces(&word) {
			if expandWord(&word) {
				return
			}
			continue
		}
		for w, err := range BracesSeq(&e.Config, &word) {
			if err != nil {
				yield("", err)
				return
			}
			if expandWord(w) {
				return
			}
		}
	}
}

type fieldPart struct {
	val   string
	quote quoteLevel
}

type quoteLevel uint

const (
	quoteNone quoteLevel = iota
	quoteDouble
	quoteHeredoc
	quoteSingle
)

func (e *expander) wordField(wps []syntax.WordPart, ql quoteLevel) ([]fieldPart, error) {
	var field []fieldPart
	for i, wp := range wps {
		switch wp := wp.(type) {
		case *syntax.Lit:
			s := wp.Value
			if i == 0 && ql == quoteNone {
				if prefix, rest := e.expandUser(s, len(wps) > 1); prefix != "" {
					// TODO: return two separate fieldParts,
					// like in wordFields?
					s = prefix + rest
				}
			}
			if (ql == quoteDouble || ql == quoteHeredoc) && strings.Contains(s, "\\") {
				sb := e.strBuilder()
				for i := 0; i < len(s); i++ {
					b := s[i]
					if b == '\\' && i+1 < len(s) {
						switch s[i+1] {
						case '"':
							if ql != quoteDouble {
								break
							}
							fallthrough
						case '\\', '$', '`': // special chars
							i++
							b = s[i] // write the special char, skipping the backslash
						}
					}
					sb.WriteByte(b)
				}
				s = sb.String()
			}
			s, _, _ = strings.Cut(s, "\x00") // TODO: why is this needed?
			field = append(field, fieldPart{val: s})
		case *syntax.SglQuoted:
			fp := fieldPart{quote: quoteSingle, val: wp.Value}
			if wp.Dollar {
				fp.val, _, _ = e.format(fp.val, nil)
				fp.val, _, _ = strings.Cut(fp.val, "\x00") // cut the string if format included \x00
			}
			field = append(field, fp)
		case *syntax.DblQuoted:
			wfield, err := e.wordField(wp.Parts, quoteDouble)
			if err != nil {
				return nil, err
			}
			for _, part := range wfield {
				part.quote = quoteDouble
				field = append(field, part)
			}
		case *syntax.ParamExp:
			val, err := e.paramExp(wp)
			if err != nil {
				return nil, err
			}
			field = append(field, fieldPart{val: val})
		case *syntax.CmdSubst:
			val, err := e.cmdSubst(wp)
			if err != nil {
				return nil, err
			}
			field = append(field, fieldPart{val: val})
		case *syntax.ArithmExp:
			n, err := e.arithm(wp.X)
			if err != nil {
				return nil, err
			}
			field = append(field, fieldPart{val: strconv.Itoa(n)})
		case *syntax.ProcSubst:
			path, err := e.procSubst(wp)
			if err != nil {
				return nil, err
			}
			field = append(field, fieldPart{val: path})
		case *syntax.ExtGlob:
			// Like how [expander.wordFields] deals with [syntax.ExtGlob],
			// except that we allow these through even when [Config.ExtGlob]
			// is false, as it only applies to pathname expansion.
			field = append(field, fieldPart{val: wp.Op.String() + wp.Pattern.Value + ")"})
		default:
			panic(fmt.Sprintf("unhandled word part: %T", wp))
		}
	}
	return field, nil
}

func (e *expander) procSubst(ps *syntax.ProcSubst) (string, error) {
	if e.ProcSubst == nil {
		return "", fmt.Errorf("unexpected process substitution at %s", ps.Pos())
	}
	return e.ProcSubst(ps)
}

func (e *expander) cmdSubst(cs *syntax.CmdSubst) (string, error) {
	if e.CmdSubst == nil {
		return "", UnexpectedCommandError{Node: cs}
	}
	sb := e.strBuilder()
	if err := e.CmdSubst(sb, cs); err != nil {
		return "", err
	}
	out := sb.String()
	out = strings.ReplaceAll(out, "\x00", "")
	return strings.TrimRight(out, "\n"), nil
}

func (e *expander) wordFields(wps []syntax.WordPart) ([][]fieldPart, error) {
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
		fieldStart := -1
		for i, r := range val {
			if e.ifsRune(r) {
				if fieldStart >= 0 { // ending a field
					curField = append(curField, fieldPart{val: val[fieldStart:i]})
					fieldStart = -1
				}
				flush()
			} else {
				if fieldStart < 0 { // starting a new field
					fieldStart = i
				}
			}
		}
		if fieldStart >= 0 { // ending a field without IFS
			curField = append(curField, fieldPart{val: val[fieldStart:]})
		}
	}
	for i, wp := range wps {
		switch wp := wp.(type) {
		case *syntax.Lit:
			s := wp.Value
			if i == 0 {
				prefix, rest := e.expandUser(s, len(wps) > 1)
				curField = append(curField, fieldPart{
					quote: quoteSingle,
					val:   prefix,
				})
				s = rest
			}
			// Escaped characters are quoted, so that they aren't
			// treated as metacharacters when globbing.
			for {
				i := strings.IndexByte(s, '\\')
				if i < 0 || i == len(s)-1 {
					break // a trailing backslash is kept as-is
				}
				_, size := utf8.DecodeRuneInString(s[i+1:])
				curField = append(curField,
					fieldPart{val: s[:i]},
					fieldPart{quote: quoteSingle, val: s[i+1 : i+1+size]},
				)
				s = s[i+1+size:]
			}
			curField = append(curField, fieldPart{val: s})
		case *syntax.SglQuoted:
			allowEmpty = true
			fp := fieldPart{quote: quoteSingle, val: wp.Value}
			if wp.Dollar {
				fp.val, _, _ = e.format(fp.val, nil)
				fp.val, _, _ = strings.Cut(fp.val, "\x00") // cut the string if format included \x00
			}
			curField = append(curField, fp)
		case *syntax.DblQuoted:
			if len(wp.Parts) == 1 {
				pe, _ := wp.Parts[0].(*syntax.ParamExp)
				elems, err := e.quotedElemFields(pe)
				if err != nil {
					return nil, err
				}
				if elems != nil {
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
			allowEmpty = true
			wfield, err := e.wordField(wp.Parts, quoteDouble)
			if err != nil {
				return nil, err
			}
			for _, part := range wfield {
				part.quote = quoteDouble
				curField = append(curField, part)
			}
		case *syntax.ParamExp:
			if elems, ok := e.unquotedElemFields(wp); ok {
				// Unquoted "*" or "@" expansions produce one field per
				// element; joining and re-splitting them would lose
				// fields when IFS is empty.
				for j, elem := range elems {
					if j > 0 {
						flush()
					}
					splitAdd(elem)
				}
				continue
			}
			val, err := e.paramExp(wp)
			if err != nil {
				return nil, err
			}
			splitAdd(val)
		case *syntax.CmdSubst:
			val, err := e.cmdSubst(wp)
			if err != nil {
				return nil, err
			}
			splitAdd(val)
		case *syntax.ArithmExp:
			n, err := e.arithm(wp.X)
			if err != nil {
				return nil, err
			}
			curField = append(curField, fieldPart{val: strconv.Itoa(n)})
		case *syntax.ProcSubst:
			path, err := e.procSubst(wp)
			if err != nil {
				return nil, err
			}
			splitAdd(path)
		case *syntax.ExtGlob:
			if !e.ExtGlob {
				return nil, fmt.Errorf("extended globbing operator used without the \"extglob\" option set")
			}
			// We don't translate or interpret the pattern here in any way;
			// that's done later when globbing takes place via [pattern.Regexp].
			// Here, all we do is keep the extended globbing expression in string form.
			//
			// TODO(v4): perhaps the syntax parser should keep extended globbing expressions
			// as plain literal strings, because a custom node is not particularly helpful.
			// It's not like other globbing operators like `*` or `**` get their own nodes.
			curField = append(curField, fieldPart{val: wp.Op.String() + wp.Pattern.Value + ")"})
		default:
			panic(fmt.Sprintf("unhandled word part: %T", wp))
		}
	}
	flush()
	if allowEmpty && len(fields) == 0 {
		fields = append(fields, curField)
	}
	return fields, nil
}

// listElems returns the elements of a "*" or "@" expansion of a list, like
// $@ or ${arr[*]}, with star set for the "*" forms which join into a single
// field when quoted. ok is false for any other parameter expansion.
func (e *expander) listElems(pe *syntax.ParamExp) (elems []string, star, ok bool) {
	if pe.Param == nil { // e.g. zsh's ${}; paramExp rejects it
		return nil, false, false
	}
	switch name := pe.Param.Value; name {
	case "*", "@":
		return e.sliceElems(pe, e.Env.Get(name).List, nil, true), name == "*", true
	}
	switch lit := nodeLit(pe.Index); lit {
	case "@", "*":
		switch vr := e.Env.Get(pe.Param.Value); vr.Kind {
		case Indexed:
			return e.sliceElems(pe, vr.List, vr.Indexes, false), lit == "*", true
		case Associative:
			return slices.Sorted(maps.Values(vr.Map)), lit == "*", true
		}
	}
	return nil, false, false
}

// unquotedElemFields returns the elements of an unquoted "*" or "@" list
// expansion like $* or ${foo[@]}; ok is false for any other expansion.
func (e *expander) unquotedElemFields(pe *syntax.ParamExp) ([]string, bool) {
	if pe.Excl || pe.Length || pe.Width || pe.IsSet || pe.Repl != nil || pe.Exp != nil {
		return nil, false
	}
	elems, _, ok := e.listElems(pe)
	return elems, ok
}

// quotedElemFields returns the list of elements resulting from a quoted
// parameter expansion that should be treated especially, like "${foo[@]}".
// The result is nil for any other parameter expansion.
func (e *expander) quotedElemFields(pe *syntax.ParamExp) ([]string, error) {
	if pe == nil || pe.Param == nil || pe.Length || pe.Width || pe.IsSet {
		return nil, nil
	}
	name := pe.Param.Value
	if pe.Excl {
		switch pe.Names {
		case syntax.NamesPrefixWords: // "${!prefix@}"
			return e.namesByPrefix(pe.Param.Value), nil
		case syntax.NamesPrefix: // "${!prefix*}"
			return nil, nil
		}
		switch nodeLit(pe.Index) {
		case "@": // "${!name[@]}"
			switch vr := e.Env.Get(name); vr.Kind {
			case Indexed:
				return vr.indexedKeys(), nil
			case Associative:
				return slices.Collect(maps.Keys(vr.Map)), nil
			}
		}
		return nil, nil
	}
	if nodeLit(pe.Index) == "@" && !overridingUnset(pe) && !e.Env.Get(name).IsSet() {
		// An unset "${name[@]}" produces zero fields, like an empty array.
		return []string{}, nil
	}
	if elems, star, ok := e.listElems(pe); ok {
		// Operators like "${foo[@]#prefix}" apply to each element.
		elems, err := e.perElemOps(pe, elems)
		if err != nil {
			return nil, err
		}
		if star {
			return []string{e.ifsJoin(elems)}, nil
		}
		return elems, nil
	}
	return nil, nil
}

// sliceElems applies ${var:offset:length} slicing to a list of elements.
// When positional is true, $0 is prepended to the list before slicing.
// In bash, positional parameter offsets ($@ and $*) are 1-based and
// offset 0 includes $0 (the shell or script name). Negative offsets
// count from $# + 1, so $0 is reachable via large enough negative values.
// A non-nil indexes records the index of each element in a sparse array;
// see [Variable.Indexes].
func (e *expander) sliceElems(pe *syntax.ParamExp, elems []string, indexes []int, positional bool) []string {
	if pe.Slice == nil {
		return elems
	}
	if positional {
		elems = append([]string{e.Env.Get("0").Str}, elems...)
	}
	slicePos := func(n int) int {
		if n < 0 {
			n = len(elems) + n
			if n < 0 {
				n = len(elems)
			}
		} else if n > len(elems) {
			n = len(elems)
		}
		return n
	}
	if pe.Slice.Offset != nil {
		offset, err := e.arithm(pe.Slice.Offset)
		if err != nil {
			return elems
		}
		if len(indexes) > 0 {
			// Sparse arrays slice by index: a negative offset counts
			// from one past the maximum index, and the result begins
			// with the first element whose index is at least the offset.
			if offset < 0 {
				offset += indexes[len(indexes)-1] + 1
				if offset < 0 {
					offset = indexes[len(indexes)-1] + 1
				}
			}
			pos, _ := slices.BinarySearch(indexes, offset)
			elems = elems[pos:]
		} else {
			elems = elems[slicePos(offset):]
		}
	}
	if pe.Slice.Length != nil {
		length, err := e.arithm(pe.Slice.Length)
		if err != nil {
			return elems
		}
		elems = elems[:slicePos(length)]
	}
	return elems
}

func (e *expander) expandUser(field string, moreFields bool) (prefix, rest string) {
	name, ok := strings.CutPrefix(field, "~")
	if !ok {
		// No tilde prefix to expand, e.g. "foo".
		return "", field
	}
	i := strings.IndexByte(name, '/')
	if i < 0 && moreFields {
		// There is a tilde prefix, but followed by more fields, e.g. "~'foo'".
		// We only proceed if an unquoted slash was found in this field, e.g. "~/'foo'".
		return "", field
	}
	if i >= 0 {
		rest = name[i:]
		name = name[:i]
	}
	if name == "" {
		// Current user; try via "HOME", otherwise fall back to the
		// system's appropriate home dir env var. Don't use os/user, as
		// that's overkill. We can't use [os.UserHomeDir], because we want
		// to use e.Env, and we always want to check "HOME" first.

		if vr := e.Env.Get("HOME"); vr.IsSet() {
			return vr.String(), rest
		}

		if runtime.GOOS == "windows" {
			if vr := e.Env.Get("USERPROFILE"); vr.IsSet() {
				return vr.String(), rest
			}
		}
		return "", field
	}

	// Not the current user; try via "HOME <name>", otherwise fall back to
	// os/user. There isn't a way to lookup user home dirs without cgo.

	if vr := e.Env.Get("HOME " + name); vr.IsSet() {
		return vr.String(), rest
	}

	u, err := user.Lookup(name)
	if err != nil {
		return "", field
	}
	return u.HomeDir, rest
}

func findAllIndex(pat, name string, n int) [][]int {
	expr, err := pattern.Regexp(pat, 0)
	if err != nil {
		return nil
	}
	rx, err := regexp.Compile(expr)
	if err != nil {
		return nil
	}
	return rx.FindAllStringIndex(name, n)
}

var (
	rxGlobStar        = regexp.MustCompile(`^[^/.][^/]*$`)
	rxGlobStarDotGlob = regexp.MustCompile(`^[^/]*$`)
)

// pathJoin2 is a simpler version of [filepath.Join] without cleaning the result,
// since that's needed for globbing.
func pathJoin2(elem1, elem2 string) string {
	if elem1 == "" {
		return elem2
	}
	if strings.HasSuffix(elem1, string(filepath.Separator)) {
		return elem1 + elem2
	}
	return elem1 + string(filepath.Separator) + elem2
}

// pathSplit splits a file path into its elements, retaining empty ones. Before
// splitting, slashes are replaced with [filepath.Separator], so that splitting
// Unix paths on Windows works as well.
func pathSplit(path string) []string {
	path = filepath.FromSlash(path)
	return strings.Split(path, string(filepath.Separator))
}

func (e *expander) glob(base, pat string) ([]string, error) {
	parts := pathSplit(pat)
	matches := []string{""}
	if filepath.IsAbs(pat) {
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
	// TODO: as an optimization, we could do chunks of the path all at once,
	// like doing a single stat for "/foo/bar" in "/foo/bar/*".

	// TODO: Another optimization would be to reduce the number of ReadDir2 calls.
	// For example, /foo/* can end up doing one duplicate call:
	//
	//    ReadDir2("/foo") to ensure that "/foo/" exists and only matches a directory
	//    ReadDir2("/foo") glob "*"

	for i, part := range parts {
		// Keep around for debugging.
		// log.Printf("matches %q part %d %q", matches, i, part)

		wantDir := i < len(parts)-1
		switch {
		case part == "", part == ".", part == "..":
			for i, dir := range matches {
				matches[i] = pathJoin2(dir, part)
			}
			continue
		case !pattern.HasMeta(part, 0):
			var newMatches []string
			for _, dir := range matches {
				match := dir
				if !filepath.IsAbs(match) {
					match = filepath.Join(base, match)
				}
				match = pathJoin2(match, part)
				// We can't use [Config.ReadDir2] on the parent and match the directory
				// entry by name, because short paths on Windows break that.
				// Our only option is to [Config.ReadDir2] on the directory entry itself,
				// which can be wasteful if we only want to see if it exists,
				// but at least it's correct in all scenarios.
				if _, err := e.readDir2(match); err != nil {
					if errors.Is(err, syscall.ENOTDIR) {
						// Reading a regular file as a directory.
						// Note that on Windows this error also satisfies
						// [fs.ErrNotExist], so it must be checked first;
						// see https://github.com/golang/go/issues/46734.
					} else if errors.Is(err, fs.ErrNotExist) {
						continue // simply doesn't exist
					}
					if wantDir {
						continue // exists but not a directory
					}
				}
				newMatches = append(newMatches, pathJoin2(dir, part))
			}
			matches = newMatches
			continue
		case part == "**" && e.GlobStar:
			next := i + 1 // the next non-empty path element
			for next < len(parts) && parts[next] == "" {
				next++
			}
			// Find all recursive matches for "**".
			// Note that we need the results to be in depth-first order,
			// and to avoid recursion, we use a slice as a stack.
			// Since we pop from the back, we populate the stack backwards.
			type walkDir struct {
				path string
				link bool
			}
			stack := make([]walkDir, 0, len(matches))
			for _, match := range slices.Backward(matches) {
				// "a/**" should match "a/ a/b a/b/cfg ...";
				// note how the zero-match case there has a trailing separator.
				stack = append(stack, walkDir{path: pathJoin2(match, "")})
			}
			// Like Bash, don't walk symbolic links, which may form loops,
			// and only match them when "**" is the last path element.
			last := next == len(parts)
			matches = matches[:0]
			for len(stack) > 0 {
				dir := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				matches = append(matches, dir.path)
				if dir.link {
					continue
				}

				// If dir is not a directory, we keep the stack as-is and continue.
				rx := rxGlobStar.MatchString
				if e.DotGlob {
					rx = rxGlobStarDotGlob.MatchString
				}
				n := len(stack)
				e.globDir(base, dir.path, rx, wantDir, last, func(path string, link bool) {
					stack = append(stack, walkDir{path: path, link: link})
				})
				slices.Reverse(stack[n:])
			}
			continue
		}
		mode := pattern.Filenames | pattern.EntireString | pattern.NoGlobStar
		if e.NoCaseGlob {
			mode |= pattern.NoGlobCase
		}
		if e.DotGlob {
			mode |= pattern.GlobLeadingDot
		}
		if e.ExtGlob {
			mode |= pattern.ExtendedOperators
		}
		matcher, err := internal.ExtendedPatternMatcher(part, mode)
		if err != nil {
			return nil, err
		}
		var newMatches []string
		for _, dir := range matches {
			if err := e.globDir(base, dir, matcher, wantDir, true, func(path string, _ bool) {
				newMatches = append(newMatches, path)
			}); err != nil {
				return nil, err
			}
		}
		matches = newMatches
	}
	// Note that the results need to be sorted.
	// TODO: above we do a BFS; if we did a DFS, the matches would already be sorted.
	slices.Sort(matches)
	// Remove any empty matches left behind from "**".
	if len(matches) > 0 && matches[0] == "" {
		matches = matches[1:]
	}
	return matches, nil
}

// globDir calls match with the path of each entry in dir accepted by matcher,
// only including directories if wantDir is set and symbolic links if links is set,
// and whether the entry is a symbolic link.
func (e *expander) globDir(base, dir string, matcher func(string) bool, wantDir, links bool, match func(path string, link bool)) error {
	fullDir := dir
	if !filepath.IsAbs(dir) {
		fullDir = filepath.Join(base, dir)
	}
	infos, err := e.readDir2(fullDir)
	if err != nil {
		return err
	}
	for _, info := range infos {
		name := info.Name()
		mode := info.Type()
		link := mode&os.ModeSymlink != 0
		if link && !links {
			continue
		}
		if !wantDir {
			// No filtering.
		} else if link {
			// We need to know if the symlink points to a directory.
			// This requires an extra syscall, as [Config.ReadDir] on the parent directory
			// does not follow symlinks for each of the directory entries.
			// ReadDir is somewhat wasteful here, as we only want its error result,
			// but we could try to reuse its result as per the TODO in [expander.glob].
			if _, err := e.readDir2(filepath.Join(fullDir, info.Name())); err != nil {
				continue
			}
		} else if !mode.IsDir() {
			// Not a symlink nor a directory.
			continue
		}
		if matcher(name) {
			match(pathJoin2(dir, name), link)
		}
	}
	return nil
}

// ReadFields splits and returns n fields from s, like the "read" shell builtin.
// If raw is set, backslash escape sequences are not interpreted.
//
// The config specifies shell expansion options; nil behaves the same as an
// empty config.
func ReadFields(cfg *Config, s string, n int, raw bool) []string {
	e := newExpander(cfg)
	defer e.release()
	type pos struct {
		start, end int
	}
	var fpos []pos

	runes := make([]rune, 0, len(s))
	infield := false
	esc := false
	for _, r := range s {
		if infield {
			if e.ifsRune(r) && (raw || !esc) {
				fpos[len(fpos)-1].end = len(runes)
				infield = false
			}
		} else {
			if !e.ifsRune(r) && (raw || !esc) {
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
		// The single field spans the whole line minus leading and trailing
		// IFS whitespace; anything outside the fields is already IFS.
		lo, hi := 0, len(runes)
		for lo < fpos[0].start && e.ifsWhitespace(runes[lo]) {
			lo++
		}
		for hi > fpos[len(fpos)-1].end && e.ifsWhitespace(runes[hi-1]) {
			hi--
		}
		fpos[0].start, fpos[0].end = lo, hi
		fpos = fpos[:1]
	case n != -1 && n < len(fpos):
		// combine to max n fields
		fpos[n-1].end = fpos[len(fpos)-1].end
		fpos = fpos[:n]
	}

	fields := make([]string, len(fpos))
	for i, p := range fpos {
		fields[i] = string(runes[p.start:p.end])
	}
	return fields
}
