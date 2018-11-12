// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main

import (
	"fmt"
	"io"
	"testing"

	"mvdan.cc/sh/v3/internal"
	"mvdan.cc/sh/v3/interp"
)

// Each test has an even number of strings, which form input-output pairs for
// the interactive shell. The input string is fed to the interactive shell, and
// bytes are read from its output until the expected output string is matched or
// an error is encountered.
//
// In other words, each first string is what the user types, and each following
// string is what the shell will print back. Note that the first "$ " output is
// implicit.

var interactiveTests = [][]string{
	{},
	{
		"\n",
		"$ ",
		"\n",
		"$ ",
	},
	{
		"echo foo\n",
		"foo\n",
	},
	{
		"echo foo\n",
		"foo\n$ ",
		"echo bar\n",
		"bar\n",
	},
	{
		"if true\n",
		"> ",
		"then echo bar; fi\n",
		"bar\n",
	},
	{
		"echo 'foo\n",
		"> ",
		"bar'\n",
		"foo\nbar\n",
	},
	{
		"echo foo; echo bar\n",
		"foo\nbar\n",
	},
	{
		"echo foo; echo 'bar\n",
		"> ",
		"baz'\n",
		"foo\nbar\nbaz\n",
	},
	{
		"(\n",
		"> ",
		"echo foo)\n",
		"foo\n",
	},
	{
		"[[\n",
		"> ",
		"true ]]\n",
		"$ ",
	},
	{
		"echo foo ||\n",
		"> ",
		"echo bar\n",
		"foo\n",
	},
	{
		"echo foo |\n",
		"> ",
		"cat\n",
		"foo\n",
	},
	{
		"echo foo",
		"",
		" bar\n",
		"foo bar\n",
	},
	{
		"echo\\\n",
		"> ",
		" foo\n",
		"foo\n",
	},
	{
		"echo foo\\\n",
		"> ",
		"bar\n",
		"foobar\n",
	},
}

func TestInteractive(t *testing.T) {
	t.Parallel()
	runner, _ := interp.New()
	for i, tc := range interactiveTests {
		t.Run(fmt.Sprintf("%02d", i), func(t *testing.T) {
			inReader, inWriter := io.Pipe()
			outReader, outWriter := io.Pipe()
			interp.StdIO(inReader, outWriter, outWriter)(runner)
			runner.Reset()

			errc := make(chan error)
			go func() {
				errc <- interactive(runner)
			}()

			if err := internal.ReadString(outReader, "$ "); err != nil {
				t.Fatal(err)
			}

			line := 1
			for len(tc) > 0 {
				if _, err := io.WriteString(inWriter, tc[0]); err != nil {
					t.Fatal(err)
				}
				if err := internal.ReadString(outReader, tc[1]); err != nil {
					t.Fatal(err)
				}

				line++
				tc = tc[2:]
			}

			// Close the input pipe, so that the parser can stop.
			inWriter.Close()

			// Once the input pipe is closed, close the output pipe
			// so that any remaining prompt writes get discarded.
			outReader.Close()

			if err := <-errc; err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
