// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/kr/pretty"
)

func TestParseAST(t *testing.T) {
	defaultPos = Pos{}
	for i, c := range astTests {
		want := fullProg(c.ast)
		setPosRecurse(t, want.Stmts, defaultPos, false)
		for j, in := range c.strs {
			t.Run(fmt.Sprintf("%d-%d", i, j), singleParseAST(in, want))
		}
	}
}

func singleParseAST(in string, want File) func(t *testing.T) {
	return func(t *testing.T) {
		r := strings.NewReader(in)
		got, err := Parse(r, "")
		if err != nil {
			t.Fatalf("Unexpected error in %q: %v", in, err)
		}
		setPosRecurse(t, got.Stmts, defaultPos, true)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("AST mismatch in %q\ndiff:\n%s", in,
				strings.Join(pretty.Diff(want, got), "\n"),
			)
		}
	}
}
