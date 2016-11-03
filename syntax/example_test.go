// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax_test

import (
	"os"

	"github.com/mvdan/sh/syntax"
)

func ExampleFormat() {
	in := "{ foo;bar; }"
	f, err := syntax.Parse([]byte(in), "", 0)
	if err != nil {
		return
	}
	syntax.Fprint(os.Stdout, f)
	// Output:
	// {
	//	foo
	//	bar
	// }
}
