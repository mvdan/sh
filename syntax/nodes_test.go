// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mvdan/sh/internal"
	"github.com/mvdan/sh/internal/tests"
	"github.com/mvdan/sh/syntax"
)

func TestNodePos(t *testing.T) {
	internal.DefaultPos = 1234
	for i, c := range tests.FileTests {
		for j, prog := range c.All {
			t.Run(fmt.Sprintf("%03d-%d", i, j), func(t *testing.T) {
				tests.SetPosRecurse(t, "", prog, internal.DefaultPos, true)
			})
		}
	}
}

func TestPosition(t *testing.T) {
	internal.DefaultPos = 0
	for i, c := range tests.FileTests {
		for j, in := range c.Strs {
			t.Run(fmt.Sprintf("%03d-%d", i, j), func(t *testing.T) {
				prog, err := parser.Parse([]byte(in), "", 0)
				if err != nil {
					t.Fatal(err)
				}
				v := &posVisitor{
					t:     t,
					f:     prog,
					lines: strings.Split(in, "\n"),
				}
				syntax.Walk(v, prog)
			})
		}
	}
}

type posVisitor struct {
	t     *testing.T
	f     *syntax.File
	lines []string
}

func (v *posVisitor) Visit(n syntax.Node) syntax.Visitor {
	if n == nil {
		return v
	}
	pos := v.f.Position(n.Pos())
	offs := 0
	for l := 0; l < pos.Line-1; l++ {
		// since lines here are missing the trailing newline
		offs += len(v.lines[l]) + 1
	}
	// column is 1-indexed, offset is 0-indexed
	offs += pos.Column - 1
	if offs != pos.Offset {
		v.t.Fatalf("Inconsistent Position: line %d, col %d; wanted offset %d, got %d ",
			pos.Line, pos.Column, pos.Offset, offs)
	}
	return v
}
