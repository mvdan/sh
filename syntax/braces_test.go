// Copyright (c) 2026, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"

	"mvdan.cc/sh/v3/syntax"
)

func TestSplitBracesDeep(t *testing.T) {
	t.Parallel()

	// Nesting deeper than the parser's maxNesting is left literal so that a
	// long word cannot overflow the stack, plus one level for expand's limit.
	const wantDepth = 10_001
	depth := wantDepth * 2
	src := strings.Repeat("{a,", depth) + "a" + strings.Repeat("}", depth)
	f, err := syntax.NewParser().Parse(strings.NewReader(src), "")
	qt.Assert(t, qt.IsNil(err))
	word := f.Stmts[0].Cmd.(*syntax.CallExpr).Args[0]
	qt.Assert(t, qt.IsTrue(syntax.SplitBraces(word)))

	got := 0
	for parts := word.Parts; ; got++ {
		qt.Assert(t, qt.HasLen(parts, 1))
		br, ok := parts[0].(*syntax.BraceExp)
		if !ok {
			lit := parts[0].(*syntax.Lit)
			qt.Assert(t, qt.IsTrue(strings.HasPrefix(lit.Value, "{a,")))
			break
		}
		qt.Assert(t, qt.HasLen(br.Elems, 2))
		parts = br.Elems[1].Parts
	}
	qt.Assert(t, qt.Equals(got, wantDepth))
}

func TestSplitBracesLong(t *testing.T) {
	t.Parallel()

	// Many opening braces share the last closing brace as a sequence
	// candidate, and each fails the loose check without rescanning the rest
	// of the word. Both words are literal text, as in Bash.
	for _, src := range []string{
		strings.Repeat("{a}", 300_000) + "..b}",
		strings.Repeat("{a}", 300_000) + "\\...b}",
	} {
		word := &syntax.Word{Parts: []syntax.WordPart{&syntax.Lit{Value: src}}}
		qt.Assert(t, qt.IsFalse(syntax.SplitBraces(word)))
	}
}

func TestSplitBracesPos(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
	}{
		{"foo{bar,baz}qux", `"foo"@1:1-1:16 {@1:?-1:17 ["bar"@1:1-1:16] ["baz"@1:1-1:16]} "qux"@1:1-1:16`},
		{"{,a}", `{@1:1-1:5 [] ["a"@1:3-1:4]}`},
		{"{a,}", `{@1:1-1:5 ["a"@1:2-1:3] []}`},
		{"a{b,$x}c", `"a"@1:1-1:5 {@1:?-1:8 ["b"@1:1-1:5] [*syntax.ParamExp@1:5-1:7]} "c"@1:7-1:9`},
		{"{1..3}", `{@1:?-1:8 ["1"@1:1-1:7] ["3"@1:1-1:7]}`},
		{"{a,{b,c}}", `{@1:?-1:12 ["a"@1:1-1:10] [{@1:?-1:11 ["b"@1:1-1:10] ["c"@1:1-1:10]}]}`},
		{"x{a,b}{c,d}", `"x"@1:1-1:12 {@1:?-1:13 ["a"@1:1-1:12] ["b"@1:1-1:12]} {@1:?-1:13 ["c"@1:1-1:12] ["d"@1:1-1:12]}`},
		{"'{'{a,b}", `*syntax.SglQuoted@1:1-1:4 {@1:3-1:10 ["a"@1:4-1:9] ["b"@1:4-1:9]}`},
		// Positions after an escaped newline are approximate, as a literal's
		// value drops it, but the word still ends where the literal does.
		{"a\\\n{b,c}", `"a"@1:1-2:6 {@1:?-2:7 ["b"@1:1-2:6] ["c"@1:1-2:6]}`},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			f, err := syntax.NewParser().Parse(strings.NewReader(tc.in), "")
			qt.Assert(t, qt.IsNil(err))
			word := f.Stmts[0].Cmd.(*syntax.CallExpr).Args[0]
			qt.Assert(t, qt.IsTrue(syntax.SplitBraces(word)))
			if tc.in == "{,a}" || tc.in == "{a,}" {
				// BraceExp derives its positions from its elements,
				// panicking when the first or last element is empty,
				// so the positions below are not reached.
				defer func() { qt.Assert(t, qt.Not(qt.IsNil(recover()))) }()
			}
			qt.Assert(t, qt.Equals(dumpParts(word.Parts), tc.want))
		})
	}
}

// dumpParts describes each part by its value or type and its Pos and End,
// with each element of a brace expansion in brackets.
func dumpParts(parts []syntax.WordPart) string {
	var sb strings.Builder
	for i, part := range parts {
		if i > 0 {
			sb.WriteByte(' ')
		}
		switch part := part.(type) {
		case *syntax.Lit:
			fmt.Fprintf(&sb, "%q@%s-%s", part.Value, part.Pos(), part.End())
		case *syntax.BraceExp:
			fmt.Fprintf(&sb, "{@%s-%s", part.Pos(), part.End())
			for _, elem := range part.Elems {
				fmt.Fprintf(&sb, " [%s]", dumpParts(elem.Parts))
			}
			sb.WriteByte('}')
		default:
			fmt.Fprintf(&sb, "%T@%s-%s", part, part.Pos(), part.End())
		}
	}
	return sb.String()
}
