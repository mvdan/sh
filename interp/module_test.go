// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"mvdan.cc/sh/syntax"
)

var modCases = []struct {
	name string
	exec ModuleExec
	open ModuleOpen
	src  string
	want string
}{
	{
		name: "ExecBlacklist",
		exec: func(ctx Ctxt, path, name string, args []string) error {
			if name == "sleep" {
				return fmt.Errorf("blacklisted: %s", name)
			}
			return DefaultExec(ctx, path, name, args)
		},
		src:  "echo foo; /bin/echo foo; sleep 1",
		want: "foo\nfoo\nblacklisted: sleep",
	},
	{
		name: "ExecWhitelist",
		exec: func(ctx Ctxt, path, name string, args []string) error {
			switch name {
			case "sed", "grep":
			default:
				return fmt.Errorf("blacklisted: %s", name)
			}
			return DefaultExec(ctx, path, name, args)
		},
		src:  "a=$(echo la | sed 's/a/s/g'); echo $a; $a args",
		want: "ls\nblacklisted: ls",
	},
	{
		name: "ExecSubshell",
		exec: func(ctx Ctxt, path, name string, args []string) error {
			return fmt.Errorf("blacklisted: %s", name)
		},
		src:  "(ls)",
		want: "blacklisted: ls",
	},
	{
		name: "ExecPipe",
		exec: func(ctx Ctxt, path, name string, args []string) error {
			return fmt.Errorf("blacklisted: %s", name)
		},
		src:  "ls | echo foo",
		want: "foo\nblacklisted: ls",
	},
	{
		name: "ExecCmdSubst",
		exec: func(ctx Ctxt, path, name string, args []string) error {
			return fmt.Errorf("blacklisted: %s", name)
		},
		src:  "a=$(ls)",
		want: "blacklisted: ls",
	},
	{
		name: "ExecBackground",
		exec: func(ctx Ctxt, path, name string, args []string) error {
			return fmt.Errorf("blacklisted: %s", name)
		},
		// TODO: find a way to bubble up the error, perhaps
		src:  "{ ls; echo foo; } & wait",
		want: "",
	},
	{
		name: "OpenForbidNonDev",
		open: func(ctx Ctxt, path string, flags int, mode os.FileMode) (io.ReadWriteCloser, error) {
			// won't pass on windows, but ok for now
			if !strings.HasPrefix(path, "/dev/") {
				return nil, fmt.Errorf("non-dev: %s", path)
			}
			return DefaultOpen(ctx, path, flags, mode)
		},
		src:  "echo foo >/dev/null; echo bar >/tmp/x",
		want: "non-dev: /tmp/x",
	},
}

func TestRunnerModules(t *testing.T) {
	p := syntax.NewParser()
	for _, tc := range modCases {
		t.Run(tc.name, func(t *testing.T) {
			file, err := p.Parse(strings.NewReader(tc.src), "")
			if err != nil {
				t.Fatalf("could not parse: %v", err)
			}
			var cb concBuffer
			r := Runner{
				Stdout: &cb,
				Stderr: &cb,
				Exec:   tc.exec,
				Open:   tc.open,
			}
			r.Reset()
			if err := r.Run(file); err != nil {
				cb.WriteString(err.Error())
			}
			got := cb.String()
			if got != tc.want {
				t.Fatalf("want:\n%s\ngot:\n%s", tc.want, got)
			}
		})
	}
}

func TestSignalSending(t *testing.T) {
	if testing.Short() {
		t.Skip("sleeps and timeouts are slow")
	}
	tests := []struct {
		src            string
		want           string
		contextTimeout time.Duration
		killTimeout    time.Duration
		forcedKill     bool
	}{
		// killed immediately
		{
			`bash -c "trap 'echo trap' INT; sleep 0.1"`,
			"",
			50 * time.Millisecond,
			-1,
			true,
		},
		// interrupted first, and stops itself in time
		{
			`bash -c "trap 'echo trapped' INT; sleep 0.1"`,
			"trapped\n",
			50 * time.Millisecond,
			time.Second,
			false,
		},
		// interrupted first, but does not stop itself in time
		{
			`bash -c "trap 'echo trapped; while true; do sleep 0.01; done' INT; sleep 0.1"`,
			"trapped\n",
			50 * time.Millisecond,
			100 * time.Millisecond,
			true,
		},
	}

	for i, test := range tests {
		t.Run(fmt.Sprintf("TestSignalSending%d", i+1), func(t *testing.T) {
			t.Parallel()
			p := syntax.NewParser()
			file, err := p.Parse(strings.NewReader(test.src), "")
			if err != nil {
				t.Errorf("could not parse: %v", err)
			}
			var buff bytes.Buffer
			ctx, _ := context.WithTimeout(context.Background(), test.contextTimeout)
			r := Runner{
				Context:     ctx,
				Stdout:      &buff,
				Stderr:      &buff,
				KillTimeout: test.killTimeout,
			}
			if err = r.Reset(); err != nil {
				t.Errorf("could not reset: %v", err)
			}
			err = r.Run(file)
			if test.forcedKill {
				if _, ok := err.(ExitCode); !ok {
					t.Error("command was not force-killed")
				}
			} else {
				if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
					t.Errorf("execution errored: %v", err)
				}
			}
			got := buff.String()
			if got != test.want {
				t.Fatalf("want:\n%s\ngot:\n%s", test.want, got)
			}
		})
	}
}
