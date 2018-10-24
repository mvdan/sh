// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package shell

import (
	"context"
	"fmt"
	"io"
	"os"

	"mvdan.cc/sh/expand"
	"mvdan.cc/sh/interp"
	"mvdan.cc/sh/syntax"
)

// SourceFile sources a shell file from disk and returns the variables
// declared in it. It is a convenience function that uses a default shell
// parser, parses a file from disk, and calls SourceNode.
func SourceFile(ctx context.Context, path string) (map[string]expand.Variable, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("could not open: %v", err)
	}
	defer f.Close()
	p := syntax.NewParser()
	file, err := p.Parse(f, path)
	if err != nil {
		return nil, fmt.Errorf("could not parse: %v", err)
	}
	return SourceNode(ctx, file)
}

// purePrograms holds a list of common programs that do not have side
// effects, or otherwise cannot modify or harm the system that runs
// them.
var purePrograms = []string{
	// string handling
	"sed", "grep", "tr", "cut", "cat", "head", "tail", "seq", "yes",
	"wc",
	// paths
	"ls", "pwd", "basename", "realpath",
	// others
	"env", "sleep", "uniq", "sort",
}

func pureRunner() *interp.Runner {
	// forbid executing programs that might cause trouble
	exec := interp.ModuleExec(func(ctx context.Context, path string, args []string) error {
		for _, name := range purePrograms {
			if args[0] == name {
				return interp.DefaultExec(ctx, path, args)
			}
		}
		return fmt.Errorf("program not in whitelist: %s", args[0])
	})
	// forbid opening any real files
	open := interp.OpenDevImpls(func(ctx context.Context, path string, flags int, mode os.FileMode) (io.ReadWriteCloser, error) {
		mc, _ := interp.FromModuleContext(ctx)
		return nil, fmt.Errorf("cannot open path: %s", mc.UnixPath(path))
	})
	r, err := interp.New(interp.Module(exec), interp.Module(open))
	if err != nil {
		panic(err)
	}
	return r
}

// SourceNode sources a shell program from a node and returns the
// variables declared in it. It accepts the same set of node types that
// interp/Runner.Run does.
//
// Any side effects or modifications to the system are forbidden when
// interpreting the program. This is enforced via whitelists when
// executing programs and opening files.
func SourceNode(ctx context.Context, node syntax.Node) (map[string]expand.Variable, error) {
	r := pureRunner()
	if err := r.Run(ctx, node); err != nil {
		return nil, fmt.Errorf("could not run: %v", err)
	}
	// delete the internal shell vars that the user is not
	// interested in
	delete(r.Vars, "PWD")
	delete(r.Vars, "HOME")
	delete(r.Vars, "PATH")
	delete(r.Vars, "IFS")
	delete(r.Vars, "OPTIND")
	return r.Vars, nil
}
