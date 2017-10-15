// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"fmt"
	"strings"
	"testing"

	"mvdan.cc/sh/syntax"
)

var modCases = []struct {
	name string
	mods func(r *Runner)
	src  string
	want string
}{
	{
		"ExecBlacklist",
		func(r *Runner) {
			r.Exec = func(ctx Ctxt, name string, args []string) error {
				if name == "sleep" {
					return fmt.Errorf("blacklisted: %s", name)
				}
				return DefaultExec(ctx, name, args)
			}
		},
		"echo foo; /bin/echo foo; sleep 1",
		"foo\nfoo\nblacklisted: sleep",
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
			}
			r.Reset()
			tc.mods(&r)
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
