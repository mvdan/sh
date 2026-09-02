// Copyright (c) 2026, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package shell

import (
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

// Split splits s into words as a shell would, performing quote removal but
// no expansion. Unlike [Fields], parameter expansions, command substitutions,
// and arithmetic expansions are kept verbatim, and tildes and braces are
// left untouched.
//
// Note that the result is not necessarily the final list of arguments a shell
// would produce, as expanding an unquoted word like $x may then require
// further word splitting and globbing of the expanded value.
// To expand and split at the same time, use [Fields] or the [expand] package.
//
// An error will be reported if the input string had invalid syntax.
func Split(s string) ([]string, error) {
	p := syntax.NewParser()
	var words []string
	var sb strings.Builder
	for w, err := range p.WordsSeq(strings.NewReader(s)) {
		if err != nil {
			return nil, err
		}
		sb.Reset()
		unquoteParts(&sb, s, w.Parts, false)
		words = append(words, sb.String())
	}
	return words, nil
}

// unquoteParts writes the word parts to sb with quotes removed.
// Expansions are written verbatim from src, using their positions.
func unquoteParts(sb *strings.Builder, src string, parts []syntax.WordPart, dblQuoted bool) {
	for _, part := range parts {
		switch part := part.(type) {
		case *syntax.Lit:
			unescape(sb, part.Value, dblQuoted)
		case *syntax.SglQuoted:
			val := part.Value
			if part.Dollar {
				val, _, _ = expand.Format(nil, val, nil)
			}
			sb.WriteString(val)
		case *syntax.DblQuoted:
			unquoteParts(sb, src, part.Parts, true)
		default:
			sb.WriteString(src[part.Pos().Offset():part.End().Offset()])
		}
	}
}

// unescape writes s to sb, removing backslashes which quote the following
// character. Within double quotes, only a few characters can be quoted.
func unescape(sb *strings.Builder, s string, dblQuoted bool) {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b == '\\' && i+1 < len(s) {
			next := s[i+1]
			if !dblQuoted || strings.IndexByte("\"\\$`", next) >= 0 {
				i++
				b = next
			}
		}
		sb.WriteByte(b)
	}
}

// Join quotes each argument with [syntax.Quote] for Bash and joins them with
// spaces, so that [Split] or [Fields] on the result recover the original
// arguments. It is the inverse of [Split].
//
// An error will be reported if an argument cannot be quoted,
// such as one containing null bytes.
func Join(args ...string) (string, error) {
	var sb strings.Builder
	for i, arg := range args {
		if i > 0 {
			sb.WriteByte(' ')
		}
		quoted, err := syntax.Quote(arg, syntax.LangBash)
		if err != nil {
			return "", err
		}
		sb.WriteString(quoted)
	}
	return sb.String(), nil
}
