// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"bytes"
	"fmt"
	"io"
)

type prog struct {
	stmts []node
}

func (p prog) String() string {
	return nodeJoin(p.stmts, "; ")
}

type node interface {
	fmt.Stringer
}

func nodeJoin(ns []node, sep string) string {
	var b bytes.Buffer
	for i, n := range ns {
		if i > 0 {
			io.WriteString(&b, sep)
		}
		io.WriteString(&b, n.String())
	}
	return b.String()
}

type command struct {
	args []node

	background bool
}

func (c command) String() string {
	nodes := make([]node, 0, len(c.args))
	for _, l := range c.args {
		nodes = append(nodes, l)
	}
	suffix := ""
	if c.background {
		suffix += " &"
	}
	return nodeJoin(nodes, " ") + suffix
}

type redirect struct {
	op  token
	obj node
}

func (r redirect) String() string {
	return r.op.String() + r.obj.String()
}

type subshell struct {
	stmts []node
}

func (s subshell) String() string {
	return "( " + nodeJoin(s.stmts, "; ") + "; )"
}

type block struct {
	stmts []node
}

func (b block) String() string {
	return "{ " + nodeJoin(b.stmts, "; ") + "; }"
}

type ifStmt struct {
	cond      node
	thenStmts []node
	elifs     []node
	elseStmts []node
}

func (s ifStmt) String() string {
	var b bytes.Buffer
	io.WriteString(&b, "if ")
	io.WriteString(&b, s.cond.String())
	io.WriteString(&b, "; then ")
	io.WriteString(&b, nodeJoin(s.thenStmts, "; "))
	for _, n := range s.elifs {
		e := n.(elif)
		io.WriteString(&b, "; ")
		io.WriteString(&b, e.String())
	}
	if len(s.elseStmts) > 0 {
		io.WriteString(&b, "; else ")
		io.WriteString(&b, nodeJoin(s.elseStmts, "; "))
	}
	io.WriteString(&b, "; fi")
	return b.String()
}

type elif struct {
	cond      node
	thenStmts []node
}

func (e elif) String() string {
	var b bytes.Buffer
	io.WriteString(&b, "elif ")
	io.WriteString(&b, e.cond.String())
	io.WriteString(&b, "; then ")
	io.WriteString(&b, nodeJoin(e.thenStmts, "; "))
	return b.String()
}

type whileStmt struct {
	cond    node
	doStmts []node
}

func (w whileStmt) String() string {
	var b bytes.Buffer
	io.WriteString(&b, "while ")
	io.WriteString(&b, w.cond.String())
	io.WriteString(&b, "; do ")
	io.WriteString(&b, nodeJoin(w.doStmts, "; "))
	io.WriteString(&b, "; done")
	return b.String()
}

type binaryExpr struct {
	X, Y node
	op   token
}

func (b binaryExpr) String() string {
	return fmt.Sprintf("%s %s %s", b.X, b.op, b.Y)
}

type comment struct {
	text string
}

func (c comment) String() string {
	return "#" + c.text
}

type funcDecl struct {
	name lit
	body node
}

func (f funcDecl) String() string {
	return fmt.Sprintf("%s() %s", f.name, f.body)
}

type lit struct {
	val string
}

func (l lit) String() string {
	return l.val
}
