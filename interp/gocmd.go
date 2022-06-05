package interp

import (
	"context"
	"io"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

// DeclareGoCommand registers a Go function as if it was a bash command.
func (r *Runner) DeclareGoCommand(name string, cmd GoCmd) {
	// FIXME: looks like r.Funcs isn't always allocated
	if r.Funcs == nil {
		r.Funcs = make(map[string]*syntax.Stmt)
	}
	r.Funcs[name] = &syntax.Stmt{Cmd: goCmdExpr{cmd}}
}

// GoCmd represents a go function that can be called by the interpreter
//
// FIXME: do we really want to expose local variables in the env?
// FIXME: what about other file descriptors, wouldn't it be best to pass a
//        list of fds?
type GoCmd func(ctx context.Context, args []string, env expand.Environ, cwd string, stdin io.Reader, stdout, stderr io.Writer) (exit uint8)

// goCmdExpr extends the syntax.Command with a custom node
//
// FIXME: should this struct be moved to the syntax package? Technically it's
// not a parsed expression so I didn't want to pollute the other package.
type goCmdExpr struct {
	fn GoCmd
}

// FIXME: move to the syntax package?
var noPos = syntax.Pos{}

func (goCmdExpr) Pos() syntax.Pos {
	return noPos
}

func (goCmdExpr) End() syntax.Pos {
	return noPos
}

func (goCmdExpr) CommandNode() {}

var _ syntax.Command = goCmdExpr{}
