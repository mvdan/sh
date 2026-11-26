// Copyright (c) 2026, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"strings"
	"testing"

	"github.com/go-quicktest/qt"
)

// Bash allows bracket glob characters in function names declared with the
// "function" keyword. The lexer reports foo[abc] as two literals, so the
// parser needs to join them into a single name.
func TestBashFuncNameWithBrackets(t *testing.T) {
	t.Parallel()

	const in = "function foo[abc] { echo hi; }\n"
	f, err := NewParser(Variant(LangBash)).Parse(strings.NewReader(in), "")
	qt.Assert(t, qt.IsNil(err))

	fun, ok := f.Stmts[0].Cmd.(*FuncDecl)
	qt.Assert(t, qt.IsTrue(ok))
	qt.Assert(t, qt.Equals(fun.Name.Value, "foo[abc]"))

	got, err := strPrint(NewPrinter(), f)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(got, in))
}
