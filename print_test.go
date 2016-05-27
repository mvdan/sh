// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"bytes"
	"fmt"
	"testing"
)

func TestFprint(t *testing.T) {
	for i, c := range astTests {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			in := fullProg(c.ast)
			want := c.strs[0]
			var buf bytes.Buffer
			Fprint(&buf, in)
			got := buf.String()
			if got != want {
				t.Fatalf("AST print mismatch\nwant: %s\ngot:  %s",
					want, got)
			}
		})
	}
}
