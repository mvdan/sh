// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"bytes"
	"fmt"
)

type Prog struct {
	Stmts []Stmt
}

func (p Prog) String() string {
	return stmtJoin(p.Stmts)
}

type Node interface {
	fmt.Stringer
}

func nodeJoin(ns []Node, sep string) string {
	var b bytes.Buffer
	for i, n := range ns {
		if i > 0 {
			fmt.Fprintf(&b, "%s", sep)
		}
		fmt.Fprintf(&b, "%s", n)
	}
	return b.String()
}

func stmtJoin(stmts []Stmt) string {
	ns := make([]Node, len(stmts))
	for i, stmt := range stmts {
		ns[i] = stmt
	}
	return nodeJoin(ns, "; ")
}

func wordJoin(words []Word, sep string) string {
	ns := make([]Node, len(words))
	for i, w := range words {
		ns[i] = w
	}
	return nodeJoin(ns, sep)
}

type Stmt struct {
	Node

	Background bool
}

func (s Stmt) String() string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "%s", s.Node)
	if s.Background {
		fmt.Fprintf(&b, " &")
	}
	return b.String()
}

type Command struct {
	Args []Node
}

func (c Command) String() string {
	return nodeJoin(c.Args, " ")
}

type Redirect struct {
	Op  Token
	Obj Word
}

func (r Redirect) String() string {
	return r.Op.String() + r.Obj.String()
}

type Subshell struct {
	Stmts []Stmt
}

func (s Subshell) String() string {
	return "(" + stmtJoin(s.Stmts) + ")"
}

type Block struct {
	Stmts []Stmt
}

func (b Block) String() string {
	return "{ " + stmtJoin(b.Stmts) + "; }"
}

type IfStmt struct {
	Cond      Stmt
	ThenStmts []Stmt
	Elifs     []Elif
	ElseStmts []Stmt
}

func (s IfStmt) String() string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "if %s; then %s", s.Cond, stmtJoin(s.ThenStmts))
	for _, elif := range s.Elifs {
		fmt.Fprintf(&b, "; %s", elif)
	}
	if len(s.ElseStmts) > 0 {
		fmt.Fprintf(&b, "; else %s", stmtJoin(s.ElseStmts))
	}
	fmt.Fprintf(&b, "; fi")
	return b.String()
}

type Elif struct {
	Cond      Stmt
	ThenStmts []Stmt
}

func (e Elif) String() string {
	return fmt.Sprintf("elif %s; then %s", e.Cond, stmtJoin(e.ThenStmts))
}

type WhileStmt struct {
	Cond    Stmt
	DoStmts []Stmt
}

func (w WhileStmt) String() string {
	return fmt.Sprintf("while %s; do %s; done", w.Cond, stmtJoin(w.DoStmts))
}

type ForStmt struct {
	Name     Lit
	WordList []Word
	DoStmts  []Stmt
}

func (f ForStmt) String() string {
	return fmt.Sprintf("for %s in %s; do %s; done", f.Name,
		wordJoin(f.WordList, " "), stmtJoin(f.DoStmts))
}

type BinaryExpr struct {
	X, Y Stmt
	Op   Token
}

func (b BinaryExpr) String() string {
	return fmt.Sprintf("%s %s %s", b.X, b.Op, b.Y)
}

type FuncDecl struct {
	Name Lit
	Body Stmt
}

func (f FuncDecl) String() string {
	return fmt.Sprintf("%s() %s", f.Name, f.Body)
}

type Word struct {
	Parts []Node
}

func (w Word) String() string {
	return nodeJoin(w.Parts, "")
}

type Lit struct {
	Val string
}

func (l Lit) String() string {
	return l.Val
}

type DblQuoted struct {
	Parts []Node
}

func (q DblQuoted) String() string {
	return `"` + nodeJoin(q.Parts, "") + `"`
}

type CmdSubst struct {
	Stmts []Stmt
}

func (c CmdSubst) String() string {
	return "$(" + stmtJoin(c.Stmts) + ")"
}

type ParamExp struct {
	Short bool
	Text  string
}

func (p ParamExp) String() string {
	if p.Short {
		return "$" + p.Text
	}
	return "${" + p.Text + "}"
}

type ArithmExp struct {
	Text string
}

func (a ArithmExp) String() string {
	return "$((" + a.Text + "))"
}

type CaseStmt struct {
	Word Word
	List []PatternList
}

func (c CaseStmt) String() string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "case %s in ", c.Word)
	for i, plist := range c.List {
		if i > 0 {
			fmt.Fprintf(&b, ";; ")
		}
		fmt.Fprintf(&b, "%s", plist)
	}
	fmt.Fprintf(&b, "; esac")
	return b.String()
}

type PatternList struct {
	Patterns []Word
	Stmts    []Stmt
}

func (p PatternList) String() string {
	return fmt.Sprintf("%s) %s", wordJoin(p.Patterns, " | "), stmtJoin(p.Stmts))
}
