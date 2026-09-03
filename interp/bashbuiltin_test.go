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

func TestCompgenBuiltin(t *testing.T) {
	t.Parallel()
	if s := runScript(t, "compgen -b pw"); strings.TrimSpace(s) != "pwd" {
		t.Fatalf("compgen -b = %q", s)
	}
	if s := runScript(t, "compgen -k wh"); strings.TrimSpace(s) != "while" {
		t.Fatalf("compgen -k = %q", s)
	}
	if s := runScript(t, "compgen -W 'foo bar baz' b"); strings.TrimSpace(s) != "bar\nbaz" {
		t.Fatalf("compgen -W = %q", s)
	}
	if s := runScript(t, "compgen -P '<' -S '>' -W 'foo' f"); strings.TrimSpace(s) != "<foo>" {
		t.Fatalf("compgen prefix and suffix = %q", s)
	}
	if s := runScript(t, "myfunc() { :; }; compgen -A function myf"); strings.TrimSpace(s) != "myfunc" {
		t.Fatalf("compgen -A function = %q", s)
	}
	if s := runScript(t, "shopt -s expand_aliases; alias myalias=pwd; compgen -a myal"); strings.TrimSpace(s) != "myalias" {
		t.Fatalf("compgen -a = %q", s)
	}
	if s := runScript(t, "MYVAR=1; compgen -v MYVA"); strings.TrimSpace(s) != "MYVAR" {
		t.Fatalf("compgen -v = %q", s)
	}
	if s := runScript(t, "MYVAR=1; export MYEXP=1; compgen -e MY"); strings.TrimSpace(s) != "MYEXP" {
		t.Fatalf("compgen -e = %q", s)
	}
	// No match is an error status with no output, as in bash.
	if s := runScript(t, "compgen -b zzz; echo status=$?"); strings.TrimSpace(s) != "status=1" {
		t.Fatalf("compgen with no match = %q", s)
	}
	if s := runScript(t, "compgen -Z"); !strings.Contains(s, "invalid option") {
		t.Fatalf("compgen -Z = %q", s)
	}
}

func TestHistoryBuiltin(t *testing.T) {
	t.Parallel()
	// Without a line editor to supply one, there is no history list.
	if s := runScript(t, "history"); !strings.Contains(s, "no history list") {
		t.Fatalf("history without a list = %q", s)
	}

	lines := []string{"echo one", "echo two", "echo three"}
	run := func(src string) string {
		t.Helper()
		var out strings.Builder
		r, err := interp.New(
			interp.StdIO(strings.NewReader(""), &out, &out),
			interp.History(
				func() []string { return lines },
				func() { lines = nil },
			),
		)
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

	if s := run("history"); strings.TrimSpace(s) != "1  echo one\n    2  echo two\n    3  echo three" {
		t.Fatalf("history = %q", s)
	}
	// A trimmed listing keeps the original numbering.
	if s := run("history 2"); strings.TrimSpace(s) != "2  echo two\n    3  echo three" {
		t.Fatalf("history 2 = %q", s)
	}
	if s := run("history -c; history"); strings.TrimSpace(s) != "" {
		t.Fatalf("history -c = %q", s)
	}
}
