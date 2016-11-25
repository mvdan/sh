// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"fmt"
	"strings"
	"testing"
)

func TestPosition(t *testing.T) {
	t.Parallel()
	for i, c := range fileTests {
		for j, in := range c.Strs {
			t.Run(fmt.Sprintf("%03d-%d", i, j), func(t *testing.T) {
				prog, err := Parse(strings.NewReader(in), "", 0)
				if err != nil {
					t.Fatal(err)
				}
				v := &posVisitor{
					t:     t,
					f:     prog,
					lines: strings.Split(in, "\n"),
				}
				Walk(v, prog)
			})
		}
	}
}

type posVisitor struct {
	t     *testing.T
	f     *File
	lines []string
}

func (v *posVisitor) Visit(n Node) Visitor {
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

func TestWeirdOperatorString(t *testing.T) {
	op := RedirOperator(1000)
	want := "token(1000)"
	if got := op.String(); got != want {
		t.Fatalf("token.String() mismatch: want %s, got %s", want, got)
	}
}
