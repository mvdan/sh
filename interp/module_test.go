// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

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
		exec: func(ctx Ctxt, name string, args []string) error {
			if name == "sleep" {
				return fmt.Errorf("blacklisted: %s", name)
			}
			return DefaultExec(ctx, name, args)
		},
		src:  "echo foo; /bin/echo foo; sleep 1",
		want: "foo\nfoo\nblacklisted: sleep",
	},
	{
		name: "ExecWhitelist",
		exec: func(ctx Ctxt, name string, args []string) error {
			switch name {
			case "sed", "grep":
			default:
				return fmt.Errorf("blacklisted: %s", name)
			}
			return DefaultExec(ctx, name, args)
		},
		src:  "a=$(echo foo | sed 's/o/a/g'); echo $a; $a args",
		want: "faa\nblacklisted: faa",
	},
	{
		name: "ExecSubshell",
		exec: func(ctx Ctxt, name string, args []string) error {
			return fmt.Errorf("blacklisted: %s", name)
		},
		src:  "(malicious)",
		want: "blacklisted: malicious",
	},
	{
		name: "ExecPipe",
		exec: func(ctx Ctxt, name string, args []string) error {
			return fmt.Errorf("blacklisted: %s", name)
		},
		src:  "malicious | echo foo",
		want: "foo\nblacklisted: malicious",
	},
	{
		name: "ExecCmdSubst",
		exec: func(ctx Ctxt, name string, args []string) error {
			return fmt.Errorf("blacklisted: %s", name)
		},
		src:  "a=$(malicious)",
		want: "blacklisted: malicious",
	},
	{
		name: "ExecBackground",
		exec: func(ctx Ctxt, name string, args []string) error {
			return fmt.Errorf("blacklisted: %s", name)
		},
		// TODO: find a way to bubble up the error, perhaps
		src:  "{ malicious; echo foo; } & wait",
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
