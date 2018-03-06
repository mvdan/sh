// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"mvdan.cc/sh/syntax"
)

var jsonTests = []struct {
	in   string
	want string
}{
	{"", `{}`},
	{"foo", `{"Stmts":[{"Cmd":{"Args":[{"Parts":[{"Type":"Lit","Value":"foo"}]}],"Type":"CallExpr"}}]}`},
	{"((2))", `{"Stmts":[{"Cmd":{"Type":"ArithmCmd","X":{"Parts":[{"Type":"Lit","Value":"2"}],"Type":"Word"}}}]}`},
}

func TestWriteJSON(t *testing.T) {
	t.Parallel()
	parser := syntax.NewParser(syntax.KeepComments)

	for i, tc := range jsonTests {
		t.Run(fmt.Sprintf("%02d", i), func(t *testing.T) {
			prog, err := parser.Parse(strings.NewReader(tc.in), "")
			if err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			writeJSON(&buf, prog, false)
			got := buf.String()
			if got != tc.want+"\n" {
				t.Fatalf("mismatch on %q\nwant:\n%s\ngot:\n%s",
					tc.in, tc.want, got)
			}
		})
	}
}
