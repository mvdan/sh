// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"bytes"
	"regexp"
	"strings"
)

func match(pattern, name string) bool {
	expr := translatePattern(pattern, true)
	rx, err := regexp.Compile("^" + expr + "$")
	if err != nil {
		return false
	}
	return rx.MatchString(name)
}

func findAllIndex(pattern, name string, n int) [][]int {
	expr := translatePattern(pattern, true)
	rx, err := regexp.Compile(expr)
	if err != nil {
		return nil
	}
	return rx.FindAllStringIndex(name, n)
}

func charClass(s string) string {
	if !strings.HasPrefix(s, "[[:") {
		return ""
	}
	name := s[3:]
	end := strings.Index(name, ":]]")
	if end < 0 {
		return ""
	}
	name = name[:end]
	switch name {
	case "alnum", "alpha", "ascii", "blank", "cntrl", "digit", "graph",
		"lower", "print", "punct", "space", "upper", "word", "xdigit":
	default:
		return ""
	}
	return s[:len(name)+6]
}

func translatePattern(pattern string, greedy bool) string {
	var buf bytes.Buffer
loop:
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
			if s := charClass(pattern[i:]); s != "" {
				buf.WriteString(s)
				i += len(s) - 1
				break
			}
			buf.WriteByte(c)
			if i++; i >= len(pattern) {
				break loop
			}
			c = pattern[i]
			if c == '!' {
				c = '^'
			}
			buf.WriteByte(c)
			for {
				if i++; i >= len(pattern) {
					break loop
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
	return buf.String()
}
