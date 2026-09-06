// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

// Job control: the jobs, kill, disown, fg and bg builtins.
//
// A background job in this interpreter is a goroutine running a subshell, not
// an operating system process, which shapes what these builtins can honestly
// do. Jobs can be listed, waited for and cancelled — kill terminates a job by
// cancelling its context, which is what every fatal signal would have amounted
// to here. There is no controlling terminal and no process group, so nothing
// is ever *stopped*: fg waits for a job rather than handing it the terminal,
// bg reports on a job that is already running, and SIGSTOP/SIGCONT are
// rejected rather than faked.
//
// This is also what lets the builtins work on js/wasm, where there are no
// processes and no signals to send.
//
// Two differences from bash are known and not addressed here:
//
// A subshell sees no jobs at all. bash clears the table in an explicit
// `( ... )` but keeps it in a pipeline element and in a command substitution,
// so `jobs | grep sleep` and `x=$(jobs)` list the parent's jobs there and list
// nothing here. Matching that means deciding which subshells inherit the
// table, which is a change to [Runner.subshell] rather than to job control.
//
// Nothing is reported without being asked. bash announces a job's death before
// the next prompt, which is where its `[2]  Terminated` lines come from; that
// belongs to an interactive loop, and `jobs -n` is the piece of it a shell
// built on this package can call for itself.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// StopJobs cancels every background job this runner started, disowned ones
// included, and waits for them to finish, giving up early if ctx is done
// first. An interactive runner's jobs outlive the command line that started
// them, so an embedder should call this when the shell itself goes away — the
// closest thing here to the SIGHUP bash sends its jobs on exit. The shells
// behind process substitutions are not jobs and cannot be cancelled here:
// they follow the context of the [Runner.Run] call that started them.
func (r *Runner) StopJobs(ctx context.Context) {
	for _, bg := range r.bgProcs {
		if bg.cancel != nil {
			bg.cancel()
		}
	}
	for _, bg := range r.bgProcs {
		if bg.cancel == nil {
			continue
		}
		select {
		case <-bg.done:
		case <-ctx.Done():
			return
		}
	}
}

// jobText renders a backgrounded statement the way jobs prints it.
func jobText(st *syntax.Stmt) string {
	var b strings.Builder
	printer := syntax.NewPrinter(syntax.SingleLine(true))
	if err := printer.Print(&b, st); err != nil {
		return "<job>"
	}
	return b.String()
}

// running reports whether a job has not finished yet.
func (bg bgProc) running() bool {
	select {
	case <-bg.done:
		return false
	default:
		return true
	}
}

// await blocks until the job finishes, reporting false if the caller's
// context was cancelled first. An interactive runner's jobs are detached from
// the caller's context, so waiting for one must watch the caller's separately
// or an interrupted shell would block on a job that will not stop.
func (bg bgProc) await(ctx context.Context) bool {
	select {
	case <-bg.done:
		return true
	case <-ctx.Done():
		return false
	}
}

// status is the second column of the jobs listing, following bash: Done for a
// job that succeeded, "Exit N" for one that failed, Terminated for one kill
// cancelled.
//
// running is passed in rather than read here so that one observation of the
// job describes the whole row. Reading it twice lets a job finish in between
// and be reported as Running by one half of the line and Done by the other.
func (bg bgProc) status(running bool) string {
	if running {
		return "Running"
	}
	if bg.signal != "" {
		return "Terminated"
	}
	if bg.exit.code != 0 {
		return fmt.Sprintf("Exit %d", bg.exit.code)
	}
	return "Done"
}

// finalExit is the status wait and fg report for a finished job. bash gives a
// job its signal killed 128 plus the signal number, and jobs already says
// Terminated for the same reason, so the two agree.
func (bg bgProc) finalExit() exitStatus {
	if bg.signal != "" {
		return exitStatus{code: uint8(128 + signalNum(bg.signal))}
	}
	exit := *bg.exit
	exit.exiting = false
	return exit
}

// jobList returns the jobs the builtins act on, oldest first. Disowned jobs,
// the shells behind process substitutions and jobs already reaped are not jobs
// as far as the user is concerned, so they are left out.
func (r *Runner) jobList() []*bgProc {
	var out []*bgProc
	for _, bg := range r.bgProcs {
		if bg.disowned || bg.substitution || bg.reaped {
			continue
		}
		out = append(out, bg)
	}
	return out
}

// currentJob and previousJob are bash's %+ and %-. bash tracks these as the
// two most recently *stopped* jobs, falling back to the most recent running
// ones; with nothing ever stopped here, the two most recent jobs are all that
// distinction can mean.
func (r *Runner) currentJob() *bgProc {
	live := r.jobList()
	if len(live) == 0 {
		return nil
	}
	return live[len(live)-1]
}

func (r *Runner) previousJob() *bgProc {
	live := r.jobList()
	if len(live) < 2 {
		return r.currentJob()
	}
	return live[len(live)-2]
}

// jobMark is the +/- column bash prints after the job number.
func (r *Runner) jobMark(bg *bgProc) string {
	switch bg {
	case r.currentJob():
		return "+"
	case r.previousJob():
		return "-"
	}
	return " "
}

// jobLine renders one row of the jobs listing the way bash does: the job
// number and mark, the status padded to a fixed width, then the command.
//
// bash writes the trailing `&` for a job that is still running in the
// background, and drops it once the job has finished.
func (r *Runner) jobLine(bg *bgProc, running, long bool) string {
	prefix := fmt.Sprintf("[%d]%s  ", bg.num, r.jobMark(bg))
	if long {
		prefix = fmt.Sprintf("[%d]%s %-6s", bg.num, r.jobMark(bg), r.bgProcID(bg))
	}
	cmd := bg.cmd
	if running {
		cmd += " &"
	}
	return fmt.Sprintf("%s%-27s%s", prefix, bg.status(running), cmd)
}

// jobSpec resolves a job specification to a job. It accepts bash's % forms —
// %1, %+, %%, %-, %string and %?string — as well as what $! expands to: the
// real PID when the background statement started exactly one external program,
// and a "gN" fake PID otherwise. A spec without a leading % is a PID, never a
// job number, matching bash.
//
// The error text omits the spec, which every caller prints for itself.
func (r *Runner) jobSpec(spec string) (*bgProc, error) {
	noSuchJob := func() (*bgProc, error) { return nil, fmt.Errorf("no such job") }
	if spec == "" {
		return noSuchJob()
	}
	rest, ok := strings.CutPrefix(spec, "%")
	if !ok {
		// Not a % form, so a PID: find the job by what $! reported, like
		// [Runner.lookupBgProc]. A PID names the job directly, so a disowned
		// job is still found — bash's kill also still reaches a disowned
		// job's process by PID.
		for _, bg := range slices.Backward(r.bgProcs) {
			if !bg.substitution && !bg.reaped && r.bgProcID(bg) == spec {
				return bg, nil
			}
		}
		return noSuchJob()
	}
	switch rest {
	case "%", "+":
		if bg := r.currentJob(); bg != nil {
			return bg, nil
		}
		return noSuchJob()
	case "-":
		if bg := r.previousJob(); bg != nil {
			return bg, nil
		}
		return noSuchJob()
	}
	if n, err := strconv.Atoi(rest); err == nil {
		if n <= 0 {
			return noSuchJob()
		}
		for _, bg := range r.jobList() {
			if bg.num == n {
				return bg, nil
			}
		}
		return noSuchJob()
	}
	// %?string matches anywhere in the command, %string only at its start.
	substring := false
	if s, ok := strings.CutPrefix(rest, "?"); ok {
		substring, rest = true, s
	}
	var match *bgProc
	for _, bg := range r.jobList() {
		if (substring && strings.Contains(bg.cmd, rest)) || (!substring && strings.HasPrefix(bg.cmd, rest)) {
			if match != nil {
				// bash names the string rather than the whole spec here, and
				// then reports the spec as not found as well.
				return nil, ambiguousJobSpec{str: rest}
			}
			match = bg
		}
	}
	if match == nil {
		return noSuchJob()
	}
	return match, nil
}

// runJobs implements the jobs builtin.
func (r *Runner) runJobs(args []string) exitStatus {
	var long, pidsOnly, newOnly, runningOnly, stoppedOnly bool
	rest := args
	for len(rest) > 0 && strings.HasPrefix(rest[0], "-") && len(rest[0]) > 1 {
		flag := rest[0]
		if flag == "--" {
			rest = rest[1:]
			break
		}
		for _, c := range flag[1:] {
			switch c {
			case 'l':
				long = true
			case 'p':
				pidsOnly = true
			case 'n':
				newOnly = true
			case 'r':
				runningOnly = true
			case 's':
				stoppedOnly = true
			default:
				r.errf("jobs: -%c: invalid option\n", c)
				r.errf("jobs: usage: %s\n", helpTable["jobs"].synopsis)
				return exitStatus{code: 2}
			}
		}
		rest = rest[1:]
	}

	jobs := r.jobList()
	if len(rest) > 0 {
		jobs = nil
		for _, spec := range rest {
			bg, err := r.jobSpec(spec)
			if err != nil {
				r.errJobSpec("jobs", spec, err)
				return exitStatus{code: 1}
			}
			jobs = append(jobs, bg)
		}
	}

	// Each job is inspected once: whether it is still running decides the
	// whole row and whether it is reaped, so that a job finishing mid-listing
	// cannot be reported as Running and reaped in the same breath.
	var reap []*bgProc
	for _, bg := range jobs {
		running := bg.running()
		// Nothing is ever stopped without a controlling terminal.
		if stoppedOnly || (runningOnly && !running) {
			continue
		}
		// -n lists only the jobs that finished since the last report, which
		// after reaping is every finished job still in the table.
		if newOnly && running {
			continue
		}
		if pidsOnly {
			r.outf("%s\n", r.bgProcID(bg))
		} else {
			r.outf("%s\n", r.jobLine(bg, running, long))
		}
		// bash drops a job from the table once it has reported it as
		// finished, which is why a second jobs prints nothing and why the
		// numbering starts again from one.
		if !running {
			reap = append(reap, bg)
		}
	}
	for _, bg := range reap {
		r.reapBgProc(bg)
	}
	return exitStatus{}
}

// signalByName resolves "TERM", "SIGTERM" or "15" to a number and canonical
// name.
//
// Both directions go through the platform on unix — see signalName in
// os_unix.go for why a table compiled into the shell is wrong there.
func signalByName(spec string) (int, string, bool) {
	if n, err := strconv.Atoi(spec); err == nil {
		if n == 0 {
			return 0, "", true
		}
		if name := signalName(n); name != "" {
			return n, name, true
		}
		return 0, "", false
	}
	name := strings.TrimPrefix(strings.ToUpper(spec), "SIG")
	if n := signalNum(name); n > 0 {
		return n, name, true
	}
	return 0, "", false
}

// signalEffect says what a signal does to a job here. Since a job is a
// goroutine, anything whose default action would end a process cancels it, and
// job control signals have no meaning without a terminal.
type signalEffect int

const (
	signalTerminates  signalEffect = iota
	signalTests                    // signal 0: only check that the job exists
	signalUnsupported              // stop and continue, which need job control
)

// effectOf classifies a signal by NAME and not by number.
//
// It used to switch on 17 through 22, which are CHLD through TTOU on Linux and
// something else everywhere else: on darwin 17 is STOP and 20 is CHLD, so the
// set was both wrong and differently wrong per platform. The names are the
// portable thing, so the number is resolved first and the answer keyed on that.
func effectOf(num int) signalEffect {
	if num == 0 {
		return signalTests
	}
	switch signalName(num) {
	case "CHLD", "CONT", "STOP", "TSTP", "TTIN", "TTOU":
		return signalUnsupported
	}
	return signalTerminates
}

// runKill implements the kill builtin.
func (r *Runner) runKill(args []string) exitStatus {
	signum, signame := 15, "TERM"
	rest := args
	for len(rest) > 0 {
		arg := rest[0]
		if arg == "--" {
			rest = rest[1:]
			break
		}
		if len(arg) < 2 || arg[0] != '-' {
			break
		}
		switch {
		case arg == "-l" || arg == "-L":
			return r.killList(rest[1:])
		case arg == "-s" || arg == "-n":
			if len(rest) < 2 {
				r.errf("kill: %s: option requires an argument\n", arg)
				return exitStatus{code: 2}
			}
			num, name, ok := signalByName(rest[1])
			if !ok {
				r.errf("kill: %s: invalid signal specification\n", rest[1])
				return exitStatus{code: 1}
			}
			signum, signame = num, name
			rest = rest[2:]
			continue
		default:
			num, name, ok := signalByName(arg[1:])
			if !ok {
				r.errf("kill: %s: invalid signal specification\n", arg[1:])
				return exitStatus{code: 1}
			}
			signum, signame = num, name
		}
		rest = rest[1:]
	}

	if len(rest) == 0 {
		r.errf("kill: usage: %s\n", helpTable["kill"].synopsis)
		return exitStatus{code: 2}
	}

	exit := exitStatus{}
	for _, spec := range rest {
		bg, err := r.jobSpec(spec)
		if err != nil {
			// Only this runner's own jobs can be signalled. A PID naming
			// anything else belongs to the host, and an embedded interpreter
			// has no business killing the application it runs inside or any
			// other process on the machine.
			//
			// TODO: signalling a process this runner did not start could be
			// allowed behind an opt-in option, along with the rest of #171.
			r.errJobSpec("kill", spec, err)
			exit.code = 1
			continue
		}
		switch effectOf(signum) {
		case signalTests:
			// Existence already confirmed by resolving the spec.
			//
			// Not "is it still running": bash accepts kill -0 on a job that
			// has finished but has not been reaped, and only reports "no such
			// job" once wait or jobs has reported it and dropped it from the
			// table. Testing running() here instead made `true & kill -0 $!`
			// a race on whether the job had finished yet.
		case signalUnsupported:
			// A job is a goroutine with no terminal behind it, so there is
			// nothing to stop or continue. Faking it would be worse than
			// refusing it.
			r.errf("kill: %s: no job control\n", spec)
			exit.code = 1
		default:
			if bg.running() {
				bg.signal = signame
				bg.cancel()
			}
		}
	}
	return exit
}

// killList implements `kill -l`, which either names one signal or lists them
// all in bash's five-per-row layout.
func (r *Runner) killList(args []string) exitStatus {
	if len(args) > 0 {
		exit := exitStatus{}
		for _, arg := range args {
			num, name, ok := signalByName(arg)
			if !ok || num == 0 {
				r.errf("kill: %s: invalid signal specification\n", arg)
				exit.code = 1
				continue
			}
			// `kill -l NUMBER` names the signal, `kill -l NAME` numbers it.
			if _, err := strconv.Atoi(arg); err == nil {
				r.outf("%s\n", name)
			} else {
				r.outf("%d\n", num)
			}
		}
		return exit
	}
	// Walked rather than ranged over a table, so the list is the platform's
	// own: darwin stops at 31, Linux carries realtime signals past it.
	col := 0
	for num := 1; num <= maxSignal; num++ {
		name := signalName(num)
		if name == "" {
			continue
		}
		r.outf("%2d) SIG%-9s", num, name)
		if col++; col%5 == 0 {
			r.out("\n")
		}
	}
	if col%5 != 0 {
		r.out("\n")
	}
	return exitStatus{}
}

// runDisown implements the disown builtin.
func (r *Runner) runDisown(args []string) exitStatus {
	var all, runningOnly bool
	rest := args
	for len(rest) > 0 && strings.HasPrefix(rest[0], "-") && len(rest[0]) > 1 {
		flag := rest[0]
		if flag == "--" {
			rest = rest[1:]
			break
		}
		for _, c := range flag[1:] {
			switch c {
			case 'a':
				all = true
			case 'r':
				runningOnly = true
			case 'h':
				// Marks a job to survive SIGHUP. Nothing sends one here, so
				// the job is simply left in the table, as bash leaves it.
			default:
				r.errf("disown: -%c: invalid option\n", c)
				r.errf("disown: usage: %s\n", helpTable["disown"].synopsis)
				return exitStatus{code: 2}
			}
		}
		rest = rest[1:]
	}

	var jobs []*bgProc
	switch {
	case len(rest) > 0:
		for _, spec := range rest {
			bg, err := r.jobSpec(spec)
			if err != nil {
				r.errJobSpec("disown", spec, err)
				return exitStatus{code: 1}
			}
			jobs = append(jobs, bg)
		}
	case all || runningOnly:
		jobs = r.jobList()
	default:
		if bg := r.currentJob(); bg != nil {
			jobs = []*bgProc{bg}
		} else {
			r.errf("disown: current: no such job\n")
			return exitStatus{code: 1}
		}
	}
	for _, bg := range jobs {
		if runningOnly && !bg.running() {
			continue
		}
		bg.disowned = true
	}
	return exitStatus{}
}

// runFg implements the fg builtin. Without a controlling terminal there is no
// foreground to move a job to, so this waits for the job and reports its exit
// status, which is what a caller of fg is ultimately after.
func (r *Runner) runFg(ctx context.Context, args []string) exitStatus {
	// bash names the defaulted spec "current" when it reports a failure.
	spec, shown := "%+", "current"
	if len(args) > 0 {
		spec, shown = args[0], args[0]
	}
	bg, err := r.jobSpec(spec)
	if err != nil {
		r.errJobSpec("fg", shown, err)
		return exitStatus{code: 1}
	}
	r.outf("%s\n", bg.cmd)
	select {
	case <-ctx.Done():
		// The rest of interp surfaces a cancelled context as a fatal error
		// rather than as a status, so that a caller can tell it apart from a
		// command that merely exited 130.
		var exit exitStatus
		exit.fatal(ctx.Err())
		return exit
	case <-bg.done:
	}
	exit := bg.finalExit()
	// Waiting for a job is reaping it, as it is for wait.
	r.reapBgProc(bg)
	return exit
}

// runBg implements the bg builtin. Jobs here always run in the background
// already, so this reports on the job rather than resuming it, and fails on a
// job that has finished the way bash fails on one it cannot continue.
func (r *Runner) runBg(args []string) exitStatus {
	specs := args
	// bash names the defaulted spec "current" when it reports a failure.
	shown := func(spec string) string { return spec }
	if len(specs) == 0 {
		specs = []string{"%+"}
		shown = func(string) string { return "current" }
	}
	exit := exitStatus{}
	for _, spec := range specs {
		bg, err := r.jobSpec(spec)
		if err != nil {
			r.errJobSpec("bg", shown(spec), err)
			exit.code = 1
			continue
		}
		if !bg.running() {
			r.errf("bg: job %d has terminated\n", bg.num)
			exit.code = 1
			continue
		}
		r.outf("[%d]%s %s &\n", bg.num, r.jobMark(bg), bg.cmd)
	}
	return exit
}

// ambiguousJobSpec is the failure of a %string spec that matched more than one
// job. bash reports it in two lines — naming the string, then the whole spec —
// so the callers have to tell it apart from a plain failure.
type ambiguousJobSpec struct{ str string }

func (e ambiguousJobSpec) Error() string { return "ambiguous job spec" }

// errJobSpec reports a job spec that did not resolve, the way bash's builtins
// do, with builtin naming the one that was called.
func (r *Runner) errJobSpec(builtin, spec string, err error) {
	var amb ambiguousJobSpec
	if errors.As(err, &amb) {
		r.errf("%s: %s: ambiguous job spec\n", builtin, amb.str)
		r.errf("%s: %s: no such job\n", builtin, spec)
		return
	}
	r.errf("%s: %s: %v\n", builtin, spec, err)
}
