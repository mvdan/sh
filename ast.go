// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"bytes"
	"fmt"
)

var defaultPos = Position{}

func nodeFirstPos(ns []Node) Position {
	if len(ns) == 0 {
		return defaultPos
	}
	return ns[0].Pos()
}

func wordFirstPos(ws []Word) Position {
	if len(ws) == 0 {
		return defaultPos
	}
	return ws[0].Pos()
}

type Prog struct {
	Stmts []Stmt
}

func (p Prog) String() string { return stmtJoin(p.Stmts) }

type Node interface {
	fmt.Stringer
	Pos() Position
}

func nodeJoin(ns []Node, sep string) string {
	var b bytes.Buffer
	for i, n := range ns {
		if i > 0 {
			fmt.Fprint(&b, sep)
		}
		fmt.Fprint(&b, n)
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
	Position

	Redirs     []Redirect
	Background bool
}

func (s Stmt) String() string {
	var b bytes.Buffer
	if s.Node != nil {
		fmt.Fprint(&b, s.Node)
	}
	for i, redir := range s.Redirs {
		if i > 0 || s.Node != nil {
			fmt.Fprint(&b, " ")
		}
		fmt.Fprint(&b, redir)
	}
	if s.Background {
		fmt.Fprintf(&b, " &")
	}
	return b.String()
}
func (s Stmt) Pos() Position { return s.Position }

type Redirect struct {
	OpPos Position

	Op   Token
	N    Lit
	Word Word
}

func (r Redirect) String() string {
	var b bytes.Buffer
	fmt.Fprint(&b, r.N)
	fmt.Fprint(&b, r.Op)
	fmt.Fprint(&b, r.Word)
	return b.String()
}

type Command struct {
	Args []Word
}

func (c Command) String() string { return wordJoin(c.Args, " ") }
func (c Command) Pos() Position  { return wordFirstPos(c.Args) }

type Subshell struct {
	Lparen, Rparen Position

	Stmts []Stmt
}

func (s Subshell) String() string { return "(" + stmtJoin(s.Stmts) + ")" }
func (s Subshell) Pos() Position  { return s.Lparen }

type Block struct {
	Lbrace, Rbrace Position

	Stmts []Stmt
}

func (b Block) String() string { return "{ " + stmtJoin(b.Stmts) + "; }" }
func (b Block) Pos() Position  { return b.Rbrace }

type IfStmt struct {
	If, Fi Position

	Conds     []Stmt
	ThenStmts []Stmt
	Elifs     []Elif
	ElseStmts []Stmt
}

func (s IfStmt) String() string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "if %s; ", stmtJoin(s.Conds))
	fmt.Fprintf(&b, "then %s", stmtJoin(s.ThenStmts))
	for _, elif := range s.Elifs {
		fmt.Fprintf(&b, "; %s", elif)
	}
	if len(s.ElseStmts) > 0 {
		fmt.Fprintf(&b, "; else %s", stmtJoin(s.ElseStmts))
	}
	fmt.Fprintf(&b, "; fi")
	return b.String()
}
func (s IfStmt) Pos() Position { return s.If }

type Elif struct {
	Elif Position

	Conds     []Stmt
	ThenStmts []Stmt
}

func (e Elif) String() string {
	return fmt.Sprintf("elif %s; then %s", stmtJoin(e.Conds),
		stmtJoin(e.ThenStmts))
}

type WhileStmt struct {
	While, Done Position

	Conds   []Stmt
	DoStmts []Stmt
}

func (w WhileStmt) String() string {
	return fmt.Sprintf("while %s; do %s; done", stmtJoin(w.Conds),
		stmtJoin(w.DoStmts))
}
func (w WhileStmt) Pos() Position { return w.While }

type ForStmt struct {
	For, Done Position

	Name     Lit
	WordList []Word
	DoStmts  []Stmt
}

func (f ForStmt) String() string {
	return fmt.Sprintf("for %s in %s; do %s; done", f.Name,
		wordJoin(f.WordList, " "), stmtJoin(f.DoStmts))
}
func (f ForStmt) Pos() Position { return f.For }

type BinaryExpr struct {
	OpPos Position

	X, Y Stmt
	Op   Token
}

func (b BinaryExpr) String() string {
	return fmt.Sprintf("%s %s %s", b.X, b.Op, b.Y)
}
func (b BinaryExpr) Pos() Position { return b.X.Pos() }

type FuncDecl struct {
	Name Lit
	Body Stmt
}

func (f FuncDecl) String() string {
	return fmt.Sprintf("%s() %s", f.Name, f.Body)
}
func (f FuncDecl) Pos() Position { return f.Name.Pos() }

type Word struct {
	Parts []Node
}

func (w Word) String() string { return nodeJoin(w.Parts, "") }
func (w Word) Pos() Position  { return nodeFirstPos(w.Parts) }

type Lit struct {
	ValuePos Position
	Value    string
}

func (l Lit) String() string { return l.Value }
func (l Lit) Pos() Position  { return l.ValuePos }

type DblQuoted struct {
	Quote Position

	Parts []Node
}

func (q DblQuoted) String() string { return `"` + nodeJoin(q.Parts, "") + `"` }
func (q DblQuoted) Pos() Position  { return q.Quote }

type CmdSubst struct {
	Exp Position

	Stmts []Stmt
}

func (c CmdSubst) String() string { return "$(" + stmtJoin(c.Stmts) + ")" }
func (c CmdSubst) Pos() Position  { return c.Exp }

type ParamExp struct {
	Exp Position

	Short bool
	Text  string
}

func (p ParamExp) String() string {
	if p.Short {
		return "$" + p.Text
	}
	return "${" + p.Text + "}"
}
func (p ParamExp) Pos() Position { return p.Exp }

type ArithmExp struct {
	Exp Position

	Text string
}

func (a ArithmExp) String() string { return "$((" + a.Text + "))" }
func (a ArithmExp) Pos() Position  { return a.Exp }

type CaseStmt struct {
	Case, Esac Position

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
		fmt.Fprint(&b, plist)
	}
	fmt.Fprintf(&b, "; esac")
	return b.String()
}
func (c CaseStmt) Pos() Position { return c.Case }

type PatternList struct {
	Patterns []Word
	Stmts    []Stmt
}

func (p PatternList) String() string {
	return fmt.Sprintf("%s) %s", wordJoin(p.Patterns, " | "), stmtJoin(p.Stmts))
}
