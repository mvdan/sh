// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax_test

import (
	"os"
	"strings"

	"github.com/mvdan/sh/syntax"
)

func Example() {
	in := strings.NewReader("{ foo; bar; }")
	f, err := syntax.NewParser().Parse(in, "")
	if err != nil {
		return
	}
	syntax.NewPrinter(syntax.PrintConfig{}).Print(os.Stdout, f)
	// Output:
	// {
	//	foo
	//	bar
	// }
}
