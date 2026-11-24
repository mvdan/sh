// Copyright (c) 2025, Andrey Nering <andrey@nering.com.br>
// See LICENSE for licensing information

package coreutils

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"

	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

func runScript(t *testing.T, dir, script string) (string, error) {
	t.Helper()
	var in bytes.Buffer
	var out strings.Builder
	r, err := interp.New(
		interp.Dir(dir),
		interp.StdIO(&in, &out, &out),
		interp.ExecHandlers(ExecHandler),
	)
	if err != nil {
		t.Fatalf("failed to create interpreter: %v", err)
	}
	program, err := syntax.NewParser().Parse(strings.NewReader(script), "")
	if err != nil {
		t.Fatalf("failed to parse command %q: %v", script, err)
	}
	err = r.Run(t.Context(), program)
	return out.String(), err
}

// utilities that don't treat a leading -- argument as an option error
var badOptionUnsupported = map[string]bool{
	"expr":   true,
	"time":   true,
	"unlink": true,
	"yes":    true,
}

func TestExecHandlerUsageError(t *testing.T) {
	t.Parallel()
	for coreUtil := range commandBuilders {
		if badOptionUnsupported[coreUtil] {
			continue
		}
		t.Run(coreUtil, func(t *testing.T) {
			t.Parallel()
			out, err := runScript(t, t.TempDir(), coreUtil+" --badoption")
			code, ok := interp.IsExitStatus(err)
			if !ok || code == 0 {
				t.Fatalf("expected non-zero exit status for command %q, got: %v", coreUtil, err)
			}
			if !strings.Contains(out, "Try '"+coreUtil+" --help'") {
				t.Errorf("expected usage hint for command %q, got: %q", coreUtil, out)
			}
		})
	}
}

func TestExecHandlerErrorNotFatal(t *testing.T) {
	t.Parallel()
	out, err := runScript(t, t.TempDir(), "rm does-not-exist || echo recovered")
	if err != nil {
		t.Fatalf("expected Run to succeed, got: %v", err)
	}
	// The error message printed by rm varies by platform; only check
	// that the script recovered after it.
	got := out
	if !strings.HasPrefix(got, "rm: ") || !strings.HasSuffix(got, "\nrecovered\n") {
		t.Fatalf("expected an rm error followed by %q, got: %q", "recovered", got)
	}
}

func TestExecHandlerNestedExec(t *testing.T) {
	t.Parallel()
	t.Run("coreutils", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		out, err := runScript(t, dir, "printf 'hello' > f; printf 'f\\n' | xargs cat")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "hello" {
			t.Errorf("expected nested utilities to run, got: %q", out)
		}
	})
	t.Run("environment", func(t *testing.T) {
		t.Parallel()
		out, err := runScript(t, t.TempDir(), "env -i FOO=bar printenv FOO")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "bar\n" {
			t.Errorf("expected nested env overrides, got: %q", out)
		}
	})
	t.Run("builtin shadow", func(t *testing.T) {
		t.Parallel()
		out, err := runScript(t, t.TempDir(), "printf 'hello\\n' | xargs")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "hello\n" {
			t.Errorf("expected xargs default utility, got: %q", out)
		}
	})
	t.Run("external chain overrides", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		var out strings.Builder
		var cmdArgs []string
		var hc interp.HandlerContext
		r, err := interp.New(
			interp.Dir(dir),
			interp.StdIO(bytes.NewBufferString("arg\n"), &out, &out),
			interp.ExecHandlers(ExecHandler, func(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
				return func(ctx context.Context, args []string) error {
					cmdArgs = slices.Clone(args)
					hc = interp.HandlerCtx(ctx)
					return nil
				}
			}),
		)
		if err != nil {
			t.Fatalf("failed to create interpreter: %v", err)
		}
		script := "env -i FOO=custom xargs /bin/echo"
		program, err := syntax.NewParser().Parse(strings.NewReader(script), "")
		if err != nil {
			t.Fatalf("failed to parse: %v", err)
		}
		if err := r.Run(t.Context(), program); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := reflect.DeepEqual(cmdArgs, []string{"/bin/echo", "arg"}); !got {
			t.Errorf("nested args: %q", cmdArgs)
		}
		if hc.Dir != dir {
			t.Errorf("nested dir: %q want %q", hc.Dir, dir)
		}
		if v := hc.Env.Get("FOO"); !v.Set || v.Str != "custom" {
			t.Errorf("nested FOO: %+v", v)
		}
		data, err := io.ReadAll(hc.Stdin)
		if err != nil || len(data) != 0 {
			t.Errorf("nested stdin should be empty, got %q err %v", data, err)
		}
	})
}
