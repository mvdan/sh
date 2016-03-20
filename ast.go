// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"bytes"
	"fmt"
	"io"
)

type Prog struct {
	Stmts []Node
}

func (p Prog) String() string {
	return nodeJoin(p.Stmts, "; ")
}

type Node interface {
	fmt.Stringer
}

func nodeJoin(ns []Node, sep string) string {
	var b bytes.Buffer
	for i, n := range ns {
		if i > 0 {
			io.WriteString(&b, sep)
		}
		io.WriteString(&b, n.String())
	}
	return b.String()
}

type Command struct {
	Args []Node

	Background bool
}

func (c Command) String() string {
	nodes := make([]Node, 0, len(c.Args))
	for _, l := range c.Args {
		nodes = append(nodes, l)
	}
	suffix := ""
	if c.Background {
		suffix += " &"
	}
	return nodeJoin(nodes, " ") + suffix
}

type Redirect struct {
	Op  Token
	Obj Node
}

func (r Redirect) String() string {
	return r.Op.String() + r.Obj.String()
}

type Subshell struct {
	Stmts []Node
}

func (s Subshell) String() string {
	return "( " + nodeJoin(s.Stmts, "; ") + "; )"
}

type Block struct {
	Stmts []Node
}

func (b Block) String() string {
	return "{ " + nodeJoin(b.Stmts, "; ") + "; }"
}

type IfStmt struct {
	Cond      Node
	ThenStmts []Node
	Elifs     []Node
	ElseStmts []Node
}

func (s IfStmt) String() string {
	var b bytes.Buffer
	io.WriteString(&b, "if ")
	io.WriteString(&b, s.Cond.String())
	io.WriteString(&b, "; then ")
	io.WriteString(&b, nodeJoin(s.ThenStmts, "; "))
	for _, n := range s.Elifs {
		e := n.(Elif)
		io.WriteString(&b, "; ")
		io.WriteString(&b, e.String())
	}
	if len(s.ElseStmts) > 0 {
		io.WriteString(&b, "; else ")
		io.WriteString(&b, nodeJoin(s.ElseStmts, "; "))
	}
	io.WriteString(&b, "; fi")
	return b.String()
}

type Elif struct {
	Cond      Node
	ThenStmts []Node
}

func (e Elif) String() string {
	var b bytes.Buffer
	io.WriteString(&b, "elif ")
	io.WriteString(&b, e.Cond.String())
	io.WriteString(&b, "; then ")
	io.WriteString(&b, nodeJoin(e.ThenStmts, "; "))
	return b.String()
}

type WhileStmt struct {
	Cond    Node
	DoStmts []Node
}

func (w WhileStmt) String() string {
	var b bytes.Buffer
	io.WriteString(&b, "while ")
	io.WriteString(&b, w.Cond.String())
	io.WriteString(&b, "; do ")
	io.WriteString(&b, nodeJoin(w.DoStmts, "; "))
	io.WriteString(&b, "; done")
	return b.String()
}

type ForStmt struct {
	Name     Node
	WordList []Node
	DoStmts  []Node
}

func (w ForStmt) String() string {
	var b bytes.Buffer
	io.WriteString(&b, "for ")
	io.WriteString(&b, w.Name.String())
	io.WriteString(&b, " in ")
	io.WriteString(&b, nodeJoin(w.WordList, " "))
	io.WriteString(&b, "; do ")
	io.WriteString(&b, nodeJoin(w.DoStmts, "; "))
	io.WriteString(&b, "; done")
	return b.String()
}

type BinaryExpr struct {
	X, Y Node
	Op   Token
}

func (b BinaryExpr) String() string {
	return fmt.Sprintf("%s %s %s", b.X, b.Op, b.Y)
}

type Comment struct {
	Text string
}

func (c Comment) String() string {
	return "#" + c.Text
}

type FuncDecl struct {
	Name Lit
	Body Node
}

func (f FuncDecl) String() string {
	return fmt.Sprintf("%s() %s", f.Name, f.Body)
}

type Lit struct {
	Val string
}

func (l Lit) String() string {
	return l.Val
}
