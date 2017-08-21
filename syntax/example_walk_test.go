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
