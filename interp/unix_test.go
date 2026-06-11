// Copyright (c) 2019, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

//go:build unix

package interp_test

import (
	"bufio"
	"context"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"mvdan.cc/sh/v3/interp"
)

type virtualDirInfo struct{}

func (virtualDirInfo) Name() string       { return "virtual" }
func (virtualDirInfo) Size() int64        { return 0 }
func (virtualDirInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o755 }
func (virtualDirInfo) ModTime() time.Time { return time.Time{} }
func (virtualDirInfo) IsDir() bool        { return true }
func (virtualDirInfo) Sys() any           { return nil }

func TestRunnerAccessStatHandlerVirtualDir(t *testing.T) {
	t.Parallel()

	file := parse(t, nil, "cd /virtual; pwd")
	stat := func(ctx context.Context, name string, followSymlinks bool) (fs.FileInfo, error) {
		if name == "/virtual" {
			return virtualDirInfo{}, nil
		}
		return interp.DefaultStatHandler()(ctx, name, followSymlinks)
	}
	var cb concBuffer
	r, err := interp.New(
		interp.StdIO(nil, &cb, &cb),
		interp.StatHandler(stat),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Run(context.Background(), file); err != nil {
		t.Fatal(err)
	}
	if got, want := cb.String(), "/virtual\n"; got != want {
		t.Fatalf("\nwant: %q\ngot:  %q", want, got)
	}
}

func TestRunnerTerminalStdIO(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files func(*testing.T) (secondary io.Writer, primary io.Reader)
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
			primary, secondary, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			return secondary, primary
		}, "012end\r\n"},
	}
	file := parse(t, nil, `
		for n in 0 1 2 3; do if [[ -t $n ]]; then echo -n $n; fi; done; echo end
	`)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			secondary, primary := test.files(t)
			// some secondary ends can be used as stdin too
			secondaryReader, _ := secondary.(io.Reader)

			r, _ := interp.New(interp.StdIO(secondaryReader, secondary, secondary))
			go func() {
				// To mimic [os/exec.Cmd.Start], use a goroutine.
				if err := r.Run(context.Background(), file); err != nil {
					t.Error(err)
				}
			}()

			got, err := bufio.NewReader(primary).ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("\nwant: %q\ngot:  %q", test.want, got)
			}
			if closer, ok := secondary.(io.Closer); ok {
				if err := closer.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if closer, ok := primary.(io.Closer); ok {
				if err := closer.Close(); err != nil {
					t.Fatal(err)
				}
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
			// Note that we avoid pty.Start,
			// as it closes the secondary terminal via a defer,
			// possibly before the command has finished.
			// That can lead to "signal: hangup" flakes.
			primary, secondary, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			cmd.Stdin = secondary
			cmd.Stdout = secondary
			cmd.Stderr = secondary
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			return primary
		}, "012end\r\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			cmd := exec.Command(os.Getenv("GOSH_PROG"),
				"for n in 0 1 2 3; do if [[ -t $n ]]; then echo -n $n; fi; done; echo end")
			primary := test.start(t, cmd)

			got, err := bufio.NewReader(primary).ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("\nwant: %q\ngot:  %q", test.want, got)
			}
			if closer, ok := primary.(io.Closer); ok {
				if err := closer.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if err := cmd.Wait(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func shortPathName(path string) (string, error) {
	panic("only works on windows")
}
