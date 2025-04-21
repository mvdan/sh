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
	"regexp"
	"strings"
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

var numRange = regexp.MustCompile(`^([+-]?\d+)\.\.([+-]?\d+)}`)

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
	if !needsEscaping && mode&EntireString == 0 { // short-cut without a string copy
		return pat, nil
	}
	var sb strings.Builder
	// Enable matching `\n` with the `.` metacharacter as globs match `\n`
	sb.WriteString("(?s)")
	dotMeta := false
	if mode&NoGlobCase != 0 {
		sb.WriteString("(?i)")
	}
	if mode&EntireString != 0 {
		sb.WriteString("^")
	}
writeLoop:
	for i := 0; i < len(pat); i++ {
		switch c := pat[i]; c {
		case '*':
			if mode&Filenames != 0 {
				// "**" only acts as globstar if it is alone as a path element.
				singleBefore := i == 0 || pat[i-1] == '/'
				if i++; i < len(pat) && pat[i] == '*' {
					singleAfter := i == len(pat)-1 || pat[i+1] == '/'
					if mode&NoGlobStar != 0 || !singleBefore || !singleAfter {
						// foo**, **bar, or NoGlobStar - behaves like "*"
						sb.WriteString("([^/.][^/]*)?")
					} else if i++; i < len(pat) && pat[i] == '/' {
						// **/ - requires a trailing slash when matching
						sb.WriteString("(.*/)?")
						dotMeta = true
					} else {
						// ** - the base logic matching zero or any path elements
						sb.WriteString(".*")
						dotMeta = true
						i--
					}
				} else {
					// * - matches anything except slashes and leading dots
					sb.WriteString("([^/.][^/]*)?")
					i--
				}
			} else {
				// * - matches anything when not in filename mode
				sb.WriteString(".*")
				dotMeta = true
			}
			if mode&Shortest != 0 {
				sb.WriteByte('?')
			}
		case '?':
			if mode&Filenames != 0 {
				sb.WriteString("[^/]")
			} else {
				sb.WriteByte('.')
				dotMeta = true
			}
		case '\\':
			if i++; i >= len(pat) {
				return "", &SyntaxError{msg: `\ at end of pattern`}
			}
			sb.WriteString(regexp.QuoteMeta(string(pat[i])))
		case '[':
			name, err := charClass(pat[i:])
			if err != nil {
				return "", &SyntaxError{msg: "charClass invalid", err: err}
			}
			if name != "" {
				sb.WriteString(name)
				i += len(name) - 1
				break
			}
			if mode&Filenames != 0 {
				for _, c := range pat[i:] {
					if c == ']' {
						break
					} else if c == '/' {
						sb.WriteString("\\[")
						continue writeLoop
					}
				}
			}
			sb.WriteByte(c)
			if i++; i >= len(pat) {
				return "", &SyntaxError{msg: "[ was not matched with a closing ]"}
			}
			switch c = pat[i]; c {
			case '!', '^':
				sb.WriteByte('^')
				if i++; i >= len(pat) {
					return "", &SyntaxError{msg: "[ was not matched with a closing ]"}
				}
			}
			if c = pat[i]; c == ']' {
				sb.WriteByte(']')
				if i++; i >= len(pat) {
					return "", &SyntaxError{msg: "[ was not matched with a closing ]"}
				}
			}
			rangeStart := byte(0)
		loopBracket:
			for ; i < len(pat); i++ {
				c = pat[i]
				sb.WriteByte(c)
				switch c {
				case '\\':
					if i++; i < len(pat) {
						sb.WriteByte(pat[i])
					}
					continue
				case ']':
					break loopBracket
				}
				if rangeStart != 0 && rangeStart > c {
					return "", &SyntaxError{msg: fmt.Sprintf("invalid range: %c-%c", rangeStart, c)}
				}
				if c == '-' {
					rangeStart = pat[i-1]
				} else {
					rangeStart = 0
				}
			}
			if i >= len(pat) {
				return "", &SyntaxError{msg: "[ was not matched with a closing ]"}
			}
		default:
			if c > 128 {
				sb.WriteByte(c)
			} else {
				sb.WriteString(regexp.QuoteMeta(string(c)))
			}
		}
	}
	if mode&EntireString != 0 {
		sb.WriteString("$")
	}
	// No `.` metacharacters were used, so don't return the (?s) flag.
	if !dotMeta {
		return sb.String()[4:], nil
	}
	return sb.String(), nil
}

func charClass(s string) (string, error) {
	if strings.HasPrefix(s, "[[.") || strings.HasPrefix(s, "[[=") {
		return "", fmt.Errorf("collating features not available")
	}
	name, ok := strings.CutPrefix(s, "[[:")
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
	return s[:len(name)+6], nil
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
