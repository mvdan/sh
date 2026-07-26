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
			if err == nil {
				t.Fatalf("expected error for command %q, got none", cmd)
			}

			if !strings.Contains(err.Error(), "flag provided but not defined: -badoption") {
				t.Errorf("expected error for command %q, got: %v", cmd, err)
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
	// TODO: a failing core utility should result in a regular non-zero
	// exit status so that the script can recover, rather than a fatal
	// error which aborts the entire run.
	if err == nil {
		t.Fatalf("expected Run to return a fatal error; output: %q", out.String())
	}
	if got := out.String(); got != "" {
		t.Fatalf("expected no output, got: %q", got)
	}
}
