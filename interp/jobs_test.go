package interp_test

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

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

// needsSubprocesses skips tests that background an external command. js/wasm
// has no subprocesses, so those commands fail to run rather than becoming
// jobs, and the job state under test never arises.
func needsSubprocesses(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "js" {
		t.Skip("js/wasm has no subprocesses")
	}
}

func TestJobsBuiltin(t *testing.T) {
	needsSubprocesses(t)
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
	// Skipped on Windows, where they are not supported.
	if runtime.GOOS != "windows" {
		if s := runSrc(t, "cat <(echo hi) >/dev/null; jobs"); strings.TrimSpace(s) != "" {
			t.Fatalf("jobs after process substitution = %q", s)
		}
	}
	if s := runSrc(t, "jobs -Z"); !strings.Contains(s, "invalid option") {
		t.Fatalf("jobs -Z = %q", s)
	}
}

func TestKillBuiltin(t *testing.T) {
	needsSubprocesses(t)
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
	needsSubprocesses(t)
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

// syncBuffer collects output from the shell and from its background jobs,
// which write concurrently.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) takeString() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.buf.String()
	b.buf.Reset()
	return s
}

func TestJobsOutliveTheirContext(t *testing.T) {
	needsSubprocesses(t)
	t.Parallel()
	var out syncBuffer
	r, err := interp.New(interp.StdIO(strings.NewReader(""), &out, &out))
	if err != nil {
		t.Fatal(err)
	}
	run := func(ctx context.Context, src string) {
		t.Helper()
		f, err := syntax.NewParser().Parse(strings.NewReader(src), "")
		if err != nil {
			t.Fatal(err)
		}
		_ = r.Run(ctx, f)
	}

	// An interactive shell cancels each line's context once the line is
	// done; the job it started must survive that.
	ctx, cancel := context.WithCancel(context.Background())
	run(ctx, "sleep 30 &")
	cancel()
	out.takeString()

	run(context.Background(), "jobs")
	if s := out.takeString(); !strings.Contains(s, "Running") {
		t.Fatalf("job did not outlive its context: %q", s)
	}

	// Waiting for one still gives up when the caller's context is cancelled,
	// rather than blocking on a job that no longer dies with it.
	done := make(chan struct{})
	go func() {
		defer close(done)
		waitCtx, waitCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer waitCancel()
		run(waitCtx, "wait")
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("wait did not return when its context was cancelled")
	}
	out.takeString()

	// StopJobs is how the embedder ends them when the shell goes away.
	r.StopJobs()
	run(context.Background(), "jobs")
	if s := out.takeString(); strings.Contains(s, "Running") {
		t.Fatalf("StopJobs left a job running: %q", s)
	}
}
