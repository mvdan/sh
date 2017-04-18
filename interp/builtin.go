// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"fmt"
	"os"
	"strconv"

	"github.com/mvdan/sh/syntax"
)

func (r *Runner) builtin(pos syntax.Pos, name string, args []string) bool {
	exit := 0
	switch name {
	case "true", ":":
	case "false":
		exit = 1
	case "exit":
		switch len(args) {
		case 0:
			r.lastExit()
		case 1:
			if n, err := strconv.Atoi(args[0]); err != nil {
				r.runErr(pos, "invalid exit code: %q", args[0])
			} else if n != 0 {
				r.err = ExitCode(n)
			}
		default:
			r.runErr(pos, "exit cannot take multiple arguments")
		}
	case "set":
		r.args = args
	case "shift":
		n := 1
		switch len(args) {
		case 0:
		case 1:
			if n2, err := strconv.Atoi(args[0]); err == nil {
				n = n2
				break
			}
			fallthrough
		default:
			r.errf("usage: shift [n]\n")
			exit = 2
		}
		if len(r.args) < n {
			n = len(r.args)
		}
		r.args = r.args[n:]
	case "unset":
		for _, arg := range args {
			r.delVar(arg)
		}
	case "echo":
		newline := true
	opts:
		for len(args) > 0 {
			switch args[0] {
			case "-n":
				newline = false
			case "-e", "-E":
				// TODO: what should be our default?
				// exactly what is the difference in
				// what we write?
			default:
				break opts
			}
			args = args[1:]
		}
		for i, arg := range args {
			if i > 0 {
				r.outf(" ")
			}
			r.outf("%s", arg)
		}
		if newline {
			r.outf("\n")
		}
	case "printf":
		if len(args) == 0 {
			r.errf("usage: printf format [arguments]\n")
			exit = 2
			break
		}
		var a []interface{}
		for _, arg := range args[1:] {
			a = append(a, arg)
		}
		r.outf(args[0], a...)
	case "break":
		if !r.inLoop {
			r.errf("break is only useful in a loop")
			break
		}
		switch len(args) {
		case 0:
			r.breakEnclosing = 1
		case 1:
			if n, err := strconv.Atoi(args[0]); err == nil {
				r.breakEnclosing = n
				break
			}
			fallthrough
		default:
			r.errf("usage: break [n]\n")
			exit = 2
		}
	case "continue":
		if !r.inLoop {
			r.errf("continue is only useful in a loop")
			break
		}
		switch len(args) {
		case 0:
			r.contnEnclosing = 1
		case 1:
			if n, err := strconv.Atoi(args[0]); err == nil {
				r.contnEnclosing = n
				break
			}
			fallthrough
		default:
			r.errf("usage: continue [n]\n")
			exit = 2
		}
	case "pwd":
		r.outf(r.getVar("PWD"))
	case "cd":
		if len(args) > 1 {
			r.errf("usage: cd [dir]\n")
			exit = 2
			break
		}
		var dir string
		if len(args) == 0 {
			dir = r.getVar("HOME")
		} else {
			dir = args[0]
		}
		if err := os.Chdir(dir); err != nil {
			exit = 1
		}
	case "trap", "type", "wait", "source", "command", "pushd", "popd",
		"builtin", "umask", "alias", "unalias", "fg", "bg", "getopts":
		panic(fmt.Sprintf("unhandled builtin: %s", name))
	default:
		return false
	}
	r.exit = exit
	return true
}
