// Copyright (c) 2025, Andrey Nering <andrey@nering.com.br>
// See LICENSE for licensing information

package coreutils

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

func TestExecHandler(t *testing.T) {
	for coreUtil := range commandBuilders {
		t.Run(coreUtil, func(t *testing.T) {
			var in bytes.Buffer
			var out strings.Builder

			r, err := interp.New(
				interp.StdIO(&in, &out, &out),
				interp.ExecHandlers(ExecHandler),
			)
			if err != nil {
				t.Fatalf("failed to create interpreter: %v", err)
			}

			cmd := fmt.Sprintf("%s --badoption", coreUtil)

			program, err := syntax.NewParser().Parse(strings.NewReader(cmd), "")
			if err != nil {
				t.Fatalf("failed to parse command %q: %v", cmd, err)
			}
			err = r.Run(t.Context(), program)
			if code, ok := interp.IsExitStatus(err); !ok || code != 1 {
				t.Fatalf("expected exit status 1 for command %q, got: %v", cmd, err)
			}

			if !strings.Contains(out.String(), "flag provided but not defined: -badoption") {
				t.Errorf("expected error output for command %q, got: %q", cmd, out.String())
			}
		})
	}
}

func TestExecHandlerErrorNotFatal(t *testing.T) {
	var out strings.Builder
	r, err := interp.New(
		interp.Dir(t.TempDir()),
		interp.StdIO(nil, &out, &out),
		interp.ExecHandlers(ExecHandler),
	)
	if err != nil {
		t.Fatalf("failed to create interpreter: %v", err)
	}

	cmd := "rm does-not-exist || echo recovered"
	program, err := syntax.NewParser().Parse(strings.NewReader(cmd), "")
	if err != nil {
		t.Fatalf("failed to parse command %q: %v", cmd, err)
	}
	err = r.Run(t.Context(), program)
	if err != nil {
		t.Fatalf("expected Run to succeed, got: %v", err)
	}
	// The error message printed by rm varies by platform; only check
	// that the script recovered after it.
	got := out.String()
	if !strings.HasPrefix(got, "rm: ") || !strings.HasSuffix(got, "\nrecovered\n") {
		t.Fatalf("expected an rm error followed by %q, got: %q", "recovered", got)
	}
}
