// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mvdan/sh/syntax"
)

func TestWriteJSON(t *testing.T) {
	in := `cmd arg1 "arg2"`
	want := `{"Comments":[],"Name":"","Stmts":[{"Assigns":[],"Background":false,"Cmd":{"Args":[{"Parts":[{"Type":"Lit","Value":"cmd"}]},{"Parts":[{"Type":"Lit","Value":"arg1"}]},{"Parts":[{"Dollar":false,"Parts":[{"Type":"Lit","Value":"arg2"}],"Type":"DblQuoted"}]}],"Type":"CallExpr"},"Coprocess":false,"Negated":false,"Redirs":[]}]}`
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
