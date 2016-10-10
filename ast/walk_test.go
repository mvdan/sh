// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package ast_test

import (
	"fmt"
	"testing"

	"github.com/mvdan/sh/ast"
	"github.com/mvdan/sh/internal/tests"
)

func TestWalk(t *testing.T) {
	for i, c := range tests.FileTests {
		for j, prog := range c.All {
			t.Run(fmt.Sprintf("%03d-%d", i, j), func(t *testing.T) {
				ast.Walk(nopVisitor{}, prog)
			})
		}
	}
}

type nopVisitor struct{}

func (v nopVisitor) Visit(node ast.Node) ast.Visitor {
	return v
}
