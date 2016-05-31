// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

var defaultPos = Pos{}

func stmtFirstPos(sts []Stmt) Pos {
	if len(sts) == 0 {
		return defaultPos
	}
	return sts[0].Pos()
}

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

// File is a shell program.
type File struct {
	Name string

	Stmts []Stmt
}

func (f File) Pos() Pos { return stmtFirstPos(f.Stmts) }

// Node represents an AST node.
type Node interface {
	Pos() Pos
}

type Stmt struct {
	Node
	Position   Pos
	Negated    bool
	Background bool
	Assigns    []Assign
	Redirs     []Redirect
}

func (s Stmt) Pos() Pos { return s.Position }

type Assign struct {
	Append bool
	Name   Node
	Value  Word
}

func (a Assign) Pos() Pos {
	if a.Name == nil {
		return a.Value.Pos()
	}
	return a.Name.Pos()
}

type Redirect struct {
	OpPos Pos
	Op    Token
	N     Lit
	Word  Word
}

type Command struct {
	Args []Word
}

func (c Command) Pos() Pos { return wordFirstPos(c.Args) }

type Subshell struct {
	Lparen, Rparen Pos
	Stmts          []Stmt
}

func (s Subshell) Pos() Pos { return s.Lparen }

type Block struct {
	Lbrace, Rbrace Pos
	Stmts          []Stmt
}

func (b Block) Pos() Pos { return b.Rbrace }

type IfStmt struct {
	If, Fi    Pos
	Cond      Node
	ThenStmts []Stmt
	Elifs     []Elif
	Else      Pos
	ElseStmts []Stmt
}

func (s IfStmt) Pos() Pos { return s.If }

type StmtCond struct {
	Stmts []Stmt
}

func (s StmtCond) Pos() Pos { return s.Stmts[0].Pos() }

type CStyleCond struct {
	Lparen, Rparen Pos
	Cond           Node
}

func (c CStyleCond) Pos() Pos { return c.Lparen }

type Elif struct {
	Elif      Pos
	Cond      Node
	ThenStmts []Stmt
}

type WhileStmt struct {
	While, Done Pos
	Cond        Node
	DoStmts     []Stmt
}

func (w WhileStmt) Pos() Pos { return w.While }

type UntilStmt struct {
	Until, Done Pos
	Cond        Node
	DoStmts     []Stmt
}

func (u UntilStmt) Pos() Pos { return u.Until }

type ForStmt struct {
	For, Done Pos
	Cond      Node
	DoStmts   []Stmt
}

func (f ForStmt) Pos() Pos { return f.For }

type WordIter struct {
	Name Lit
	List []Word
}

func (w WordIter) Pos() Pos { return w.Name.Pos() }

type CStyleLoop struct {
	Lparen, Rparen   Pos
	Init, Cond, Post Node
}

func (c CStyleLoop) Pos() Pos { return c.Lparen }

type UnaryExpr struct {
	OpPos Pos
	Op    Token
	Post  bool
	X     Node
}

func (u UnaryExpr) Pos() Pos { return u.OpPos }

type BinaryExpr struct {
	OpPos Pos
	Op    Token
	X, Y  Node
}

func (b BinaryExpr) Pos() Pos { return b.X.Pos() }

type FuncDecl struct {
	Position  Pos
	BashStyle bool
	Name      Lit
	Body      Stmt
}

func (f FuncDecl) Pos() Pos { return f.Position }

type Word struct {
	Parts []Node
}

func (w Word) Pos() Pos { return nodeFirstPos(w.Parts) }

type Lit struct {
	ValuePos Pos
	Value    string
}

func (l Lit) Pos() Pos { return l.ValuePos }

type SglQuoted struct {
	Quote Pos
	Value string
}

func (q SglQuoted) Pos() Pos { return q.Quote }

type Quoted struct {
	QuotePos Pos
	Quote    Token
	Parts    []Node
}

func (q Quoted) Pos() Pos { return q.QuotePos }

type CmdSubst struct {
	Left, Right Pos
	Backquotes  bool
	Stmts       []Stmt
}

func (c CmdSubst) Pos() Pos { return c.Left }

type ParamExp struct {
	Dollar        Pos
	Short, Length bool
	Param         Lit
	Ind           *Index
	Repl          *Replace
	Exp           *Expansion
}

func (p ParamExp) Pos() Pos { return p.Dollar }

type Index struct {
	Word Word
}

type Replace struct {
	All        bool
	Orig, With Word
}

type Expansion struct {
	Op   Token
	Word Word
}

type ArithmExpr struct {
	Dollar, Rparen Pos
	X              Node
}

func (a ArithmExpr) Pos() Pos { return a.Dollar }

type ParenExpr struct {
	Lparen, Rparen Pos
	X              Node
}

func (p ParenExpr) Pos() Pos { return p.Lparen }

type CaseStmt struct {
	Case, Esac Pos
	Word       Word
	List       []PatternList
}

func (c CaseStmt) Pos() Pos { return c.Case }

type PatternList struct {
	Dsemi    Pos
	Patterns []Word
	Stmts    []Stmt
}

type DeclStmt struct {
	Declare Pos
	Local   bool
	Opts    []Word
	Assigns []Assign
}

func (d DeclStmt) Pos() Pos { return d.Declare }

type ArrayExpr struct {
	Lparen, Rparen Pos
	List           []Word
}

func (a ArrayExpr) Pos() Pos { return a.Lparen }

type CmdInput struct {
	Lss, Rparen Pos
	Stmts       []Stmt
}

func (c CmdInput) Pos() Pos { return c.Lss }

type EvalStmt struct {
	Eval Pos
	Stmt Stmt
}

func (e EvalStmt) Pos() Pos { return e.Eval }

type LetStmt struct {
	Let   Pos
	Exprs []Node
}

func (l LetStmt) Pos() Pos { return l.Let }
