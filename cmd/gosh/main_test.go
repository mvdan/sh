// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"testing"

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

var interactiveTests = []struct {
	pairs   []string
	wantErr string
}{
	{},
	{
		pairs: []string{
			"\n",
			"$ ",
			"\n",
			"$ ",
		},
	},
	{
		pairs: []string{
			"echo foo\n",
			"foo\n",
		},
	},
	{
		pairs: []string{
			"echo foo\n",
			"foo\n$ ",
			"echo bar\n",
			"bar\n",
		},
	},
	{
		pairs: []string{
			"if true\n",
			"> ",
			"then echo bar; fi\n",
			"bar\n",
		},
	},
	{
		pairs: []string{
			"echo 'foo\n",
			"> ",
			"bar'\n",
			"foo\nbar\n",
		},
	},
	{
		pairs: []string{
			"echo foo; echo bar\n",
			"foo\nbar\n",
		},
	},
	{
		pairs: []string{
			"echo foo; echo 'bar\n",
			"> ",
			"baz'\n",
			"foo\nbar\nbaz\n",
		},
	},
	{
		pairs: []string{
			"(\n",
			"> ",
			"echo foo)\n",
			"foo\n",
		},
	},
	{
		pairs: []string{
			"[[\n",
			"> ",
			"true ]]\n",
			"$ ",
		},
	},
	{
		pairs: []string{
			"echo foo ||\n",
			"> ",
			"echo bar\n",
			"foo\n",
		},
	},
	{
		pairs: []string{
			"echo foo |\n",
			"> ",
			"read var; echo $var\n",
			"foo\n",
		},
	},
	{
		pairs: []string{
			"echo foo",
			"",
			" bar\n",
			"foo bar\n",
		},
	},
	{
		pairs: []string{
			"echo\\\n",
			"> ",
			" foo\n",
			"foo\n",
		},
	},
	{
		pairs: []string{
			"echo foo\\\n",
			"> ",
			"bar\n",
			"foobar\n",
		},
	},
	{
		pairs: []string{
			"echo 你好\n",
			"你好\n$ ",
		},
	},
	{
		pairs: []string{
			"echo foo; exit 0; echo bar\n",
			"foo\n",
			"echo baz\n",
			"",
		},
	},
	{
		pairs: []string{
			"echo foo; exit 1; echo bar\n",
			"foo\n",
			"echo baz\n",
			"",
		},
		wantErr: "exit status 1",
	},
	{
		pairs: []string{
			"(\n",
			"> ",
		},
		wantErr: "1:1: reached EOF without matching ( with )",
	},
}

func TestInteractive(t *testing.T) {
	t.Parallel()
	for i, tc := range interactiveTests {
		t.Run(fmt.Sprintf("%02d", i), func(t *testing.T) {
			inReader, inWriter := io.Pipe()
			outReader, outWriter := io.Pipe()
			runner, _ := interp.New(interp.StdIO(inReader, outWriter, outWriter))
			errc := make(chan error, 1)
			go func() {
				errc <- runInteractive(runner, inReader, outWriter, outWriter)
				// Discard the rest of the input.
				io.Copy(ioutil.Discard, inReader)
			}()

			if err := readString(outReader, "$ "); err != nil {
				t.Fatal(err)
			}

			line := 1
			for len(tc.pairs) > 0 {
				if _, err := io.WriteString(inWriter, tc.pairs[0]); err != nil {
					t.Fatal(err)
				}
				if err := readString(outReader, tc.pairs[1]); err != nil {
					t.Fatal(err)
				}

				line++
				tc.pairs = tc.pairs[2:]
			}

			// Close the input pipe, so that the parser can stop.
			inWriter.Close()

			// Once the input pipe is closed, close the output pipe
			// so that any remaining prompt writes get discarded.
			outReader.Close()

			err := <-errc
			if err != nil && tc.wantErr == "" {
				t.Fatalf("unexpected error: %v", err)
			} else if tc.wantErr != "" && fmt.Sprint(err) != tc.wantErr {
				t.Fatalf("want error %q, got: %v", tc.wantErr, err)
			}
		})
	}
}

func TestInteractiveExit(t *testing.T) {
	inReader, inWriter := io.Pipe()
	defer inReader.Close()
	go io.WriteString(inWriter, "exit\n")
	w := ioutil.Discard
	runner, _ := interp.New(interp.StdIO(inReader, w, w))
	if err := runInteractive(runner, inReader, w, w); err != nil {
		t.Fatal("expected a nil error")
	}
}

// readString will keep reading from a reader until all bytes from the supplied
// string are read.
func readString(r io.Reader, want string) error {
	p := make([]byte, len(want))
	_, err := io.ReadFull(r, p)
	if err != nil {
		return err
	}
	got := string(p)
	if got != want {
		return fmt.Errorf("ReadString: read %q, wanted %q", got, want)
	}
	return nil
}
