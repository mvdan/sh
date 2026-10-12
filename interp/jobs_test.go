package interp_test

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

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

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// takeString drains the buffer, for tests that assert on one step at a time.
func (b *syncBuffer) takeString() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.buf.String()
	b.buf.Reset()
	return s
}

// runSrc runs src with the default exec handler, so background commands are
// real processes with real PIDs, and returns the combined output. The
// deterministic job control cases live in runTests; these tests cover jobs
// that are genuinely running concurrently, and each src must end all jobs it
// starts — `kill ...; wait` — so no processes outlive the test.
func runSrc(t *testing.T, src string) string {
	t.Helper()
	var out syncBuffer
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

// session runs several sources on one runner, so that a test can observe the
// job table between commands the way an interactive shell does.
type session struct {
	t   *testing.T
	r   *interp.Runner
	out *syncBuffer
}

func newSession(t *testing.T) *session {
	t.Helper()
	out := new(syncBuffer)
	r, err := interp.New(interp.StdIO(strings.NewReader(""), out, out))
	if err != nil {
		t.Fatal(err)
	}
	return &session{t: t, r: r, out: out}
}

// run executes src and returns only what it printed.
func (s *session) run(src string) string {
	s.t.Helper()
	before := len(s.out.String())
	f, err := syntax.NewParser().Parse(strings.NewReader(src), "")
	if err != nil {
		s.t.Fatal(err)
	}
	_ = s.r.Run(context.Background(), f)
	return s.out.String()[before:]
}

// jobsUntil runs the jobs builtin until its output contains want, giving the
// background goroutine time to finish without a fixed sleep. Note that jobs
// only reaps what it reports as finished, so polling it leaves a running job
// in the table.
func (s *session) jobsUntil(want string) string {
	s.t.Helper()
	var last string
	for range 400 {
		last = s.run("jobs")
		if strings.Contains(last, want) {
			return last
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.t.Fatalf("jobs never reported %q; last listing was %q", want, last)
	return ""
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
	// A running job reports Running, and -p prints the PIDs that $! also
	// reports — real ones, since each of these jobs is one external program.
	if s := runSrc(t, "sleep 30 & jobs; kill %1; wait"); !strings.Contains(s, "Running") {
		t.Fatalf("jobs while running = %q", s)
	}
	s := runSrc(t, "sleep 30 & jobs -p; kill %1; wait")
	if pid, err := strconv.Atoi(strings.TrimSpace(s)); err != nil || pid <= 0 {
		t.Fatalf("jobs -p = %q, want a real PID", s)
	}
	// The two most recent jobs are marked + and -.
	s = runSrc(t, "sleep 30 & sleep 30 & jobs; kill %1 %2; wait")
	if !strings.Contains(s, "[1]-") || !strings.Contains(s, "[2]+") {
		t.Fatalf("job marks = %q", s)
	}
}

func TestKillBuiltin(t *testing.T) {
	needsSubprocesses(t)
	t.Parallel()
	// Killing a job cancels it, which jobs then reports as Terminated.
	// Job specs: by job number, by the real PID $! reports, by command
	// prefix, and by substring.
	for _, spec := range []string{"%1", "$!", "%sleep", "%?leep"} {
		s := newSession(t)
		s.run("sleep 30 & kill " + spec)
		if got := s.jobsUntil("Terminated"); !strings.Contains(got, "sleep 30") {
			t.Fatalf("kill %s listed %q", spec, got)
		}
		// Reporting it reaped it, so there is nothing left to list.
		if got := s.run("jobs"); got != "" {
			t.Fatalf("kill %s left %q behind", spec, got)
		}
	}
	// A job that is not exactly one external program keeps its fake gN pid,
	// which resolves the same way.
	gn := newSession(t)
	gn.run("(sleep 30) & kill $!")
	gn.jobsUntil("Terminated")
	// A bare integer is a PID, not a job number: a PID belonging to no job
	// does not touch job 1.
	s := runSrc(t, "sleep 30 & kill 99999999; echo st=$?; jobs; kill %1; wait")
	if !strings.Contains(s, "st=1") || !strings.Contains(s, "Running") {
		t.Fatalf("kill on a non-job PID = %q", s)
	}
	// Signal 0 only tests that the job exists.
	if s := runSrc(t, "sleep 30 & kill -0 %1; echo status=$?; jobs; kill %1; wait"); !strings.Contains(s, "status=0") || !strings.Contains(s, "Running") {
		t.Fatalf("kill -0 = %q", s)
	}
	// Stopping and continuing need job control, which does not exist here.
	if s := runSrc(t, "sleep 30 & kill -STOP %1; kill %1; wait"); !strings.Contains(s, "no job control") {
		t.Fatalf("kill -STOP = %q", s)
	}
	// Both spellings of the signal reach the job.
	for _, flag := range []string{"-9", "-KILL", "-SIGKILL", "-s KILL", "-n 9"} {
		s := newSession(t)
		s.run("sleep 30 & kill " + flag + " %1")
		s.jobsUntil("Terminated")
	}
}

func TestKillNonJobPID(t *testing.T) {
	needsSubprocesses(t)
	t.Parallel()
	// A PID the shell did not start names a process it may not signal, so the
	// process is still running afterwards.
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		cmd.Process.Kill()
		<-done
	})
	s := runSrc(t, fmt.Sprintf("kill %d; echo st=$?", cmd.Process.Pid))
	if !strings.Contains(s, "no such job") || !strings.Contains(s, "st=1") {
		t.Fatalf("kill on a foreign PID = %q, want it refused", s)
	}
	select {
	case <-done:
		t.Fatal("the process did not survive kill")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestDisownFgBg(t *testing.T) {
	needsSubprocesses(t)
	t.Parallel()
	// A disowned job leaves the job table, and its % spec stops resolving,
	// but its PID still does — which is also how these tests end it.
	s := runSrc(t, "sleep 30 & p=$!; disown; jobs; kill $p; wait $p")
	if strings.TrimSpace(s) != "" {
		t.Fatalf("jobs after disown = %q", s)
	}
	s = runSrc(t, "sleep 30 & p=$!; disown %1; kill %1; kill $p; wait $p")
	if !strings.Contains(s, "no such job") {
		t.Fatalf("kill by job spec after disown = %q", s)
	}
	// bg reports on a job that is already running.
	if s := runSrc(t, "sleep 30 & bg; kill %1; wait"); !strings.Contains(s, "[1]+ sleep 30 &") {
		t.Fatalf("bg = %q", s)
	}
}

func TestKillOnlyOwnJobs(t *testing.T) {
	needsSubprocesses(t)
	t.Parallel()
	// A PID this shell did not start is not signalled, whatever it names. 0 and
	// -1 are the ones that matter, since kill(2) reads them as the caller's
	// process group and as every process the user may reach.
	for _, spec := range []string{"0", "-- -1", "1", "999999999"} {
		s := runSrc(t, "kill "+spec+"; echo st=$?; echo survived")
		if !strings.Contains(s, "no such job") || !strings.Contains(s, "st=1") ||
			!strings.Contains(s, "survived") {
			t.Fatalf("kill %s = %q, want it refused", spec, s)
		}
	}
}

func TestJobNumbersSkipProcSubsts(t *testing.T) {
	needsSubprocesses(t)
	t.Parallel()
	// A process substitution runs a shell of its own, but bash gives it no
	// job number, so the sleep below is %1 and not %2.
	s := runSrc(t, "cat <(echo hi) >/dev/null; sleep 30 & jobs; kill %1; wait")
	if !strings.Contains(s, "[1]") || strings.Contains(s, "[2]") {
		t.Fatalf("jobs after a process substitution = %q, want the sleep as [1]", s)
	}
	if strings.Contains(s, "no such job") {
		t.Fatalf("kill %%1 after a process substitution = %q", s)
	}
	// Disowning a job does not renumber the ones after it, as in bash.
	s = runSrc(t, "sleep 30 & sleep 30 & p=$!; disown %1; jobs; kill %2; wait $p")
	if !strings.Contains(s, "[2]") || strings.Contains(s, "no such job") {
		t.Fatalf("jobs after disown = %q, want the survivor still [2]", s)
	}
}

func TestJobsReaping(t *testing.T) {
	needsSubprocesses(t)
	t.Parallel()
	// A finished job is reported once and then gone, as in bash.
	s := newSession(t)
	s.run("sleep 0 &")
	if got := s.jobsUntil("Done"); !strings.Contains(got, "[1]") {
		t.Fatalf("first listing = %q", got)
	}
	if got := s.run("jobs"); got != "" {
		t.Fatalf("second listing = %q, want nothing", got)
	}
	// With the table empty the numbering starts again from one.
	if got := s.run("sleep 30 & jobs"); !strings.Contains(got, "[1]") {
		t.Fatalf("numbering after reaping = %q", got)
	}
	s.run("kill %1; wait")

	// bash gives a new job the number after the highest one in the table,
	// not the lowest free one: with job 1 reaped and job 2 still running,
	// the next job is 3.
	s = newSession(t)
	s.run("sleep 0 & sleep 30 &")
	s.jobsUntil("Done")
	if got := s.run("sleep 30 & jobs"); !strings.Contains(got, "[3]") {
		t.Fatalf("numbering beside a live job = %q", got)
	}
	s.run("kill %2 %3; wait")

	// A job still running is listed but not reaped, however often it is
	// listed. Reading whether it is running twice per row used to let one
	// finishing mid-listing be reported as Running and reaped in the same
	// breath, which lost it before anything said it had ended.
	s = newSession(t)
	s.run("sleep 30 &")
	for range 20 {
		if got := s.run("jobs"); !strings.Contains(got, "Running") {
			t.Fatalf("a running job went missing from the listing: %q", got)
		}
	}
	s.run("kill %1; wait")
}

func TestWaitJobSpec(t *testing.T) {
	needsSubprocesses(t)
	t.Parallel()
	// bash's wait takes a job specification as well as a PID.
	s := newSession(t)
	if got := s.run("sleep 0 & wait %1; echo st=$?"); got != "st=0\n" {
		t.Fatalf("wait %%1 = %q", got)
	}
	// Waiting for it reaped it, so it is gone.
	if got := s.run("jobs"); got != "" {
		t.Fatalf("listing after wait %%1 = %q", got)
	}
	if got := s.run("wait %9; echo st=$?"); got != "wait: %9: no such job\nst=127\n" {
		t.Fatalf("wait on a bad spec = %q", got)
	}
}

func TestWaitReportsTheSignal(t *testing.T) {
	needsSubprocesses(t)
	t.Parallel()
	// bash gives a job its signal killed 128 plus the signal number, which is
	// 143 for TERM and 137 for KILL. The jobs listing already says Terminated
	// for the same job, so the two now agree.
	s := runSrc(t, `sleep 30 & p=$!; kill "$p"; wait "$p"; echo st=$?`)
	if !strings.Contains(s, "st=143") {
		t.Errorf("wait after kill = %q, want st=143", s)
	}
	s = runSrc(t, "sleep 30 & kill -9 %1; fg; echo st=$?")
	if !strings.Contains(s, "st=137") {
		t.Errorf("fg after kill -9 = %q, want st=137", s)
	}
}

func TestJobsOutliveTheirContext(t *testing.T) {
	needsSubprocesses(t)
	t.Parallel()
	var out syncBuffer
	r, err := interp.New(interp.Interactive(true), interp.StdIO(strings.NewReader(""), &out, &out))
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

	// StopJobs is how the embedder ends them when the shell goes away.
	out.takeString()
	r.StopJobs(context.Background())
	run(context.Background(), "jobs")
	if s := out.takeString(); strings.Contains(s, "Running") {
		t.Fatalf("StopJobs left the job running: %q", s)
	}
}

func TestNonInteractiveJobsDieWithContext(t *testing.T) {
	needsSubprocesses(t)
	t.Parallel()
	// Without Interactive, jobs keep master's behavior: an embedder bounding
	// a script with a timeout reaps its background children on cancel.
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
	ctx, cancel := context.WithCancel(context.Background())
	run(ctx, "sleep 30 &")
	cancel()
	run(context.Background(), "wait; jobs")
	if s := out.String(); strings.Contains(s, "Running") {
		t.Fatalf("non-interactive job survived its context: %q", s)
	}
}

func TestStopJobsSkipsProcessSubstitutions(t *testing.T) {
	needsSubprocesses(t)
	if runtime.GOOS == "windows" {
		t.Skip("no process substitutions on windows")
	}
	t.Parallel()
	// The shells behind process substitutions are not jobs and have no
	// cancel func; StopJobs must not block on them.
	var out syncBuffer
	r, err := interp.New(interp.Interactive(true), interp.StdIO(strings.NewReader(""), &out, &out))
	if err != nil {
		t.Fatal(err)
	}
	f, err := syntax.NewParser().Parse(strings.NewReader("echo <(sleep 20) >/dev/null"), "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // ends the substitution's shell, which follows Run's context
	_ = r.Run(ctx, f)
	start := time.Now()
	r.StopJobs(context.Background())
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("StopJobs blocked on a process substitution for %v", d)
	}
}

func TestResetStopsJobs(t *testing.T) {
	needsSubprocesses(t)
	if runtime.GOOS == "windows" || runtime.GOOS == "plan9" {
		t.Skip("checking process liveness needs signals")
	}
	t.Parallel()
	// Reset drops the job table, so it must end the jobs first: a detached
	// job surviving it would be unreachable and unstoppable.
	var out syncBuffer
	r, err := interp.New(interp.Interactive(true), interp.StdIO(strings.NewReader(""), &out, &out))
	if err != nil {
		t.Fatal(err)
	}
	f, err := syntax.NewParser().Parse(strings.NewReader("sleep 30 & echo pid=$!"), "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	_ = r.Run(ctx, f)
	cancel()
	pid := strings.TrimSpace(strings.TrimPrefix(out.String(), "pid="))
	r.Reset()
	// Reset waited for the job, so its process is gone and reaped.
	if s := runSrc(t, "kill -0 "+pid+"; echo st=$?"); !strings.Contains(s, "st=1") {
		t.Fatalf("job survived Reset: kill -0 = %q", s)
	}
}
