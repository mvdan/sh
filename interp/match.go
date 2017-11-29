// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"bytes"
	"regexp"
)

func match(pattern, name string) bool {
	expr := translatePattern(pattern)
	rx, err := regexp.Compile("^" + expr + "$")
	if err != nil {
		return false
	}
	return rx.MatchString(name)
}

func translatePattern(pattern string) string {
	// TODO: slashes need to be explicit
	// TODO: char ranges
	// TODO: char classes
	var buf bytes.Buffer
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; c {
		case '*':
			buf.WriteString(".*")
		case '?':
			buf.WriteString(".")
		case '\\':
			buf.WriteByte(c)
			i++
			buf.WriteByte(pattern[i])
		default:
			buf.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return buf.String()
}
