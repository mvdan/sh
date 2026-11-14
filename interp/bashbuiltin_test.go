package interp_test

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

// runScript runs src and returns its combined output. These builtins report
// state that varies (listings, completions), so the cases assert on
// substrings rather than exact output.
func runScript(t *testing.T, src string) string {
	t.Helper()
	var out strings.Builder
	r, err := interp.New(interp.StdIO(strings.NewReader(""), &out, &out))
	if err != nil {
		t.Fatal(err)
	}
	f, err := syntax.NewParser().Parse(strings.NewReader(src), "")
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Run(context.Background(), f)
	return out.String()
}

func TestEnableBuiltin(t *testing.T) {
	t.Parallel()
	if s := runScript(t, "enable"); !strings.Contains(s, "enable pwd\n") {
		t.Fatalf("enable = %q", s)
	}
	if s := runScript(t, "enable -s"); strings.Contains(s, "enable pwd\n") {
		t.Fatalf("enable -s listed a non-special builtin: %q", s)
	}
	// A disabled builtin is hidden from the plain listing, shown by -a, and
	// looked up as an external command instead — which for a builtin with no
	// binary behind it means it is simply not found.
	if s := runScript(t, "enable -n pwd; enable"); strings.Contains(s, "enable pwd\n") {
		t.Fatalf("disabled builtin still listed: %q", s)
	}
	if s := runScript(t, "enable -n pwd; enable -a"); !strings.Contains(s, "enable -n pwd\n") {
		t.Fatalf("enable -a = %q", s)
	}
	// Skipped on Windows, where `help` is a real external command (cmd.exe's
	// HELP), so falling through to it succeeds rather than failing to resolve.
	if runtime.GOOS != "windows" {
		if s := runScript(t, "enable -n help; help"); !strings.Contains(s, "not found") {
			t.Fatalf("disabled builtin still ran: %q", s)
		}
	}
	// `builtin` runs one regardless, as in bash.
	if s := runScript(t, "enable -n pwd; builtin pwd"); strings.Contains(s, "not found") {
		t.Fatalf("builtin did not bypass enable -n: %q", s)
	}
	// Re-enabling restores it.
	if s := runScript(t, "enable -n pwd; enable pwd; enable"); !strings.Contains(s, "enable pwd\n") {
		t.Fatalf("re-enable = %q", s)
	}
	if s := runScript(t, "enable nosuchbuiltin"); !strings.Contains(s, "not a shell builtin") {
		t.Fatalf("enable on a non-builtin = %q", s)
	}
}
