// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package shell_test

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"

	"mvdan.cc/sh/shell"
)

func ExampleExpand() {
	env := func(name string) string {
		switch name {
		case "HOME":
			return "/home/user"
		}
		return "" // leave the rest unset
	}
	out, _ := shell.Expand("No place like $HOME", env)
	fmt.Println(out)

	out, _ = shell.Expand("Some vars are ${missing:-awesome}", env)
	fmt.Println(out)

	out, _ = shell.Expand("Math is fun! $((12 * 34))", nil)
	fmt.Println(out)
	// Output:
	// No place like /home/user
	// Some vars are awesome
	// Math is fun! 408
}

func ExampleFields() {
	env := func(name string) string {
		switch name {
		case "foo":
			return "bar baz"
		}
		return "" // leave the rest unset
	}
	out, _ := shell.Fields(`"many quoted" ' strings '`, env)
	fmt.Printf("%#v\n", out)

	out, _ = shell.Fields("unquoted $foo", env)
	fmt.Printf("%#v\n", out)

	out, _ = shell.Fields(`quoted "$foo"`, env)
	fmt.Printf("%#v\n", out)
	// Output:
	// []string{"many quoted", " strings "}
	// []string{"unquoted", "bar", "baz"}
	// []string{"quoted", "bar baz"}
}

func ExampleSourceFile() {
	src := `
		foo=abc
		foo+=012
		if true; then
			bar=$(echo example_*.go)
		fi
	`
	ioutil.WriteFile("f.sh", []byte(src), 0666)
	defer os.Remove("f.sh")
	vars, err := shell.SourceFile(context.TODO(), "f.sh")
	if err != nil {
		return
	}
	fmt.Println(len(vars))
	fmt.Println("foo", vars["foo"])
	fmt.Println("bar", vars["bar"])
	// Output:
	// 2
	// foo abc012
	// bar example_test.go
}
