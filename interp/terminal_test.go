// Copyright (c) 2019, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

// +build !windows

package interp

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/creack/pty"
)

func TestRunnerTerminalStdIO(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files func(*testing.T) (slave io.Writer, master io.Reader)
		want  string
	}{
		{"Nil", func(t *testing.T) (io.Writer, io.Reader) {
			return nil, strings.NewReader("\n")
		}, "\n"},
		{"Pipe", func(t *testing.T) (io.Writer, io.Reader) {
			pr, pw := io.Pipe()
			return pw, pr
		}, "end\n"},
		{"Pseudo", func(t *testing.T) (io.Writer, io.Reader) {
			pty, tty, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			return tty, pty
		}, "012end\r\n"},
	}
	file := parse(t, nil, `
		for n in 0 1 2 3; do if [[ -t $n ]]; then echo -n $n; fi; done; echo end
	`)
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			slave, master := test.files(t)
			// some slaves can be used as stdin too, like a tty
			slaveReader, _ := slave.(io.Reader)

			r, _ := New(StdIO(slaveReader, slave, slave))
			go func() {
				// To mimic os/exec.Cmd.Start, use a goroutine.
				if err := r.Run(context.Background(), file); err != nil {
					t.Error(err)
				}
			}()

			got, err := bufio.NewReader(master).ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("\nwant: %q\ngot:  %q", test.want, got)
			}
			if closer, ok := slave.(io.Closer); ok {
				closer.Close()
			}
			if closer, ok := master.(io.Closer); ok {
				closer.Close()
			}
		})
	}
}

func TestRunnerTerminalExec(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		start func(*testing.T, *exec.Cmd) io.Reader
		want  string
	}{
		{"Nil", func(t *testing.T, cmd *exec.Cmd) io.Reader {
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			return strings.NewReader("\n")
		}, "\n"},
		{"Pipe", func(t *testing.T, cmd *exec.Cmd) io.Reader {
			out, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			cmd.Stderr = cmd.Stdout
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			return out
		}, "end\n"},
		{"Pseudo", func(t *testing.T, cmd *exec.Cmd) io.Reader {
			pty_, err := pty.Start(cmd)
			if err != nil {
				t.Fatal(err)
			}
			return pty_
		}, "012end\r\n"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			if test.name == "Pseudo" {
				t.Skipf("too flaky on GitHub's machines; see https://github.com/mvdan/sh/issues/513")
			}
			t.Parallel()

			cmd := exec.Command(os.Getenv("GOSH_PROG"),
				"for n in 0 1 2 3; do if [[ -t $n ]]; then echo -n $n; fi; done; echo end")
			out := test.start(t, cmd)

			got, err := bufio.NewReader(out).ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("\nwant: %q\ngot:  %q", test.want, string(got))
			}
			if err := cmd.Wait(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
