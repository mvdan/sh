// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"context"
	"io"
)

// Ctxt is the type passed to all the module functions. It contains some
// of the current state of the Runner, as well as some fields necessary
// to implement some of the modules.
type Ctxt struct {
	Context context.Context
	Env     []string
	Dir     string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
}

// ModuleExec is the module responsible for executing a program. It is
// executed for all CallExpr nodes where the name is neither a declared
// function nor a builtin.
//
// Use a return error of type ExitCode to set the exit code. A nil error
// has the same effect as ExitCode(0). If the error is of any other
// type, the interpreter will come to a stop.
type ModuleExec func(ctx Ctxt, name string, args []string) error
