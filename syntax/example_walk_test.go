// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax_test

import (
	"os"
	"strings"

	"mvdan.cc/sh/syntax"
)

func ExampleWalk() {
	in := strings.NewReader(`echo $foo "and $bar"`)
	f, err := syntax.NewParser().Parse(in, "")
	if err != nil {
		return
	}
	syntax.Walk(f, func(node syntax.Node) bool {
		switch x := node.(type) {
		case *syntax.ParamExp:
			x.Param.Value = strings.ToUpper(x.Param.Value)
		}
		return true
	})
	syntax.NewPrinter().Print(os.Stdout, f)
	// Output: echo $FOO "and $BAR"
}

func ExampleDebugPrint() {
	in := strings.NewReader(`echo 'foo'`)
	f, err := syntax.NewParser().Parse(in, "")
	if err != nil {
		return
	}
	syntax.DebugPrint(os.Stdout, f)
	// Output:
	// *syntax.File {
	// .  Name: ""
	// .  StmtList: syntax.StmtList {
	// .  .  Stmts: []*syntax.Stmt (len = 1) {
	// .  .  .  0: *syntax.Stmt {
	// .  .  .  .  Comments: []syntax.Comment (len = 0) {}
	// .  .  .  .  Cmd: *syntax.CallExpr {
	// .  .  .  .  .  Assigns: []*syntax.Assign (len = 0) {}
	// .  .  .  .  .  Args: []*syntax.Word (len = 2) {
	// .  .  .  .  .  .  0: *syntax.Word {
	// .  .  .  .  .  .  .  Parts: []syntax.WordPart (len = 1) {
	// .  .  .  .  .  .  .  .  0: *syntax.Lit {
	// .  .  .  .  .  .  .  .  .  ValuePos: 1:1
	// .  .  .  .  .  .  .  .  .  ValueEnd: 1:5
	// .  .  .  .  .  .  .  .  .  Value: "echo"
	// .  .  .  .  .  .  .  .  }
	// .  .  .  .  .  .  .  }
	// .  .  .  .  .  .  }
	// .  .  .  .  .  .  1: *syntax.Word {
	// .  .  .  .  .  .  .  Parts: []syntax.WordPart (len = 1) {
	// .  .  .  .  .  .  .  .  0: *syntax.SglQuoted {
	// .  .  .  .  .  .  .  .  .  Left: 1:6
	// .  .  .  .  .  .  .  .  .  Right: 1:10
	// .  .  .  .  .  .  .  .  .  Dollar: false
	// .  .  .  .  .  .  .  .  .  Value: "foo"
	// .  .  .  .  .  .  .  .  }
	// .  .  .  .  .  .  .  }
	// .  .  .  .  .  .  }
	// .  .  .  .  .  }
	// .  .  .  .  }
	// .  .  .  .  Position: 1:1
	// .  .  .  .  Semicolon: 0:0
	// .  .  .  .  Negated: false
	// .  .  .  .  Background: false
	// .  .  .  .  Coprocess: false
	// .  .  .  .  Redirs: []*syntax.Redirect (len = 0) {}
	// .  .  .  }
	// .  .  }
	// .  .  Last: []syntax.Comment (len = 0) {}
	// .  }
	// }
}
