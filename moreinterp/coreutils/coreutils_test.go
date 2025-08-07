// Copyright (c) 2025, Andrey Nering <andrey@nering.com.br>
// See LICENSE for licensing information

package coreutils

import (
	"bytes"
	"context"
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
			err = r.Run(context.Background(), program)
			if err == nil {
				t.Fatalf("expected error for command %q, got none", cmd)
			}

			// FIXME(@andreynering): Return the proper flag error from u-root to
			// avoid a special tests for chmod and gzip.
			if coreUtil == "chmod" {
				if err.Error() != "chmod: chmod [mode] filepath" {
					t.Errorf("expected %q output, got: %q", cmd, err)
				}
				return
			}
			if coreUtil == "gzip" {
				if err.Error() != "gzip: ignoring stdout, use -f to compression" {
					t.Errorf("expected %q output, got: %q", cmd, err)
				}
				return
			}

			if !strings.Contains(err.Error(), "flag provided but not defined: -badoption") {
				t.Errorf("expected error for command %q, got: %v", cmd, err)
			}
		})
	}
}
