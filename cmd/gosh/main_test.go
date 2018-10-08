// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main

import (
	"fmt"
	"testing"

	"mvdan.cc/sh/internal"
	"mvdan.cc/sh/interp"
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
		"echo foo\n",
		"foo\n",
	},
	{
		"echo foo\n",
		"foo\n",
		"",
		"$ ",
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
}

func TestInteractive(t *testing.T) {
	t.Parallel()
	runner, _ := interp.New()
	for i, tc := range interactiveTests {
		t.Run(fmt.Sprintf("%02d", i), func(t *testing.T) {
			input := internal.ChanPipe(make(chan []byte, 8))
			output := internal.ChanPipe(make(chan []byte, 8))
			interp.StdIO(&input, &output, &output)(runner)
			runner.Reset()

			errc := make(chan error)
			go func() {
				errc <- interactive(runner)
			}()

			if err := output.ReadString("$ "); err != nil {
				t.Fatal(err)
			}

			line := 1
			for len(tc) > 0 {
				input.WriteString(tc[0])
				if err := output.ReadString(tc[1]); err != nil {
					t.Fatal(err)
				}

				line++
				tc = tc[2:]
			}

			// Close the input channel, so that the shell prompt can
			// reach an EOF read and finish.
			close(input)
			if err := <-errc; err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Close the output channel once the shell prompt has
			// finished.
			close(output)
		})
	}
}
