// Copyright (c) 2019, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

//go:build unix

package interp_test

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/go-quicktest/qt"
	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/interp"
)

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

func TestExecETXTBSY(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "script.sh")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("#!/bin/sh\necho foo\n"); err != nil {
		t.Fatal(err)
	}
	// Hand the write fd to a child process which outlives our close below,
	// mimicking a concurrent fork inheriting the fd before its exec;
	// see https://go.dev/issue/22315. Executing the script fails with ETXTBSY
	// until the child exits, which happens once we close its stdin.
	holder := exec.Command("cat")
	holderStdin, err := holder.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	holder.ExtraFiles = []*os.File{f}
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	defer holder.Wait()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	time.AfterFunc(100*time.Millisecond, func() { holderStdin.Close() })
	var buf bytes.Buffer
	r, err := interp.New(interp.StdIO(nil, &buf, &buf))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Run(context.Background(), parse(t, nil, path)); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "foo\n" {
		t.Fatalf("want %q, got %q", "foo\n", got)
	}
}

func TestRunnerCommandProcSubst(t *testing.T) {
	t.Parallel()

	r, _ := interp.New()
	ctx, cancel := context.WithTimeout(t.Context(), runnerRunTimeout)
	defer cancel()
	// Nothing opens the process substitution, so wait would block on it
	// if running a bare command did not stop waiting on it once done.
	if err := r.Run(ctx, parse(t, nil, "test -e <(:)").Stmts[0].Cmd); err != nil {
		t.Fatal(err)
	}
	if err := r.Run(ctx, parse(t, nil, "wait")); err != nil {
		t.Fatal(err)
	}
	if err := ctx.Err(); err != nil {
		t.Fatal(err)
	}
}

// TestWaitBackgroundProcSubstCleanup checks that the FIFO of a process
// substitution has been removed once WaitBackground returns,
// whether or not anything opened it.
func TestWaitBackgroundProcSubstCleanup(t *testing.T) {
	t.Parallel()
	for _, script := range []string{
		": < <(echo opened)",
		"echo <(echo unopened) >/dev/null",
	} {
		t.Run(script, func(t *testing.T) {
			t.Parallel()
			tempDir := t.TempDir()
			runner, err := interp.New(interp.Env(expand.ListEnviron("TMPDIR=" + tempDir)))
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.IsNil(runner.Run(t.Context(), parse(t, nil, script))))
			runner.WaitBackground()
			entries, err := os.ReadDir(tempDir)
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.HasLen(entries, 0))
		})
	}
}

// TestBackgroundENOEXEC checks that the background jobs of a script without a
// shebang line, which runs in a nested shell, are covered by WaitBackground and
// keep using the files inherited from the caller.
func TestBackgroundENOEXEC(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	script := filepath.Join(dir, "script")
	qt.Assert(t, qt.IsNil(os.WriteFile(script, []byte("(read x; echo late) &\n"), 0o755)))
	pr, pw, err := os.Pipe()
	qt.Assert(t, qt.IsNil(err))
	defer pr.Close()
	runner, err := interp.New(interp.Dir(dir), interp.StdIO(pr, nil, nil))
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsNil(runner.Run(t.Context(), parse(t, nil, "{ ./script; } >out"))))
	// The job only writes once the caller is done with the file.
	pw.Close()
	runner.WaitBackground()
	out, err := os.ReadFile(filepath.Join(dir, "out"))
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(string(out), "late\n"))
}
