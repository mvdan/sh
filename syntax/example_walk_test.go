// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax_test

import (
	"os"
	"strings"

	"github.com/mvdan/sh/syntax"
)

type paramUpper struct{}

func (u paramUpper) Visit(node syntax.Node) syntax.Visitor {
	switch x := node.(type) {
	case *syntax.ParamExp:
		x.Param.Value = strings.ToUpper(x.Param.Value)
	}
	return u
}

func ExampleWalk() {
	in := `echo $foo "and $bar"`
	f, err := syntax.Parse(strings.NewReader(in), "", 0)
	if err != nil {
		return
	}
	syntax.Walk(paramUpper{}, f)
	syntax.Fprint(os.Stdout, f)
	// Output: echo $FOO "and $BAR"
}
