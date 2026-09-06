// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package shell_test

import (
	"fmt"
	"testing/fstest"

	"mvdan.cc/sh/v3/shell"
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

func ExampleSplit() {
	out, _ := shell.Split(`cp "my file.txt" $HOME/backup`)
	fmt.Printf("%#v\n", out)

	out, _ = shell.Split(`echo 'single quotes' escaped\ space`)
	fmt.Printf("%#v\n", out)
	// Output:
	// []string{"cp", "my file.txt", "$HOME/backup"}
	// []string{"echo", "single quotes", "escaped space"}
}

// Parse a command line such as the value of $EDITOR into a program and its
// arguments, leaving any expansions for a shell to perform later.
func ExampleSplit_editor() {
	args, _ := shell.Split(`code --wait --user-data-dir "$HOME/My Config"`)
	fmt.Printf("%q\n", args[0])
	fmt.Printf("%q\n", args[1:])
	// Output:
	// "code"
	// ["--wait" "--user-data-dir" "$HOME/My Config"]
}

func ExampleJoin() {
	out, _ := shell.Join("rm", "-f", "my file.txt", "$notavar")
	fmt.Println(out)
	// Output:
	// rm -f 'my file.txt' '$notavar'
}

// Build a command line to run via a remote shell.
func ExampleJoin_ssh() {
	line, _ := shell.Join("mkdir", "-p", "backups/2026 09")
	fmt.Println("ssh host " + line)
	// Output:
	// ssh host mkdir -p 'backups/2026 09'
}

func ExampleMatch() {
	ok, _ := shell.Match("*.go", "cmd/shfmt/main.go")
	fmt.Println(ok)

	ok, _ = shell.Match("@(main|test).go", "test.go")
	fmt.Println(ok)

	ok, _ = shell.Match("!(*.go)", "README.md")
	fmt.Println(ok)
	// Output:
	// true
	// true
	// true
}

func ExampleGlob() {
	fsys := fstest.MapFS{
		"main.go":          {},
		"main_test.go":     {},
		"cmd/tool/main.go": {},
		".hidden.go":       {},
	}
	names, _ := shell.Glob(fsys, "**/*.go")
	fmt.Println(names)

	names, _ = shell.Glob(fsys, "*_test.go cmd/*/main.go")
	fmt.Println(names)
	// Output:
	// [cmd/tool/main.go main.go main_test.go]
	// [main_test.go cmd/tool/main.go]
}
