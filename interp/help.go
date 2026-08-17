// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"maps"
	"path"
	"slices"
	"strings"
)

// The `help` builtin, following bash: `help` lists every builtin's synopsis in
// two columns, `help NAME` prints the synopsis and description of one, and
// NAME may be a glob (`help ex*`). -d, -s and -m select short, synopsis-only
// and man-page-ish output.
//
// A builtin the interpreter recognizes but does not implement is marked with a
// star, the way bash marks disabled builtins — so `help` doubles as an honest
// statement of what this shell can do.

// builtinHelp is the synopsis and description of one builtin.
type builtinHelp struct {
	synopsis string
	desc     string
}

// helpTable is keyed by builtin name. Synopses follow bash 5's wording so the
// output is familiar; descriptions are ours, and say where this shell differs.
var helpTable = map[string]builtinHelp{
	":":       {": ", "Null command; expands arguments and returns success."},
	".":       {". filename [arguments]", "Execute commands from a file in the current shell."},
	"[":       {"[ arg... ]", "Evaluate a conditional expression; a synonym for `test' requiring a closing `]'."},
	"alias":   {"alias [-p] [name[=value] ... ]", "Define or display aliases. With no arguments, print the alias list."},
	"bg":      {"bg [job_spec ...]", "Resume jobs in the background."},
	"bind":    {"bind [-lpsvPSVX] [-m keymap] [keyseq:readline-function]", "Set readline key bindings and variables."},
	"break":   {"break [n]", "Exit the enclosing for, while or until loop; with n, exit n levels."},
	"builtin": {"builtin [shell-builtin [arg ...]]", "Run a shell builtin, bypassing a function of the same name."},
	"caller":  {"caller [expr]", "Return the context of the current subroutine call."},
	"cd":      {"cd [-L|[-P [-e]]] [dir]", "Change the shell working directory."},
	"command": {"command [-pVv] command [arg ...]", "Run a command, bypassing shell function lookup."},
	"compgen": {"compgen [-abcdefgjksuv] [-o option] [word]", "Display possible completions for a word."},
	"complete": {"complete [-abcdefgjksuv] [-pr] [-o option] [name ...]",
		"Specify how arguments are to be completed by readline."},
	"compopt":  {"compopt [-o|+o option] [name ...]", "Modify or display completion options."},
	"continue": {"continue [n]", "Resume the next iteration of the enclosing loop; with n, of the nth enclosing loop."},
	"declare": {"declare [-aAfFgiIlnrtux] [name[=value] ...]",
		"Set variable values and attributes, or display them."},
	"dirs":   {"dirs [-clpv] [+N] [-N]", "Display the list of currently remembered directories."},
	"disown": {"disown [-h] [-ar] [jobspec ...]", "Remove jobs from the current shell's job table."},
	"echo":   {"echo [-neE] [arg ...]", "Write arguments to standard output, separated by spaces."},
	"enable": {"enable [-a] [-dnps] [name ...]", "Enable and disable shell builtins."},
	"eval":   {"eval [arg ...]", "Concatenate arguments into a single command and execute it."},
	"exec":   {"exec [-cl] [-a name] [command [argument ...]]", "Replace the shell with the given command."},
	"exit":   {"exit [n]", "Exit the shell with status n; without n, the status of the last command."},
	"export": {"export [-fn] [name[=value] ...] or export -p", "Mark names for automatic export to the environment."},
	"false":  {"false", "Return an unsuccessful result."},
	"fc":     {"fc [-e ename] [-lnr] [first] [last]", "Display or re-execute commands from the history list."},
	"fg":     {"fg [job_spec]", "Move a job to the foreground."},
	"for":    {"for NAME [in WORDS ... ] ; do COMMANDS; done", "Execute commands for each member in a list."},
	"getopts": {"getopts optstring name [arg ...]",
		"Parse option arguments, one at a time, as a shell script would."},
	"hash":    {"hash [-lr] [-p pathname] [-dt] [name ...]", "Remember or display program locations."},
	"help":    {"help [-dms] [pattern ...]", "Display information about builtin commands."},
	"history": {"history [-c] [n]", "Display the history list."},
	"jobs":    {"jobs [-lnprs] [jobspec ...]", "Display status of jobs."},
	"kill":    {"kill [-s sigspec | -n signum] pid | jobspec ...", "Send a signal to a job or process."},
	"let":     {"let arg [arg ...]", "Evaluate arithmetic expressions."},
	"local":   {"local [option] name[=value] ...", "Define local variables inside a shell function."},
	"logout":  {"logout [n]", "Exit a login shell."},
	"mapfile": {"mapfile [-d delim] [-n count] [-O origin] [-s count] [-t] [array]",
		"Read lines from the standard input into an indexed array variable."},
	"newgrp": {"newgrp [group]", "Change the current group ID."},
	"popd":   {"popd [-n] [+N | -N]", "Remove directories from the stack."},
	"printf": {"printf [-v var] format [arguments]", "Format and print arguments under control of the format."},
	"pushd":  {"pushd [-n] [+N | -N | dir]", "Add directories to the stack and change to the top."},
	"pwd":    {"pwd [-LP]", "Print the name of the current working directory."},
	"read":   {"read [-Eers] [-a array] [-d delim] [-n nchars] [-p prompt] [-t timeout] [name ...]", "Read a line from the standard input and split it into fields."},
	"readarray": {"readarray [-d delim] [-n count] [-O origin] [-s count] [-t] [array]",
		"Read lines from a file into an indexed array variable; a synonym for `mapfile'."},
	"readonly": {"readonly [-aAf] [name[=value] ...] or readonly -p", "Mark names as unchangeable."},
	"return":   {"return [n]", "Return from a shell function with status n."},
	"set":      {"set [-abefhkmnptuvxBCEHPT] [-o option-name] [--] [arg ...]", "Set or unset shell options and positional parameters."},
	"shift":    {"shift [n]", "Shift positional parameters left by n (default 1)."},
	"shopt":    {"shopt [-pqsu] [-o] [optname ...]", "Set and unset shell options."},
	"source":   {"source filename [arguments]", "Execute commands from a file in the current shell; a synonym for `.'."},
	"suspend":  {"suspend [-f]", "Suspend shell execution."},
	"test":     {"test [expr]", "Evaluate a conditional expression and return 0 or 1."},
	"times":    {"times", "Display process times for the shell and its children."},
	"trap":     {"trap [-lp] [[action] signal_spec ...]", "Trap signals and other events."},
	"true":     {"true", "Return a successful result."},
	"type":     {"type [-afptP] name [name ...]", "Display information about the type of each name."},
	"typeset": {"typeset [-aAfFgiIlnrtux] name[=value] ...",
		"Set variable values and attributes; a synonym for `declare'."},
	"ulimit":  {"ulimit [-SHabcdefiklmnpqrstuvxPRT] [limit]", "Modify or display shell resource limits."},
	"umask":   {"umask [-p] [-S] [mode]", "Display or set the file mode creation mask."},
	"unalias": {"unalias [-a] name [name ...]", "Remove each name from the list of aliases."},
	"unset":   {"unset [-f] [-v] [-n] [name ...]", "Unset values and attributes of shell variables and functions."},
	"wait":    {"wait [-fn] [id ...]", "Wait for job completion and return exit status."},
}

// helpUnsupported are recognized by this shell but not implemented; `help`
// stars them the way bash stars disabled builtins.
var helpUnsupported = map[string]bool{
	"bg": true, "bind": true, "caller": true, "compgen": true, "complete": true,
	"compopt": true, "disown": true, "enable": true, "fc": true, "fg": true,
	"history": true, "jobs": true, "kill": true, "logout": true, "newgrp": true,
	"suspend": true, "ulimit": true,
}

// helpMatch returns the documented names matching a bash-style pattern. An
// exact name wins; otherwise the pattern is matched as a glob, and as a
// prefix, like bash's `help ex*` and `help ex`.
func helpMatch(pattern string) []string {
	if _, ok := helpTable[pattern]; ok {
		return []string{pattern}
	}
	var out []string
	for _, name := range slices.Sorted(maps.Keys(helpTable)) {
		if ok, err := path.Match(pattern, name); err == nil && ok {
			out = append(out, name)
			continue
		}
		if strings.HasPrefix(name, pattern) {
			out = append(out, name)
		}
	}
	return out
}

// helpLine is the two-column list entry for a builtin.
func helpLine(name string) string {
	star := " "
	if helpUnsupported[name] {
		star = "*"
	}
	return star + helpTable[name].synopsis
}

// runHelp implements the help builtin. It writes through the runner so it
// honours redirections like every other builtin.
func (r *Runner) runHelp(args []string) exitStatus {
	var short, synopsisOnly, manStyle bool
	rest := args
	for len(rest) > 0 && strings.HasPrefix(rest[0], "-") && rest[0] != "-" {
		switch rest[0] {
		case "-d":
			short = true
		case "-s":
			synopsisOnly = true
		case "-m":
			manStyle = true
		case "--":
			rest = rest[1:]
			goto patterns
		default:
			r.errf("help: %s: invalid option\n", rest[0])
			r.errf("help: usage: %s\n", helpTable["help"].synopsis)
			return exitStatus{code: 2}
		}
		rest = rest[1:]
	}
patterns:

	if len(rest) == 0 {
		r.out("A shell built on mvdan.cc/sh/v3. These shell commands are defined internally.\n")
		r.out("Type `help' to see this list.\n")
		r.out("Type `help name' to find out more about the function `name'.\n\n")
		r.out("A star (*) next to a name means that the command is not implemented.\n\n")

		names := slices.Sorted(maps.Keys(helpTable))
		// two columns, like bash: first half down the left, second half right
		half := (len(names) + 1) / 2
		for i := 0; i < half; i++ {
			left := helpLine(names[i])
			if i+half < len(names) {
				r.outf("%-78s%s\n", left, helpLine(names[i+half]))
			} else {
				r.outf("%s\n", left)
			}
		}
		return exitStatus{}
	}

	exit := exitStatus{}
	for _, pattern := range rest {
		matches := helpMatch(pattern)
		if len(matches) == 0 {
			r.errf("help: no help topics match `%s'.  Try `help help'.\n", pattern)
			exit = exitStatus{code: 1}
			continue
		}
		for _, name := range matches {
			h := helpTable[name]
			switch {
			case synopsisOnly:
				r.outf("%s: %s\n", name, h.synopsis)
			case short:
				r.outf("%s - %s\n", name, h.desc)
			case manStyle:
				r.outf("NAME\n    %s - %s\n\nSYNOPSIS\n    %s\n\nDESCRIPTION\n    %s\n\n",
					name, h.desc, h.synopsis, h.desc)
			default:
				r.outf("%s: %s\n", name, h.synopsis)
				r.outf("    %s\n", h.desc)
				if helpUnsupported[name] {
					r.out("    (recognized by this shell but not implemented)\n")
				}
				r.out("\n")
			}
		}
	}
	return exit
}
