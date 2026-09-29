// Copyright (c) 2026, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package internal

import (
	"strings"
	"unicode/utf8"
)

// CutEscape slices an unquoted shell literal like `a\*b` around its first
// backslash escape, returning the text before it, the escaped character,
// and the text after it. ok is false if there is no such escape; a trailing
// backslash escapes nothing and is kept as-is.
func CutEscape(s string) (before, escaped, after string, ok bool) {
	i := strings.IndexByte(s, '\\')
	if i < 0 || i == len(s)-1 {
		return s, "", "", false
	}
	_, size := utf8.DecodeRuneInString(s[i+1:])
	return s[:i], s[i+1 : i+1+size], s[i+1+size:], true
}
