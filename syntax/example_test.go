// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax_test

import (
	"os"
	"strings"

	"mvdan.cc/sh/syntax"
)

func Example() {
	in := strings.NewReader("{ foo; bar; }")
	f, err := syntax.NewParser().Parse(in, "")
	if err != nil {
		return
	}
	syntax.NewPrinter().Print(os.Stdout, f)
	// Output:
	// {
	//	foo
	//	bar
	// }
}
