// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"bytes"
	"fmt"
)

var defaultPos = Pos{}

func nodeFirstPos(ns []Node) Pos {
	if len(ns) == 0 {
		return defaultPos
	}
	return ns[0].Pos()
}

func wordFirstPos(ws []Word) Pos {
	if len(ws) == 0 {
		return defaultPos
	}
	return ws[0].Pos()
}

type File struct {
	Name string

	Stmts []Stmt
}

func (f File) String() string { return stmtJoin(f.Stmts) }

type Node interface {
	fmt.Stringer
	Pos() Pos
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
	var b bytes.Buffer
	for i, s := range stmts {
		if i > 0 {
			fmt.Fprint(&b, "; ")
		}
		fmt.Fprint(&b, s)
	}
	return b.String()
}

func stmtList(stmts []Stmt) string {
	if len(stmts) == 0 {
		return ";"
	}
	return " " + stmtJoin(stmts) + ";"
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
	Position Pos

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
func (s Stmt) Pos() Pos { return s.Position }

type Redirect struct {
	OpPos Pos

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
func (c Command) Pos() Pos       { return wordFirstPos(c.Args) }

type Subshell struct {
	Lparen, Rparen Pos

	Stmts []Stmt
}

func (s Subshell) String() string { return "(" + stmtJoin(s.Stmts) + ")" }
func (s Subshell) Pos() Pos       { return s.Lparen }

type Block struct {
	Lbrace, Rbrace Pos

	Stmts []Stmt
}

func (b Block) String() string { return "{ " + stmtJoin(b.Stmts) + "; }" }
func (b Block) Pos() Pos       { return b.Rbrace }

type IfStmt struct {
	If, Fi Pos

	Conds     []Stmt
	ThenStmts []Stmt
	Elifs     []Elif
	ElseStmts []Stmt
}

func (s IfStmt) String() string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "if%s", stmtList(s.Conds))
	fmt.Fprintf(&b, " then%s", stmtList(s.ThenStmts))
	for _, elif := range s.Elifs {
		fmt.Fprintf(&b, " %s", elif)
	}
	if len(s.ElseStmts) > 0 {
		fmt.Fprintf(&b, " else%s", stmtList(s.ElseStmts))
	}
	fmt.Fprintf(&b, " fi")
	return b.String()
}
func (s IfStmt) Pos() Pos { return s.If }

type Elif struct {
	Elif Pos

	Conds     []Stmt
	ThenStmts []Stmt
}

func (e Elif) String() string {
	return fmt.Sprintf("elif%s then%s", stmtList(e.Conds), stmtList(e.ThenStmts))
}

type WhileStmt struct {
	While, Done Pos

	Conds   []Stmt
	DoStmts []Stmt
}

func (w WhileStmt) String() string {
	return fmt.Sprintf("while%s do%s done", stmtList(w.Conds), stmtList(w.DoStmts))
}
func (w WhileStmt) Pos() Pos { return w.While }

type ForStmt struct {
	For, Done Pos

	Name     Lit
	WordList []Word
	DoStmts  []Stmt
}

func (f ForStmt) String() string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "for %s", f.Name)
	if len(f.WordList) > 0 {
		fmt.Fprintf(&b, " in %s", wordJoin(f.WordList, " "))
	}
	fmt.Fprintf(&b, "; do%s done", stmtList(f.DoStmts))
	return b.String()
}
func (f ForStmt) Pos() Pos { return f.For }

type BinaryExpr struct {
	OpPos Pos

	X, Y Stmt
	Op   Token
}

func (b BinaryExpr) String() string {
	return fmt.Sprintf("%s %s %s", b.X, b.Op, b.Y)
}
func (b BinaryExpr) Pos() Pos { return b.X.Pos() }

type FuncDecl struct {
	Name Lit
	Body Stmt
}

func (f FuncDecl) String() string {
	return fmt.Sprintf("%s() %s", f.Name, f.Body)
}
func (f FuncDecl) Pos() Pos { return f.Name.Pos() }

type Word struct {
	Parts []Node
}

func (w Word) String() string { return nodeJoin(w.Parts, "") }
func (w Word) Pos() Pos       { return nodeFirstPos(w.Parts) }

type Lit struct {
	ValuePos Pos
	Value    string
}

func (l Lit) String() string { return l.Value }
func (l Lit) Pos() Pos       { return l.ValuePos }

type DblQuoted struct {
	Quote Pos

	Parts []Node
}

func (q DblQuoted) String() string { return `"` + nodeJoin(q.Parts, "") + `"` }
func (q DblQuoted) Pos() Pos       { return q.Quote }

type BckQuoted struct {
	Quote Pos

	Stmts []Stmt
}

func (q BckQuoted) String() string { return "`" + stmtJoin(q.Stmts) + "`" }
func (q BckQuoted) Pos() Pos       { return q.Quote }

type CmdSubst struct {
	Exp Pos

	Stmts []Stmt
}

func (c CmdSubst) String() string { return "$(" + stmtJoin(c.Stmts) + ")" }
func (c CmdSubst) Pos() Pos       { return c.Exp }

type ParamExp struct {
	Exp Pos

	Short bool
	Text  string
}

func (p ParamExp) String() string {
	if p.Short {
		return "$" + p.Text
	}
	return "${" + p.Text + "}"
}
func (p ParamExp) Pos() Pos { return p.Exp }

type ArithmExp struct {
	Exp Pos

	Text string
}

func (a ArithmExp) String() string { return "$((" + a.Text + "))" }
func (a ArithmExp) Pos() Pos       { return a.Exp }

type CaseStmt struct {
	Case, Esac Pos

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
func (c CaseStmt) Pos() Pos { return c.Case }

type PatternList struct {
	Patterns []Word
	Stmts    []Stmt
}

func (p PatternList) String() string {
	return fmt.Sprintf("%s) %s", wordJoin(p.Patterns, " | "), stmtJoin(p.Stmts))
}
