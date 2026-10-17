// Copyright (c) 2026, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax_test

import (
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
