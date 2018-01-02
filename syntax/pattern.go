// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

func charClass(s string) (string, error) {
	if !strings.HasPrefix(s, "[[:") {
		return "", nil
	}
	name := s[3:]
	end := strings.Index(name, ":]]")
	if end < 0 {
		return "", fmt.Errorf("[[: was not matched with a closing :]]")
	}
	name = name[:end]
	switch name {
	case "alnum", "alpha", "ascii", "blank", "cntrl", "digit", "graph",
		"lower", "print", "punct", "space", "upper", "word", "xdigit":
	default:
		return "", fmt.Errorf("invalid character class: %q", name)
	}
	return s[:len(name)+6], nil
}

// PatternRune returns whether a rune has special meaning in a pattern
// expression. The ones that do are '*', '?', '[' and '\\'.
func PatternRune(r rune) bool {
	return r == '*' || r == '?' || r == '[' || r == '\\'
}

func anyPatternRune(s string) bool {
	for _, r := range s {
		if PatternRune(r) {
			return true
		}
	}
	return false
}

// TranslatePattern turns a shell pattern expression into a regular
// expression that can be used with regexp.Compile. It will return an
// error if the input pattern was incorrect. Otherwise, the returned
// expression is ensured to be valid syntax.
func TranslatePattern(pattern string, greedy bool) (string, error) {
	if !anyPatternRune(pattern) { // short-cut without a string copy
		return pattern, nil
	}
	var buf bytes.Buffer
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; c {
		case '*':
			buf.WriteString(".*")
			if !greedy {
				buf.WriteByte('?')
			}
		case '?':
			buf.WriteString(".")
		case '\\':
			buf.WriteByte(c)
			i++
			buf.WriteByte(pattern[i])
		case '[':
			name, err := charClass(pattern[i:])
			if err != nil {
				return "", err
			}
			if name != "" {
				buf.WriteString(name)
				i += len(name) - 1
				break
			}
			buf.WriteByte(c)
			if i++; i >= len(pattern) {
				return "", fmt.Errorf("[ was not matched with a closing ]")
			}
			c = pattern[i]
			if c == '!' {
				c = '^'
			}
			buf.WriteByte(c)
			for {
				if i++; i >= len(pattern) {
					return "", fmt.Errorf("[ was not matched with a closing ]")
				}
				c = pattern[i]
				buf.WriteByte(c)
				if c == ']' {
					break
				}
			}
		default:
			buf.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return buf.String(), nil
}
