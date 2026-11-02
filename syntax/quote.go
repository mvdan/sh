// Copyright (c) 2021, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type QuoteError struct {
	ByteOffset int
	Message    string
}

func (e QuoteError) Error() string {
	return fmt.Sprintf("cannot quote character at byte %d: %s", e.ByteOffset, e.Message)
}

const (
	quoteErrNull  = "shell strings cannot contain null bytes"
	quoteErrPOSIX = "POSIX shell lacks escape sequences"
	quoteErrRange = "rune out of range"
	quoteErrMksh  = "mksh cannot escape codepoints above 16 bits"
)

// Quote returns s quoted as a single word which expands to s
// in the given language variant, such as when building a script
// from arbitrary strings. The result is also valid in an array subscript
// like name[word], or in an unquoted parameter expansion like ${name:-word}.
//
// The shortest quoting is chosen on a best-effort basis.
// Strings which need no quoting are returned unchanged,
// and may also be surrounded by single quotes.
//
// A [*QuoteError] is returned when s cannot be quoted in lang,
// such as null bytes in any variant, or non-printable characters in POSIX.
//
// To quote a list of arguments into a command line, see [mvdan.cc/sh/v3/shell.Join].
func Quote(s string, lang LangVariant) (string, error) {
	if s == "" {
		// Special case; an empty string must always be quoted,
		// as otherwise it expands to zero fields.
		return "''", nil
	}
	shellChars := false
	nonPrintable := false
	offs := 0
	for rem := s; len(rem) > 0; {
		r, size := utf8.DecodeRuneInString(rem)
		switch r {
		// Like regOps; token characters.
		case ';', '"', '\'', '(', ')', '$', '|', '&', '>', '<', '`',
			// Whitespace; might result in multiple fields.
			' ', '\t', '\r', '\n',
			// Escape sequences would be expanded.
			'\\',
			// Would start a comment unless quoted.
			'#',
			// Might result in brace expansion, or end a parameter expansion.
			'{', '}',
			// Might result in tilde expansion.
			'~',
			// Might result in globbing, or end an array subscript.
			'*', '?', '[', ']',
			// Might result in an assignment.
			'=':
			shellChars = true
		case '\x00':
			return "", &QuoteError{ByteOffset: offs, Message: quoteErrNull}
		}
		if r == utf8.RuneError || !unicode.IsPrint(r) {
			if lang.in(LangPOSIX) {
				return "", &QuoteError{ByteOffset: offs, Message: quoteErrPOSIX}
			}
			nonPrintable = true
		}
		rem = rem[size:]
		offs += size
	}
	if !shellChars && !nonPrintable && !IsKeyword(s) {
		// Nothing to quote; avoid allocating.
		return s, nil
	}

	// Single quotes are usually best,
	// as they don't require any escaping of characters.
	// If we have any invalid utf8 or non-printable runes,
	// use $'' so that we can escape them.
	// Note that we can't use double quotes for those.
	var b strings.Builder
	if nonPrintable {
		b.WriteString("$'")
		lastRequoteIfHex := false
		offs := 0
		for rem := s; len(rem) > 0; {
			nextRequoteIfHex := false
			r, size := utf8.DecodeRuneInString(rem)
			switch {
			case r == '\'', r == '\\':
				b.WriteByte('\\')
				b.WriteRune(r)
			case unicode.IsPrint(r) && r != utf8.RuneError:
				if lastRequoteIfHex && isHex(r) {
					b.WriteString("'$'")
				}
				b.WriteRune(r)
			case r == '\a':
				b.WriteString(`\a`)
			case r == '\b':
				b.WriteString(`\b`)
			case r == '\f':
				b.WriteString(`\f`)
			case r == '\n':
				b.WriteString(`\n`)
			case r == '\r':
				b.WriteString(`\r`)
			case r == '\t':
				b.WriteString(`\t`)
			case r == '\v':
				b.WriteString(`\v`)
			case r < utf8.RuneSelf, r == utf8.RuneError && size == 1:
				// \xXX, fixed at two hexadecimal characters.
				fmt.Fprintf(&b, "\\x%02x", rem[0])
				// Unfortunately, mksh allows \x to consume more hex characters.
				// Ensure that we don't allow it to read more than two.
				if lang.in(LangMirBSDKorn) {
					nextRequoteIfHex = true
				}
			case r > utf8.MaxRune:
				// Not a valid Unicode code point?
				return "", &QuoteError{ByteOffset: offs, Message: quoteErrRange}
			case lang.in(LangMirBSDKorn) && r > 0xFFFD:
				// From the CAVEATS section in R59's man page:
				//
				// mksh currently uses OPTU-16 internally, which is the same as
				// UTF-8 and CESU-8 with 0000..FFFD being valid codepoints.
				return "", &QuoteError{ByteOffset: offs, Message: quoteErrMksh}
			case r < 0x10000:
				// \uXXXX, fixed at four hexadecimal characters.
				fmt.Fprintf(&b, "\\u%04x", r)
			default:
				// \UXXXXXXXX, fixed at eight hexadecimal characters.
				fmt.Fprintf(&b, "\\U%08x", r)
			}
			rem = rem[size:]
			lastRequoteIfHex = nextRequoteIfHex
			offs += size
		}
		b.WriteString("'")
		return b.String(), nil
	}

	// Single quotes without any need for escaping.
	if !strings.Contains(s, "'") {
		return "'" + s + "'", nil
	}

	// The string contains single quotes,
	// so fall back to double quotes.
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\', '`', '$':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String(), nil
}

func isHex(r rune) bool {
	return (r >= '0' && r <= '9') ||
		(r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}
