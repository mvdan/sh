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
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			ast.Walk(visitor{}, c.Ast.(*ast.File))
		})
	}
}

type visitor struct{}

func (v visitor) Visit(node ast.Node) ast.Visitor {
	return v
}
