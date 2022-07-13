// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	qt "github.com/frankban/quicktest"

	"mvdan.cc/sh/v3/syntax"
)

func TestRoundtripJSON(t *testing.T) {
	t.Parallel()

	// Read testdata files.
	inputShell, err := os.ReadFile("testdata/json.sh")
	qt.Assert(t, err, qt.IsNil)
	inputJSON, err := os.ReadFile("testdata/json.json")
	if !*update { // allow it to not exist
		qt.Assert(t, err, qt.IsNil)
	}
	sb := new(strings.Builder)

	// Parse the shell source and check that it is well formatted.
	parser := syntax.NewParser(syntax.KeepComments(true))
	node, err := parser.Parse(bytes.NewReader(inputShell), "")
	qt.Assert(t, err, qt.IsNil)

	printer := syntax.NewPrinter()
	sb.Reset()
	err = printer.Print(sb, node)
	qt.Assert(t, err, qt.IsNil)
	qt.Assert(t, sb.String(), qt.Equals, string(inputShell))

	// Validate writing the pretty JSON.
	sb.Reset()
	err = writeJSON(sb, node, true)
	qt.Assert(t, err, qt.IsNil)
	got := sb.String()
	if *update {
		err := os.WriteFile("testdata/json.json", []byte(got), 0o666)
		qt.Assert(t, err, qt.IsNil)
	} else {
		qt.Assert(t, got, qt.Equals, string(inputJSON))
	}

	// Ensure we don't use the originally parsed node again.
	node = nil

	// Validate reading the pretty JSON and check that it formats the same.
	node2, err := readJSON(bytes.NewReader(inputJSON))
	qt.Assert(t, err, qt.IsNil)

	sb.Reset()
	err = printer.Print(sb, node2)
	qt.Assert(t, err, qt.IsNil)
	qt.Assert(t, sb.String(), qt.Equals, string(inputShell))

	// Validate that emitting the JSON again produces the same result.
	sb.Reset()
	err = writeJSON(sb, node2, true)
	qt.Assert(t, err, qt.IsNil)
	got = sb.String()
	qt.Assert(t, got, qt.Equals, string(inputJSON))
}
