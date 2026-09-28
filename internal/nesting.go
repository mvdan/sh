// Copyright (c) 2026, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package internal

import "io"

// NestingWriter is implemented by the writer which package expand passes to
// its Config.CmdSubst, reporting how deeply the command substitution is nested
// within the expansion, such as in arithmetic expressions or parameter
// expansions. Package interp counts these levels towards its limit of nested
// statements, as they are on the same Go stack.
type NestingWriter interface {
	io.Writer
	Nesting() int
}
