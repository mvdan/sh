// Copyright (c) 2026, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"strings"
	"testing"

	"github.com/go-quicktest/qt"
)

func TestZshAlways(t *testing.T) {
	t.Parallel()
	// This records the current rejection of valid Zsh syntax.
	_, err := NewParser(Variant(LangZsh)).Parse(strings.NewReader("{ :; } always { :; }"), "")
	qt.Assert(t, qt.IsNotNil(err))
}
