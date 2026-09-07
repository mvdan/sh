// Copyright (c) 2026, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"strings"
	"testing"

	"github.com/go-quicktest/qt"
)

func TestZshSubscripts(t *testing.T) {
	t.Parallel()
	node, err := NewParser(Variant(LangZsh)).Parse(strings.NewReader("h[foo-bar]=1"), "")
	qt.Assert(t, qt.IsNil(err))
	out, err := strPrint(NewPrinter(), node)
	// This records the current corruption of a valid associative key.
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(out, "h[foo - bar]=1\n"))
}
