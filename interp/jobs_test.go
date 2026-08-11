package interp_test

import (
	"context"
	"strings"
	"testing"

	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

// runSrc runs src and returns its combined output. Job control is awkward to
// express in runTests: these cases assert on substrings rather than exact
// output, since job state and the order two background shells finish in are
// not fixed.
func runSrc(t *testing.T, src string) string {
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

func TestJobsBuiltin(t *testing.T) {
	t.Parallel()
	// A finished job reports Done, and a failed one its exit status.
	if s := runSrc(t, "sleep 0 & wait; jobs"); !strings.Contains(s, "[1]+  Done") || !strings.Contains(s, "sleep 0") {
		t.Fatalf("jobs after wait = %q", s)
	}
	if s := runSrc(t, "(exit 3) & wait; jobs"); !strings.Contains(s, "Exit 3") {
		t.Fatalf("jobs after failure = %q", s)
	}
	// A running job reports Running, and -p prints only the fake pids that
	// $! also reports.
	if s := runSrc(t, "sleep 30 & jobs; kill %1"); !strings.Contains(s, "Running") {
		t.Fatalf("jobs while running = %q", s)
	}
	if s := runSrc(t, "sleep 30 & jobs -p; kill %1"); strings.TrimSpace(s) != "g1" {
		t.Fatalf("jobs -p = %q", s)
	}
	// The two most recent jobs are marked + and -.
	s := runSrc(t, "sleep 30 & sleep 30 & jobs; kill %1 %2")
	if !strings.Contains(s, "[1]-") || !strings.Contains(s, "[2]+") {
		t.Fatalf("job marks = %q", s)
	}
	// Process substitutions run a background shell, but are not jobs.
	if s := runSrc(t, "cat <(echo hi) >/dev/null; jobs"); strings.TrimSpace(s) != "" {
		t.Fatalf("jobs after process substitution = %q", s)
	}
	if s := runSrc(t, "jobs -Z"); !strings.Contains(s, "invalid option") {
		t.Fatalf("jobs -Z = %q", s)
	}
}

func TestKillBuiltin(t *testing.T) {
	t.Parallel()
	// Killing a job cancels it, which jobs then reports as Terminated.
	s := runSrc(t, "sleep 30 & kill %1; wait; jobs")
	if !strings.Contains(s, "Terminated") {
		t.Fatalf("jobs after kill = %q", s)
	}
	// Job specs: by number, by fake pid, by command prefix, and by substring.
	for _, spec := range []string{"%1", "1", "g1", "%sleep", "%?leep"} {
		s := runSrc(t, "sleep 30 & kill "+spec+"; wait; jobs")
		if !strings.Contains(s, "Terminated") {
			t.Fatalf("kill %s = %q", spec, s)
		}
	}
	// Signal 0 only tests that the job exists.
	if s := runSrc(t, "sleep 30 & kill -0 %1; echo status=$?; jobs; kill %1"); !strings.Contains(s, "status=0") || !strings.Contains(s, "Running") {
		t.Fatalf("kill -0 = %q", s)
	}
	if s := runSrc(t, "kill %9"); !strings.Contains(s, "no such job") {
		t.Fatalf("kill on missing job = %q", s)
	}
	// Stopping and continuing need job control, which does not exist here.
	if s := runSrc(t, "sleep 30 & kill -STOP %1; kill %1"); !strings.Contains(s, "no job control") {
		t.Fatalf("kill -STOP = %q", s)
	}
	if s := runSrc(t, "kill -NOPE %1"); !strings.Contains(s, "invalid signal specification") {
		t.Fatalf("kill -NOPE = %q", s)
	}
	// kill -l names a number, numbers a name, and lists them all.
	if s := runSrc(t, "kill -l 9"); strings.TrimSpace(s) != "KILL" {
		t.Fatalf("kill -l 9 = %q", s)
	}
	if s := runSrc(t, "kill -l TERM"); strings.TrimSpace(s) != "15" {
		t.Fatalf("kill -l TERM = %q", s)
	}
	if s := runSrc(t, "kill -l"); !strings.Contains(s, "SIGHUP") || !strings.Contains(s, "SIGTERM") {
		t.Fatalf("kill -l = %q", s)
	}
	// Both spellings of the signal reach the job.
	for _, flag := range []string{"-9", "-KILL", "-SIGKILL", "-s KILL", "-n 9"} {
		s := runSrc(t, "sleep 30 & kill "+flag+" %1; wait; jobs")
		if !strings.Contains(s, "Terminated") {
			t.Fatalf("kill %s = %q", flag, s)
		}
	}
}

func TestDisownFgBg(t *testing.T) {
	t.Parallel()
	// A disowned job leaves the job table, and a bare wait no longer waits
	// for it.
	if s := runSrc(t, "sleep 30 & disown; jobs"); strings.TrimSpace(s) != "" {
		t.Fatalf("jobs after disown = %q", s)
	}
	if s := runSrc(t, "sleep 30 & disown %1; kill %1"); !strings.Contains(s, "no such job") {
		t.Fatalf("kill after disown = %q", s)
	}
	// fg waits for the job and hands back its exit status.
	if s := runSrc(t, "(exit 4) & fg; echo status=$?"); !strings.Contains(s, "status=4") {
		t.Fatalf("fg = %q", s)
	}
	// bg reports on a job that is already running, and fails on one that
	// has finished.
	if s := runSrc(t, "sleep 30 & bg; kill %1"); !strings.Contains(s, "[1]+ sleep 30 &") {
		t.Fatalf("bg = %q", s)
	}
	if s := runSrc(t, "sleep 0 & wait; bg"); !strings.Contains(s, "has terminated") {
		t.Fatalf("bg on finished job = %q", s)
	}
}

func TestEnableBuiltin(t *testing.T) {
	t.Parallel()
	if s := runSrc(t, "enable"); !strings.Contains(s, "enable pwd\n") {
		t.Fatalf("enable = %q", s)
	}
	if s := runSrc(t, "enable -s"); strings.Contains(s, "enable pwd\n") {
		t.Fatalf("enable -s listed a non-special builtin: %q", s)
	}
	// A disabled builtin is hidden from the plain listing, shown by -a, and
	// looked up as an external command instead — which for a builtin with no
	// binary behind it means it is simply not found.
	if s := runSrc(t, "enable -n pwd; enable"); strings.Contains(s, "enable pwd\n") {
		t.Fatalf("disabled builtin still listed: %q", s)
	}
	if s := runSrc(t, "enable -n pwd; enable -a"); !strings.Contains(s, "enable -n pwd\n") {
		t.Fatalf("enable -a = %q", s)
	}
	if s := runSrc(t, "enable -n help; help"); !strings.Contains(s, "not found") {
		t.Fatalf("disabled builtin still ran: %q", s)
	}
	// `builtin` runs one regardless, as in bash.
	if s := runSrc(t, "enable -n pwd; builtin pwd"); strings.Contains(s, "not found") {
		t.Fatalf("builtin did not bypass enable -n: %q", s)
	}
	// Re-enabling restores it.
	if s := runSrc(t, "enable -n pwd; enable pwd; enable"); !strings.Contains(s, "enable pwd\n") {
		t.Fatalf("re-enable = %q", s)
	}
	if s := runSrc(t, "enable nosuchbuiltin"); !strings.Contains(s, "not a shell builtin") {
		t.Fatalf("enable on a non-builtin = %q", s)
	}
}

func TestCompgenBuiltin(t *testing.T) {
	t.Parallel()
	if s := runSrc(t, "compgen -b pw"); strings.TrimSpace(s) != "pwd" {
		t.Fatalf("compgen -b = %q", s)
	}
	if s := runSrc(t, "compgen -k wh"); strings.TrimSpace(s) != "while" {
		t.Fatalf("compgen -k = %q", s)
	}
	if s := runSrc(t, "compgen -W 'foo bar baz' b"); strings.TrimSpace(s) != "bar\nbaz" {
		t.Fatalf("compgen -W = %q", s)
	}
	if s := runSrc(t, "compgen -P '<' -S '>' -W 'foo' f"); strings.TrimSpace(s) != "<foo>" {
		t.Fatalf("compgen prefix and suffix = %q", s)
	}
	if s := runSrc(t, "myfunc() { :; }; compgen -A function myf"); strings.TrimSpace(s) != "myfunc" {
		t.Fatalf("compgen -A function = %q", s)
	}
	if s := runSrc(t, "shopt -s expand_aliases; alias myalias=pwd; compgen -a myal"); strings.TrimSpace(s) != "myalias" {
		t.Fatalf("compgen -a = %q", s)
	}
	if s := runSrc(t, "MYVAR=1; compgen -v MYVA"); strings.TrimSpace(s) != "MYVAR" {
		t.Fatalf("compgen -v = %q", s)
	}
	if s := runSrc(t, "MYVAR=1; export MYEXP=1; compgen -e MY"); strings.TrimSpace(s) != "MYEXP" {
		t.Fatalf("compgen -e = %q", s)
	}
	// No match is an error status with no output, as in bash.
	if s := runSrc(t, "compgen -b zzz; echo status=$?"); strings.TrimSpace(s) != "status=1" {
		t.Fatalf("compgen with no match = %q", s)
	}
	if s := runSrc(t, "compgen -Z"); !strings.Contains(s, "invalid option") {
		t.Fatalf("compgen -Z = %q", s)
	}
}

func TestHistoryBuiltin(t *testing.T) {
	t.Parallel()
	// Without a line editor to supply one, there is no history list.
	if s := runSrc(t, "history"); !strings.Contains(s, "no history list") {
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
