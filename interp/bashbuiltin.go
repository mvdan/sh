// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

// Bash builtins that are not about running commands.

import (
	"maps"
	"slices"
	"strings"
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
