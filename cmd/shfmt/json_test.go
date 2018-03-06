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
	{"", `{"End":{"Col":0,"Line":0,"Offset":0},"Pos":{"Col":0,"Line":0,"Offset":0}}`},
	{"foo", `{"End":{"Col":4,"Line":1,"Offset":3},"Pos":{"Col":1,"Line":1,"Offset":0},"Stmts":[{"Cmd":{"Args":[{"End":{"Col":4,"Line":1,"Offset":3},"Parts":[{"End":{"Col":4,"Line":1,"Offset":3},"Pos":{"Col":1,"Line":1,"Offset":0},"Type":"Lit","Value":"foo"}],"Pos":{"Col":1,"Line":1,"Offset":0}}],"End":{"Col":4,"Line":1,"Offset":3},"Pos":{"Col":1,"Line":1,"Offset":0},"Type":"CallExpr"},"End":{"Col":4,"Line":1,"Offset":3},"Pos":{"Col":1,"Line":1,"Offset":0}}]}`},
	{"((2))", `{"End":{"Col":6,"Line":1,"Offset":5},"Pos":{"Col":1,"Line":1,"Offset":0},"Stmts":[{"Cmd":{"End":{"Col":6,"Line":1,"Offset":5},"Pos":{"Col":1,"Line":1,"Offset":0},"Type":"ArithmCmd","X":{"End":{"Col":4,"Line":1,"Offset":3},"Parts":[{"End":{"Col":4,"Line":1,"Offset":3},"Pos":{"Col":3,"Line":1,"Offset":2},"Type":"Lit","Value":"2"}],"Pos":{"Col":3,"Line":1,"Offset":2},"Type":"Word"}},"End":{"Col":6,"Line":1,"Offset":5},"Pos":{"Col":1,"Line":1,"Offset":0}}]}`},
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
