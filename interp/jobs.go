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

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

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

// status is the second column of the jobs listing, following bash: Done for a
// job that succeeded, "Exit N" for one that failed, Terminated for one kill
// cancelled.
func (bg bgProc) status() string {
	if bg.running() {
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

// jobIndexes returns the indexes of the jobs the builtins act on, oldest
// first. Disowned jobs and the shells behind process substitutions are not
// jobs as far as the user is concerned, so they are left out.
func (r *Runner) jobIndexes() []int {
	var out []int
	for i, bg := range r.bgProcs {
		if bg.disowned || bg.cmd == "" {
			continue
		}
		out = append(out, i)
	}
	return out
}

// currentJob and previousJob are bash's %+ and %-. bash tracks these as the
// two most recently *stopped* jobs, falling back to the most recent running
// ones; with nothing ever stopped here, the two most recent jobs are all that
// distinction can mean.
func (r *Runner) currentJob() int {
	live := r.jobIndexes()
	if len(live) == 0 {
		return -1
	}
	return live[len(live)-1]
}

func (r *Runner) previousJob() int {
	live := r.jobIndexes()
	if len(live) < 2 {
		return r.currentJob()
	}
	return live[len(live)-2]
}

// jobMark is the +/- column bash prints after the job number.
func (r *Runner) jobMark(i int) string {
	switch i {
	case r.currentJob():
		return "+"
	case r.previousJob():
		return "-"
	}
	return " "
}

// jobSpec resolves a job specification to an index into bgProcs. It accepts
// bash's forms — %1, %+, %%, %-, %string and %?string — as well as the fake
// PIDs this interpreter reports in $!, which are the job number with a "g"
// prefix.
func (r *Runner) jobSpec(spec string) (int, error) {
	if spec == "" {
		return -1, fmt.Errorf("no such job")
	}
	byNumber := func(n int) (int, error) {
		if n <= 0 || n > len(r.bgProcs) {
			return -1, fmt.Errorf("no such job")
		}
		if bg := r.bgProcs[n-1]; bg.cmd == "" || bg.disowned {
			return -1, fmt.Errorf("no such job")
		}
		return n - 1, nil
	}
	if num, ok := strings.CutPrefix(spec, "g"); ok {
		if n, err := strconv.Atoi(num); err == nil {
			return byNumber(n)
		}
	}
	if n, err := strconv.Atoi(spec); err == nil {
		return byNumber(n)
	}
	rest, ok := strings.CutPrefix(spec, "%")
	if !ok {
		return -1, fmt.Errorf("no such job")
	}
	switch rest {
	case "%", "+":
		if i := r.currentJob(); i >= 0 {
			return i, nil
		}
		return -1, fmt.Errorf("no such job")
	case "-":
		if i := r.previousJob(); i >= 0 {
			return i, nil
		}
		return -1, fmt.Errorf("no such job")
	}
	if n, err := strconv.Atoi(rest); err == nil {
		return byNumber(n)
	}
	// %?string matches anywhere in the command, %string only at its start.
	substring := false
	if s, ok := strings.CutPrefix(rest, "?"); ok {
		substring, rest = true, s
	}
	match := -1
	for _, i := range r.jobIndexes() {
		cmd := r.bgProcs[i].cmd
		if (substring && strings.Contains(cmd, rest)) || (!substring && strings.HasPrefix(cmd, rest)) {
			if match >= 0 {
				return -1, fmt.Errorf("ambiguous job spec")
			}
			match = i
		}
	}
	if match < 0 {
		return -1, fmt.Errorf("no such job")
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

	indexes := r.jobIndexes()
	if len(rest) > 0 {
		indexes = nil
		for _, spec := range rest {
			i, err := r.jobSpec(spec)
			if err != nil {
				r.errf("jobs: %s: %v\n", spec, err)
				return exitStatus{code: 1}
			}
			indexes = append(indexes, i)
		}
	}

	for _, i := range indexes {
		bg := r.bgProcs[i]
		// Nothing is ever stopped without a controlling terminal.
		if stoppedOnly || (runningOnly && !bg.running()) {
			continue
		}
		if newOnly {
			if bg.running() || bg.notified {
				continue
			}
			r.bgProcs[i].notified = true
		}
		if pidsOnly {
			r.outf("g%d\n", i+1)
			continue
		}
		prefix := fmt.Sprintf("[%d]%s  ", i+1, r.jobMark(i))
		if long {
			prefix = fmt.Sprintf("[%d]%s g%-6d", i+1, r.jobMark(i), i+1)
		}
		r.outf("%s%-24s%s\n", prefix, bg.status(), bg.cmd)
	}
	return exitStatus{}
}

// signalNames is the standard signal table, used by kill and `kill -l`. The
// interpreter never delivers these to anything: they are here so that scripts
// written for a real shell name a signal and get the behaviour that signal
// would have had.
var signalNames = [...]string{
	1: "HUP", 2: "INT", 3: "QUIT", 4: "ILL", 5: "TRAP", 6: "ABRT", 7: "BUS",
	8: "FPE", 9: "KILL", 10: "USR1", 11: "SEGV", 12: "USR2", 13: "PIPE",
	14: "ALRM", 15: "TERM", 16: "STKFLT", 17: "CHLD", 18: "CONT", 19: "STOP",
	20: "TSTP", 21: "TTIN", 22: "TTOU", 23: "URG", 24: "XCPU", 25: "XFSZ",
	26: "VTALRM", 27: "PROF", 28: "WINCH", 29: "IO", 30: "PWR", 31: "SYS",
}

// signalByName resolves "TERM", "SIGTERM" or "15" to a number and canonical
// name.
func signalByName(spec string) (int, string, bool) {
	if n, err := strconv.Atoi(spec); err == nil {
		if n == 0 {
			return 0, "", true
		}
		if n > 0 && n < len(signalNames) && signalNames[n] != "" {
			return n, signalNames[n], true
		}
		return 0, "", false
	}
	name := strings.TrimPrefix(strings.ToUpper(spec), "SIG")
	for n, known := range signalNames {
		if known != "" && known == name {
			return n, known, true
		}
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

func effectOf(num int) signalEffect {
	switch num {
	case 0:
		return signalTests
	case 17, 18, 19, 20, 21, 22: // CHLD, CONT, STOP, TSTP, TTIN, TTOU
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
		i, err := r.jobSpec(spec)
		if err != nil {
			r.errf("kill: %s: %v\n", spec, err)
			exit.code = 1
			continue
		}
		switch effectOf(signum) {
		case signalTests:
			// Existence already confirmed by resolving the spec.
		case signalUnsupported:
			r.errf("kill: %s: no job control\n", spec)
			exit.code = 1
		default:
			if r.bgProcs[i].running() {
				r.bgProcs[i].signal = signame
				r.bgProcs[i].cancel()
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
	col := 0
	for num, name := range signalNames {
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

	var indexes []int
	switch {
	case len(rest) > 0:
		for _, spec := range rest {
			i, err := r.jobSpec(spec)
			if err != nil {
				r.errf("disown: %s: %v\n", spec, err)
				return exitStatus{code: 1}
			}
			indexes = append(indexes, i)
		}
	case all || runningOnly:
		indexes = r.jobIndexes()
	default:
		if i := r.currentJob(); i >= 0 {
			indexes = []int{i}
		} else {
			r.errf("disown: current: no such job\n")
			return exitStatus{code: 1}
		}
	}
	for _, i := range indexes {
		if runningOnly && !r.bgProcs[i].running() {
			continue
		}
		r.bgProcs[i].disowned = true
	}
	return exitStatus{}
}

// runFg implements the fg builtin. Without a controlling terminal there is no
// foreground to move a job to, so this waits for the job and reports its exit
// status, which is what a caller of fg is ultimately after.
func (r *Runner) runFg(ctx context.Context, args []string) exitStatus {
	spec := "%+"
	if len(args) > 0 {
		spec = args[0]
	}
	i, err := r.jobSpec(spec)
	if err != nil {
		r.errf("fg: %s: %v\n", spec, err)
		return exitStatus{code: 1}
	}
	bg := r.bgProcs[i]
	r.outf("%s\n", bg.cmd)
	select {
	case <-ctx.Done():
		return exitStatus{code: 130}
	case <-bg.done:
	}
	exit := *bg.exit
	exit.exiting = false
	return exit
}

// runBg implements the bg builtin. Jobs here always run in the background
// already, so this reports on the job rather than resuming it, and fails on a
// job that has finished the way bash fails on one it cannot continue.
func (r *Runner) runBg(args []string) exitStatus {
	specs := args
	if len(specs) == 0 {
		specs = []string{"%+"}
	}
	exit := exitStatus{}
	for _, spec := range specs {
		i, err := r.jobSpec(spec)
		if err != nil {
			r.errf("bg: %s: %v\n", spec, err)
			exit.code = 1
			continue
		}
		if !r.bgProcs[i].running() {
			r.errf("bg: job %d has terminated\n", i+1)
			exit.code = 1
			continue
		}
		r.outf("[%d]%s %s &\n", i+1, r.jobMark(i), r.bgProcs[i].cmd)
	}
	return exit
}
