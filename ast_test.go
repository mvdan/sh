// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"fmt"
	"testing"

	"github.com/mvdan/sh/ast"
	"github.com/mvdan/sh/internal"
	"github.com/mvdan/sh/internal/tests"
)

func TestNodePos(t *testing.T) {
	internal.DefaultPos = 1234
	for i, c := range tests.FileTests {
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			want := c.Ast.(*ast.File)
			tests.SetPosRecurse(t, "", want, internal.DefaultPos, true)
		})
	}
}
