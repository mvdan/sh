// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"mvdan.cc/sh/v3/syntax"
)

func blocklistBuiltinExec(name string) ExecHandlerFunc {
	return func(ctx context.Context, args []string) error {
		if args[0] == name {
			return fmt.Errorf("%s: blocklisted builtin", name)
		}
		return testExecHandler(ctx, args)
	}
}

func blocklistAllExec(ctx context.Context, args []string) error {
	return fmt.Errorf("blocklisted: %s", args[0])
}

func blocklistNondevOpen(ctx context.Context, path string, flags int, mode os.FileMode) (io.ReadWriteCloser, error) {
	if path != "/dev/null" {
		return nil, fmt.Errorf("non-dev: %s", path)
	}

	return testOpenHandler(ctx, path, flags, mode)
}

func blocklistGlob(ctx context.Context, path string) ([]os.FileInfo, error) {
	return nil, fmt.Errorf("blocklisted: glob")
}

// runnerCtx allows us to give handler functions access to the Runner, if needed.
var runnerCtx = new(int)

func execBuiltin(ctx context.Context, args []string) error {
	runner, ok := ctx.Value(runnerCtx).(*Runner)
	if ok && runner.Exited() {
		return fmt.Errorf("exec builtin: %s", args[0])
	}
	return nil
}

var modCases = []struct {
	name    string
	exec    ExecHandlerFunc
	open    OpenHandlerFunc
	call    CallHandlerFunc
	readdir ReadDirHandlerFunc
	src     string
	want    string
}{
	{
		name: "ExecBlocklistOne",
		exec: blocklistBuiltinExec("sleep"),
		src:  "echo foo; sleep 1",
		want: "foo\nsleep: blocklisted builtin",
	},
	{
		name: "ExecBlocklistOneSubshell",
		exec: blocklistBuiltinExec("faa"),
		src:  "a=$(echo foo | sed 's/o/a/g'); echo $a; $a args",
		want: "faa\nfaa: blocklisted builtin",
	},
	{
		name: "ExecBlocklistAllSubshell",
		exec: blocklistAllExec,
		src:  "(malicious)",
		want: "blocklisted: malicious",
	},
	{
		name: "ExecPipe",
		exec: blocklistAllExec,
		src:  "malicious | echo foo",
		want: "foo\nblocklisted: malicious",
	},
	{
		name: "ExecCmdSubst",
		exec: blocklistAllExec,
		src:  "a=$(malicious)",
		want: "blocklisted: malicious\n", // TODO: why the newline?
	},
	{
		name: "ExecBackground",
		exec: blocklistAllExec,
		src:  "{ malicious; true; } & { malicious; true; } & wait",
		want: "blocklisted: malicious",
	},
	{
		name: "ExecBuiltin",
		exec: execBuiltin,
		src:  "exec /bin/sh",
		want: "exec builtin: /bin/sh",
	},
	{
		name: "OpenForbidNonDev",
		open: blocklistNondevOpen,
		src:  "echo foo >/dev/null; echo bar >/tmp/x",
		want: "non-dev: /tmp/x",
	},
	{
		name: "CallReplaceWithBlank",
		open: blocklistNondevOpen,
		call: func(ctx context.Context, args []string) ([]string, error) {
			return []string{"echo", "blank"}, nil
		},
		src:  "echo foo >/dev/null; { bar; } && baz",
		want: "blank\nblank\n",
	},
	{
		name: "CallDryRun",
		call: func(ctx context.Context, args []string) ([]string, error) {
			return append([]string{"echo", "run:"}, args...), nil
		},
		src:  "cd some-dir; cat foo; exit 1",
		want: "run: cd some-dir\nrun: cat foo\nrun: exit 1\n",
	},
	{
		name: "CallError",
		call: func(ctx context.Context, args []string) ([]string, error) {
			if args[0] == "echo" && len(args) > 2 {
				return nil, fmt.Errorf("refusing to run echo builtin with multiple args")
			}
			return args, nil
		},
		src:  "echo foo; echo foo bar",
		want: "foo\nrefusing to run echo builtin with multiple args",
	},
	{
		name:    "GlobForbid",
		readdir: blocklistGlob,
		src:     "echo *",
		want:    "blocklisted: glob\n",
	},
}

func TestRunnerHandlers(t *testing.T) {
	t.Parallel()

	p := syntax.NewParser()
	for _, tc := range modCases {
		t.Run(tc.name, func(t *testing.T) {
			file := parse(t, p, tc.src)
			var cb concBuffer
			r, err := New(StdIO(nil, &cb, &cb))
			if tc.exec != nil {
				ExecHandler(tc.exec)(r)
			}
			if tc.open != nil {
				OpenHandler(tc.open)(r)
			}
			if tc.call != nil {
				CallHandler(tc.call)(r)
			}
			if tc.readdir != nil {
				ReadDirHandler(tc.readdir)(r)
			}
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.WithValue(context.Background(), runnerCtx, r)
			if err := r.Run(ctx, file); err != nil {
				cb.WriteString(err.Error())
			}
			got := cb.String()
			if got != tc.want {
				t.Fatalf("want:\n%q\ngot:\n%q", tc.want, got)
			}
		})
	}
}

type readyBuffer struct {
	buf       bytes.Buffer
	seenReady sync.WaitGroup
}

func (b *readyBuffer) Write(p []byte) (n int, err error) {
	if string(p) == "ready\n" {
		b.seenReady.Done()
		return len(p), nil
	}
	return b.buf.Write(p)
}

func TestKillTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("sleeps and timeouts are slow")
	}
	if runtime.GOOS == "windows" {
		t.Skip("skipping trap tests on windows")
	}
	t.Parallel()

	tests := []struct {
		src         string
		want        string
		killTimeout time.Duration
		forcedKill  bool
	}{
		// killed immediately
		{
			`sh -c "trap 'echo trapped; exit 0' INT; echo ready; for i in \$(seq 1 100); do sleep 0.01; done"`,
			"",
			-1,
			true,
		},
		// interrupted first, and stops itself in time
		{
			`sh -c "trap 'echo trapped; exit 0' INT; echo ready; for i in \$(seq 1 100); do sleep 0.01; done"`,
			"trapped\n",
			time.Second,
			false,
		},
		// interrupted first, but does not stop itself in time
		{
			`sh -c "trap 'echo trapped; for i in \$(seq 1 100); do sleep 0.01; done' INT; echo ready; for i in \$(seq 1 100); do sleep 0.01; done"`,
			"trapped\n",
			20 * time.Millisecond,
			true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run("", func(t *testing.T) {
			t.Parallel()
			file := parse(t, nil, test.src)
			attempt := 0
			for {
				var rbuf readyBuffer
				rbuf.seenReady.Add(1)
				ctx, cancel := context.WithCancel(context.Background())
				r, err := New(
					StdIO(nil, &rbuf, &rbuf),
					ExecHandler(DefaultExecHandler(test.killTimeout)),
				)
				if err != nil {
					t.Fatal(err)
				}
				go func() {
					rbuf.seenReady.Wait()
					cancel()
				}()
				err = r.Run(ctx, file)
				if test.forcedKill {
					if _, ok := IsExitStatus(err); ok || err == nil {
						t.Error("command was not force-killed")
					}
				} else {
					if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
						t.Errorf("execution errored: %v", err)
					}
				}
				got := rbuf.buf.String()
				if got != test.want {
					if attempt < 3 && got == "" && test.killTimeout > 0 {
						attempt++
						test.killTimeout *= 2
						continue
					}
					t.Fatalf("want:\n%s\ngot:\n%s", test.want, got)
				}
				break
			}
		})
	}
}

func TestKillSignal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping signal tests on windows")
	}
	tests := []struct {
		signal os.Signal
		want   error
	}{
		{syscall.SIGINT, NewExitStatus(130)},  // 128 + 2
		{syscall.SIGKILL, NewExitStatus(137)}, // 128 + 9
		{syscall.SIGTERM, NewExitStatus(143)}, // 128 + 15
	}

	// pid_and_hang is implemented in TestMain; we use it to have the
	// interpreter spawn a process, and easily grab its PID to send it a
	// signal directly. The program prints its PID and hangs forever.
	file := parse(t, nil, "GOSH_CMD=pid_and_hang $GOSH_PROG")
	for _, test := range tests {
		test := test
		t.Run(fmt.Sprintf("signal-%d", test.signal), func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			outReader, outWriter := io.Pipe()
			stderr := new(bytes.Buffer)
			r, _ := New(StdIO(nil, outWriter, stderr))
			errch := make(chan error, 1)
			go func() {
				errch <- r.Run(ctx, file)
				outWriter.Close()
			}()

			br := bufio.NewReader(outReader)
			line, err := br.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			pid, err := strconv.Atoi(strings.TrimSpace(line))
			if err != nil {
				t.Fatal(err)
			}

			proc, err := os.FindProcess(pid)
			if err != nil {
				t.Fatal(err)
			}
			if err := proc.Signal(test.signal); err != nil {
				t.Fatal(err)
			}
			if got := <-errch; got != test.want {
				t.Fatalf("want error %v, got %v. stderr: %s", test.want, got, stderr)
			}
		})
	}
}
