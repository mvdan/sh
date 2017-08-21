// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main

import (
	"bytes"
	"strings"
	"testing"

	"mvdan.cc/sh/syntax"
)

func TestWriteJSON(t *testing.T) {
	in := `cmd arg1 "arg2"`
	want := `{"StmtList":{"Stmts":[{"Cmd":{"Args":[{"Parts":[{"Type":"Lit","Value":"cmd"}]},{"Parts":[{"Type":"Lit","Value":"arg1"}]},{"Parts":[{"Parts":[{"Type":"Lit","Value":"arg2"}],"Type":"DblQuoted"}]}],"Type":"CallExpr"}}]}}`
	parser := syntax.NewParser(syntax.KeepComments)
	prog, err := parser.Parse(strings.NewReader(in), "")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	writeJSON(&buf, prog, false)
	got := buf.String()
	if got != want+"\n" {
		t.Fatalf("wrong output for %q\nwant: %s\ngot:  %s", in, want, got)
	}
}
