// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

// Package pattern allows working with shell pattern matching notation, also
// known as wildcards or globbing.
//
// For reference, see
// https://pubs.opengroup.org/onlinepubs/9699919799/utilities/V3_chap02.html#tag_18_13.
package pattern

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Mode can be used to supply a number of options to the package's functions.
// Not all functions change their behavior with all of the options below.
type Mode uint

type SyntaxError struct {
	msg string
	err error
}

func (e SyntaxError) Error() string { return e.msg }

func (e SyntaxError) Unwrap() error { return e.err }

// TODO(v4): flip NoGlobStar to be opt-in via GlobStar, matching bash
// TODO(v4): flip EntireString to be opt-out via PartialMatch, as EntireString causes subtle bugs when forgotten
// TODO(v4): rename NoGlobCase to CaseInsensitive for readability

const (
	Shortest     Mode = 1 << iota // prefer the shortest match.
	Filenames                     // "*" and "?" don't match slashes; only "**" does
	EntireString                  // match the entire string using ^$ delimiters
	NoGlobCase                    // Do case-insensitive match (that is, use (?i) in the regexp)
	NoGlobStar                    // Do not support "**"
)

// Regexp turns a shell pattern into a regular expression that can be used with
// [regexp.Compile]. It will return an error if the input pattern was incorrect.
// Otherwise, the returned expression can be passed to [regexp.MustCompile].
//
// For example, Regexp(`foo*bar?`, true) returns `foo.*bar.`.
//
// Note that this function (and [QuoteMeta]) should not be directly used with file
// paths if Windows is supported, as the path separator on that platform is the
// same character as the escaping character for shell patterns.
func Regexp(pat string, mode Mode) (string, error) {
	// If there are no special pattern matching or regular expression characters,
	// and we don't need to insert extras for the modes affecting non-special characters,
	// we can directly return the input string as a short-cut.
	if mode&(EntireString|NoGlobCase) == 0 {
		needsEscaping := false
	noopLoop:
		for _, r := range pat {
			switch r {
			// including those that need escaping since they are
			// regular expression metacharacters
			case '*', '?', '[', '\\', '.', '+', '(', ')', '|',
				']', '{', '}', '^', '$':
				needsEscaping = true
				break noopLoop
			}
		}
		if !needsEscaping {
			return pat, nil
		}
	}
	var sb strings.Builder
	// Enable matching `\n` with the `.` metacharacter as globs match `\n`
	sb.WriteString("(?s")
	if mode&NoGlobCase != 0 {
		sb.WriteString("i")
	}
	if mode&Shortest != 0 {
		sb.WriteString("U")
	}
	sb.WriteString(")")
	if mode&EntireString != 0 {
		sb.WriteString("^")
	}
	sl := stringLexer{s: pat}
	for {
		if err := regexpNext(&sb, &sl, mode); err == io.EOF {
			break
		} else if err != nil {
			return "", err
		}
	}
	if mode&EntireString != 0 {
		sb.WriteString("$")
	}
	return sb.String(), nil
}

// stringLexer helps us tokenize a pattern string.
// Note that we can use the null byte '\x00' to signal "no character" as shell strings cannot contain null bytes.
// TODO: should the tokenization be based on runes? e.g: [á-é]
type stringLexer struct {
	s string
	i int
}

func (sl *stringLexer) next() byte {
	if sl.i >= len(sl.s) {
		return '\x00'
	}
	c := sl.s[sl.i]
	sl.i++
	return c
}

func (sl *stringLexer) last() byte {
	if sl.i < 2 {
		return '\x00'
	}
	return sl.s[sl.i-2]
}

func (sl *stringLexer) peekNext() byte {
	if sl.i >= len(sl.s) {
		return '\x00'
	}
	return sl.s[sl.i]
}

func (sl *stringLexer) peekRest() string {
	return sl.s[sl.i:]
}

func regexpNext(sb *strings.Builder, sl *stringLexer, mode Mode) error {
	switch c := sl.next(); c {
	case '\x00':
		return io.EOF
	case '*':
		if mode&Filenames == 0 {
			// * - matches anything when not in filename mode
			sb.WriteString(".*")
			break
		}
		// "**" only acts as globstar if it is alone as a path element.
		singleBefore := sl.i == 1 || sl.last() == '/'
		if sl.peekNext() == '*' {
			sl.i++
			singleAfter := sl.i == len(sl.s) || sl.peekNext() == '/'
			if mode&NoGlobStar == 0 && singleBefore && singleAfter {
				if sl.peekNext() == '/' {
					// **/ - like "**" but requiring a trailing slash when matching
					sl.i++
					sb.WriteString("((/|[^/.][^/]*)*/)?")
				} else {
					// ** - match any number of slashes or "*" path elements
					sb.WriteString("(/|[^/.][^/]*)*")
				}
				break
			}
			// foo**, **bar, or NoGlobStar - behaves like "*" below
		}
		// * - matches anything except slashes and leading dots
		if singleBefore {
			sb.WriteString("([^/.][^/]*)?")
		} else {
			sb.WriteString("[^/]*")
		}
	case '?':
		if mode&Filenames != 0 {
			sb.WriteString("[^/]")
		} else {
			sb.WriteByte('.')
		}
	case '\\':
		c = sl.next()
		if c == '\x00' {
			return &SyntaxError{msg: `\ at end of pattern`}
		}
		sb.WriteString(regexp.QuoteMeta(string(c)))
	case '[':
		// TODO: surely char classes can be mixed with others, e.g. [[:foo:]xyz]
		if name, err := charClass(sl.peekRest()); err != nil {
			return &SyntaxError{msg: "charClass invalid", err: err}
		} else if name != "" {
			sb.WriteByte('[')
			sb.WriteString(name)
			sl.i += len(name)
			break
		}
		if mode&Filenames != 0 {
			for _, c := range sl.peekRest() {
				if c == ']' {
					break
				} else if c == '/' {
					sb.WriteString("\\[")
					return nil
				}
			}
		}
		sb.WriteByte(c)
		if c = sl.next(); c == '\x00' {
			return &SyntaxError{msg: "[ was not matched with a closing ]"}
		}
		switch c {
		case '!', '^':
			sb.WriteByte('^')
			if c = sl.next(); c == '\x00' {
				return &SyntaxError{msg: "[ was not matched with a closing ]"}
			}
		}
		if c == ']' {
			sb.WriteByte(']')
			if c = sl.next(); c == '\x00' {
				return &SyntaxError{msg: "[ was not matched with a closing ]"}
			}
		}
		for {
			sb.WriteByte(c)
			switch c {
			case '\x00':
				return &SyntaxError{msg: "[ was not matched with a closing ]"}
			case '\\':
				if c = sl.next(); c != '0' {
					sb.WriteByte(c)
				}
			case '-':
				start := sl.last()
				end := sl.peekNext()
				// TODO: what about overlapping ranges, like: [a--z]
				if end != ']' && start > end {
					return &SyntaxError{msg: fmt.Sprintf("invalid range: %c-%c", start, end)}
				}
			case ']':
				return nil
			}
			c = sl.next()
		}
	default:
		if c > utf8.RuneSelf {
			sb.WriteByte(c)
		} else {
			sb.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return nil
}

func charClass(s string) (string, error) {
	if strings.HasPrefix(s, "[.") || strings.HasPrefix(s, "[=") {
		return "", fmt.Errorf("collating features not available")
	}
	name, ok := strings.CutPrefix(s, "[:")
	if !ok {
		return "", nil
	}
	name, _, ok = strings.Cut(name, ":]]")
	if !ok {
		return "", fmt.Errorf("[[: was not matched with a closing :]]")
	}
	switch name {
	case "alnum", "alpha", "ascii", "blank", "cntrl", "digit", "graph",
		"lower", "print", "punct", "space", "upper", "word", "xdigit":
	default:
		return "", fmt.Errorf("invalid character class: %q", name)
	}
	return s[:len(name)+5], nil
}

// HasMeta returns whether a string contains any unescaped pattern
// metacharacters: '*', '?', or '['. When the function returns false, the given
// pattern can only match at most one string.
//
// For example, HasMeta(`foo\*bar`) returns false, but HasMeta(`foo*bar`)
// returns true.
//
// This can be useful to avoid extra work, like [Regexp]. Note that this
// function cannot be used to avoid [QuoteMeta], as backslashes are quoted by
// that function but ignored here.
//
// The [Mode] parameter is unused, and will be removed in v4.
func HasMeta(pat string, mode Mode) bool {
	for i := 0; i < len(pat); i++ {
		switch pat[i] {
		case '\\':
			i++
		case '*', '?', '[':
			return true
		}
	}
	return false
}

// QuoteMeta returns a string that quotes all pattern metacharacters in the
// given text. The returned string is a pattern that matches the literal text.
//
// For example, QuoteMeta(`foo*bar?`) returns `foo\*bar\?`.
//
// The [Mode] parameter is unused, and will be removed in v4.
func QuoteMeta(pat string, mode Mode) string {
	needsEscaping := false
loop:
	for _, r := range pat {
		switch r {
		case '*', '?', '[', '\\':
			needsEscaping = true
			break loop
		}
	}
	if !needsEscaping { // short-cut without a string copy
		return pat
	}
	var sb strings.Builder
	for _, r := range pat {
		switch r {
		case '*', '?', '[', '\\':
			sb.WriteByte('\\')
		}
		sb.WriteRune(r)
	}
	return sb.String()
}
