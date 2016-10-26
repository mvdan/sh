// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package ast_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/mvdan/sh/ast"
	"github.com/mvdan/sh/internal"
	"github.com/mvdan/sh/internal/tests"
	"github.com/mvdan/sh/parser"

	"github.com/kr/pretty"
)

func TestSimplify(t *testing.T) {
	for i, c := range tests.FileTests {
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			checkSimplify(t, c.Strs[0], c.Simple)
		})
	}
}

func checkSimplify(t *testing.T, inStr, wantStr string) {
	internal.DefaultPos = 0
	got, err := parser.Parse([]byte(inStr), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	tests.SetPosRecurse(t, "", got, internal.DefaultPos, false)
	want, err := parser.Parse([]byte(wantStr), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	tests.SetPosRecurse(t, "", want, internal.DefaultPos, false)
	ast.Simplify(got)
	// don't run on the whole *File as lines offets differ
	if !reflect.DeepEqual(got.Stmts, want.Stmts) {
		t.Fatalf("AST mismatch in %q\ndiff:\n%s", inStr,
			strings.Join(pretty.Diff(want, got), "\n"))
	}
}
