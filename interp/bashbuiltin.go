// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

// Bash builtins that are not about running commands: enable, compgen, history
// and logout.

import (
	"context"
	"maps"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/expand"
)

// posixSpecialBuiltins are the ones POSIX calls special, listed by `enable -s`.
var posixSpecialBuiltins = []string{
	":", ".", "break", "continue", "eval", "exec", "exit", "export",
	"readonly", "return", "set", "shift", "source", "times", "trap", "unset",
}

// builtinDisabled reports whether `enable -n` turned a builtin off, in which
// case it is looked up as an external command instead.
func (r *Runner) builtinDisabled(name string) bool {
	return r.disabledBuiltins[name]
}

// runEnable implements the enable builtin. Disabling a builtin makes the shell
// fall through to the exec handler for that name, which is the point of the
// builtin in bash too — running the real /bin/echo, say.
func (r *Runner) runEnable(args []string) exitStatus {
	var disable, printAll, reusable, specialOnly bool
	rest := args
	for len(rest) > 0 && strings.HasPrefix(rest[0], "-") && len(rest[0]) > 1 {
		flag := rest[0]
		if flag == "--" {
			rest = rest[1:]
			break
		}
		for _, c := range flag[1:] {
			switch c {
			case 'n':
				disable = true
			case 'a':
				printAll = true
			case 'p':
				reusable = true
			case 's':
				specialOnly = true
			case 'd', 'f':
				r.errf("enable: loadable builtins are not supported\n")
				return exitStatus{code: 2}
			default:
				r.errf("enable: -%c: invalid option\n", c)
				r.errf("enable: usage: %s\n", helpTable["enable"].synopsis)
				return exitStatus{code: 2}
			}
		}
		rest = rest[1:]
	}

	if len(rest) == 0 {
		names := slices.Sorted(maps.Keys(helpTable))
		if specialOnly {
			names = posixSpecialBuiltins
		}
		for _, name := range names {
			off := r.builtinDisabled(name)
			// Without -a or -p, bash lists only the enabled builtins.
			if off && !printAll && !reusable {
				continue
			}
			if off {
				r.outf("enable -n %s\n", name)
			} else {
				r.outf("enable %s\n", name)
			}
		}
		return exitStatus{}
	}

	exit := exitStatus{}
	for _, name := range rest {
		if !IsBuiltin(name) {
			r.errf("enable: %s: not a shell builtin\n", name)
			exit.code = 1
			continue
		}
		if !disable {
			delete(r.disabledBuiltins, name)
			continue
		}
		if r.disabledBuiltins == nil {
			r.disabledBuiltins = make(map[string]bool)
		}
		r.disabledBuiltins[name] = true
	}
	return exit
}

// runHistory implements the history builtin, over the list the embedder
// supplied with [History].
func (r *Runner) runHistory(args []string) exitStatus {
	clear := false
	rest := args
	for len(rest) > 0 && strings.HasPrefix(rest[0], "-") && len(rest[0]) > 1 {
		switch rest[0] {
		case "-c":
			clear = true
		case "--":
			rest = rest[1:]
			goto count
		default:
			r.errf("history: %s: invalid option\n", rest[0])
			r.errf("history: usage: %s\n", helpTable["history"].synopsis)
			return exitStatus{code: 2}
		}
		rest = rest[1:]
	}
count:

	if r.historyList == nil {
		r.errf("history: no history list is available in this shell\n")
		return exitStatus{code: 1}
	}
	if clear {
		if r.historyClear == nil {
			r.errf("history: the history list cannot be cleared in this shell\n")
			return exitStatus{code: 1}
		}
		r.historyClear()
		return exitStatus{}
	}

	lines := r.historyList()
	if len(rest) > 0 {
		n, err := strconv.Atoi(rest[0])
		if err != nil || n < 0 {
			r.errf("history: %s: numeric argument required\n", rest[0])
			return exitStatus{code: 1}
		}
		if n < len(lines) {
			lines = lines[len(lines)-n:]
		}
	}
	// bash numbers the whole list, so a trimmed listing keeps the original
	// numbers rather than restarting at one.
	first := len(r.historyList()) - len(lines) + 1
	for i, line := range lines {
		r.outf("%5d  %s\n", first+i, line)
	}
	return exitStatus{}
}

// compgenAction is one of compgen's word lists.
type compgenAction string

// runCompgen implements the compgen builtin, which is where a line editor
// above this interpreter gets its completions from: it is the only thing that
// knows the shell's aliases, functions, variables and $PATH.
func (r *Runner) runCompgen(ctx context.Context, args []string) exitStatus {
	var actions []compgenAction
	var wordlist, prefix, suffix string
	rest := args

	addFlag := func(c rune) bool {
		action, ok := map[rune]compgenAction{
			'a': "alias", 'b': "builtin", 'c': "command", 'd': "directory",
			'e': "export", 'f': "file", 'k': "keyword", 'v': "variable",
			'g': "group", 'j': "job", 's': "service", 'u': "user",
		}[c]
		if !ok {
			return false
		}
		actions = append(actions, action)
		return true
	}

	for len(rest) > 0 && strings.HasPrefix(rest[0], "-") && len(rest[0]) > 1 {
		flag := rest[0]
		if flag == "--" {
			rest = rest[1:]
			break
		}
		// The options that take a value are only recognized on their own.
		switch flag {
		case "-A", "-W", "-P", "-S":
			if len(rest) < 2 {
				r.errf("compgen: %s: option requires an argument\n", flag)
				return exitStatus{code: 2}
			}
			switch flag {
			case "-A":
				actions = append(actions, compgenAction(rest[1]))
			case "-W":
				wordlist = rest[1]
			case "-P":
				prefix = rest[1]
			case "-S":
				suffix = rest[1]
			}
			rest = rest[2:]
			continue
		case "-o", "-C", "-F", "-G", "-X":
			// Completion specifications and filters, which only mean
			// something inside a `complete` definition.
			if len(rest) < 2 {
				r.errf("compgen: %s: option requires an argument\n", flag)
				return exitStatus{code: 2}
			}
			rest = rest[2:]
			continue
		}
		for _, c := range flag[1:] {
			if !addFlag(c) {
				r.errf("compgen: -%c: invalid option\n", c)
				r.errf("compgen: usage: %s\n", helpTable["compgen"].synopsis)
				return exitStatus{code: 2}
			}
		}
		rest = rest[1:]
	}

	word := ""
	if len(rest) > 0 {
		word = rest[0]
	}

	var out []string
	if wordlist != "" {
		out = append(out, strings.Fields(wordlist)...)
	}
	for _, action := range actions {
		out = append(out, r.compgenWords(ctx, action, word)...)
	}

	// bash filters the candidates by the word as a prefix, except for
	// filenames, which are generated already anchored to it.
	matched := make([]string, 0, len(out))
	seen := make(map[string]bool, len(out))
	for _, cand := range out {
		if !strings.HasPrefix(cand, word) || seen[cand] {
			continue
		}
		seen[cand] = true
		matched = append(matched, cand)
	}
	if len(matched) == 0 {
		return exitStatus{code: 1}
	}
	for _, cand := range matched {
		r.outf("%s%s%s\n", prefix, cand, suffix)
	}
	return exitStatus{}
}

// compgenWords generates the candidates for one action. Actions naming things
// this shell has no notion of — users, groups, services — yield nothing rather
// than an error, as they do in bash on a machine with none.
func (r *Runner) compgenWords(ctx context.Context, action compgenAction, word string) []string {
	var out []string
	switch action {
	case "alias":
		for name := range r.alias {
			out = append(out, name)
		}
	case "builtin":
		out = append(out, slices.Sorted(maps.Keys(helpTable))...)
	case "keyword":
		for _, kw := range shellKeywords {
			out = append(out, kw)
		}
	case "function":
		for name := range r.Funcs {
			out = append(out, name)
		}
	case "variable", "arrayvar":
		r.writeEnv.Each(func(name string, vr expand.Variable) bool {
			out = append(out, name)
			return true
		})
	case "export":
		r.writeEnv.Each(func(name string, vr expand.Variable) bool {
			if vr.Exported {
				out = append(out, name)
			}
			return true
		})
	case "command":
		out = append(out, r.compgenWords(ctx, "alias", word)...)
		out = append(out, r.compgenWords(ctx, "builtin", word)...)
		out = append(out, r.compgenWords(ctx, "function", word)...)
		out = append(out, r.commandsInPath(ctx)...)
	case "file", "directory":
		out = append(out, r.compgenPaths(ctx, word, action == "directory")...)
	case "group", "job", "service", "user", "hostname", "setopt", "shopt", "signal":
		// Nothing this shell has a notion of (or, for job and signal, nothing
		// until the job control builtins land); yield nothing rather than an
		// error, as bash does on a machine with none of them.
	}
	sort.Strings(out)
	return out
}

// commandsInPath lists the executables reachable through $PATH, using the
// interpreter's directory handler so it sees a virtual filesystem too.
func (r *Runner) commandsInPath(ctx context.Context) []string {
	var out []string
	for _, dir := range filepath.SplitList(r.envGet("PATH")) {
		if dir == "" {
			dir = "."
		}
		entries, err := r.readDirHandler(r.handlerCtx(ctx, handlerKindReadDir, todoPos), dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				out = append(out, entry.Name())
			}
		}
	}
	return out
}

// compgenPaths lists the filesystem entries the word could complete to, in the
// directory the word names.
func (r *Runner) compgenPaths(ctx context.Context, word string, dirsOnly bool) []string {
	dir, base := filepath.Split(word)
	lookup := dir
	if lookup == "" {
		lookup = r.Dir
	} else if !filepath.IsAbs(lookup) {
		lookup = filepath.Join(r.Dir, lookup)
	}
	entries, err := r.readDirHandler(r.handlerCtx(ctx, handlerKindReadDir, todoPos), lookup)
	if err != nil {
		return nil
	}
	var out []string
	for _, entry := range entries {
		if dirsOnly && !entry.IsDir() {
			continue
		}
		if !strings.HasPrefix(entry.Name(), base) {
			continue
		}
		out = append(out, dir+entry.Name())
	}
	return out
}

// shellKeywords is what `compgen -k` lists. It is bash's list rather than
// [syntax.IsKeyword]'s, which leaves out elif.
var shellKeywords = []string{
	"!", "[[", "]]", "case", "coproc", "do", "done", "elif", "else", "esac",
	"fi", "for", "function", "if", "in", "select", "then", "time", "until",
	"while", "{", "}",
}
