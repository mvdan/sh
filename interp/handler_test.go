// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp_test

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

func blocklistOneExec(name string) func(interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
		return func(ctx context.Context, args []string) error {
			if args[0] == name {
				return fmt.Errorf("%s: blocklisted program", name)
			}
			return next(ctx, args)
		}
	}
}

func blocklistAllExec(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(ctx context.Context, args []string) error {
		return fmt.Errorf("blocklisted: %s", args[0])
	}
}

func blocklistNondevOpen(ctx context.Context, path string, flags int, mode os.FileMode) (io.ReadWriteCloser, error) {
	if path != "/dev/null" {
		return nil, fmt.Errorf("non-dev: %s", path)
	}

	return testOpenHandler(ctx, path, flags, mode)
}

func blocklistGlob(ctx context.Context, path string) ([]fs.FileInfo, error) {
	return nil, fmt.Errorf("blocklisted: glob")
}

func execPrint(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(ctx context.Context, args []string) error {
		hc := interp.HandlerCtx(ctx)
		fmt.Fprintf(hc.Stdout, "would run: %s", args)
		return nil
	}
}

// runnerCtx allows us to give handler functions access to the Runner, if needed.
var runnerCtx = new(int)

func execPrintWouldExec(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(ctx context.Context, args []string) error {
		runner, ok := ctx.Value(runnerCtx).(*interp.Runner)
		if ok && runner.Exited() {
			return fmt.Errorf("would exec via builtin: %s", args)
		}
		return nil
	}
}

// TODO: join with TestRunnerOpts?
var modCases = []struct {
	name string
	opts []interp.RunnerOption
	src  string
	want string
}{
	{
		name: "ExecBlocklistOne",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(blocklistOneExec("sleep")),
		},
		src:  "echo foo; sleep 1",
		want: "foo\nsleep: blocklisted program",
	},
	{
		name: "ExecBlocklistOneSubshell",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(blocklistOneExec("faa")),
		},
		src:  "a=$(echo foo | sed 's/o/a/g'); echo $a; $a args",
		want: "faa\nfaa: blocklisted program",
	},
	{
		name: "ExecBlocklistAllSubshell",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(blocklistAllExec),
		},
		src:  "(malicious)",
		want: "blocklisted: malicious",
	},
	{
		name: "ExecPipe",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(blocklistAllExec),
		},
		src:  "malicious | echo foo",
		want: "foo\nblocklisted: malicious",
	},
	{
		name: "ExecCmdSubst",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(blocklistAllExec),
		},
		src:  "a=$(malicious)",
		want: "blocklisted: malicious\n", // TODO: why the newline?
	},
	{
		name: "ExecBackground",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(blocklistAllExec),
		},
		src:  "{ malicious; true; } & { malicious; true; } & wait",
		want: "blocklisted: malicious",
	},
	{
		name: "ExecPrintWouldExec",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(execPrintWouldExec),
		},
		src:  "exec /bin/sh",
		want: "would exec via builtin: [/bin/sh]",
	},
	{
		name: "ExecPrintAndBlocklist",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(
				execPrint,
				blocklistOneExec("foo"),
			),
		},
		src:  "foo",
		want: "would run: [foo]",
	},
	{
		name: "ExecPrintAndBlocklistSeparate",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(execPrint),
			interp.ExecHandlers(blocklistOneExec("foo")),
		},
		src:  "foo",
		want: "would run: [foo]",
	},
	{
		name: "ExecBlocklistAndPrint",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(
				blocklistOneExec("foo"),
				execPrint,
			),
		},
		src:  "foo",
		want: "foo: blocklisted program",
	},
	{
		name: "OpenForbidNonDev",
		opts: []interp.RunnerOption{
			interp.OpenHandler(blocklistNondevOpen),
		},
		src:  "echo foo >/dev/null; echo bar >/tmp/x",
		want: "non-dev: /tmp/x",
	},
	{
		name: "CallReplaceWithBlank",
		opts: []interp.RunnerOption{
			interp.OpenHandler(blocklistNondevOpen),
			interp.CallHandler(func(ctx context.Context, args []string) ([]string, error) {
				return []string{"echo", "blank"}, nil
			}),
		},
		src:  "echo foo >/dev/null; { bar; } && baz",
		want: "blank\nblank\n",
	},
	{
		name: "CallDryRun",
		opts: []interp.RunnerOption{
			interp.CallHandler(func(ctx context.Context, args []string) ([]string, error) {
				return append([]string{"echo", "run:"}, args...), nil
			}),
		},
		src:  "cd some-dir; cat foo; exit 1",
		want: "run: cd some-dir\nrun: cat foo\nrun: exit 1\n",
	},
	{
		name: "CallError",
		opts: []interp.RunnerOption{
			interp.CallHandler(func(ctx context.Context, args []string) ([]string, error) {
				if args[0] == "echo" && len(args) > 2 {
					return nil, fmt.Errorf("refusing to run echo builtin with multiple args")
				}
				return args, nil
			}),
		},
		src:  "echo foo; echo foo bar",
		want: "foo\nrefusing to run echo builtin with multiple args",
	},
	{
		name: "GlobForbid",
		opts: []interp.RunnerOption{
			interp.ReadDirHandler(blocklistGlob),
		},
		src:  "echo *",
		want: "blocklisted: glob\n",
	},
}

func TestRunnerHandlers(t *testing.T) {
	t.Parallel()

	p := syntax.NewParser()
	for _, tc := range modCases {
		t.Run(tc.name, func(t *testing.T) {
			file := parse(t, p, tc.src)
			var cb concBuffer
			r, err := interp.New(interp.StdIO(nil, &cb, &cb))
			if err != nil {
				t.Fatal(err)
			}
			for _, opt := range tc.opts {
				opt(r)
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
				r, err := interp.New(
					interp.StdIO(nil, &rbuf, &rbuf),
					interp.ExecHandler(interp.DefaultExecHandler(test.killTimeout)),
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
					if _, ok := interp.IsExitStatus(err); ok || err == nil {
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
		{syscall.SIGINT, interp.NewExitStatus(130)},  // 128 + 2
		{syscall.SIGKILL, interp.NewExitStatus(137)}, // 128 + 9
		{syscall.SIGTERM, interp.NewExitStatus(143)}, // 128 + 15
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
			r, _ := interp.New(interp.StdIO(nil, outWriter, stderr))
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
