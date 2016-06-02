// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

// Node represents an AST node.
type Node interface {
	Pos() Pos
	End() Pos
}

// File is a shell program.
type File struct {
	Name string

	Stmts    []Stmt
	Comments []Comment
}

func (f File) Pos() Pos { return stmtFirstPos(f.Stmts) }
func (f File) End() Pos { return stmtLastEnd(f.Stmts) }

type Comment struct {
	Hash Pos
	Text string
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
func (s Stmt) End() Pos { return s.Position }

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
func (a Assign) End() Pos { return a.Value.End() }

type Redirect struct {
	OpPos Pos
	Op    Token
	N     Lit
	Word  Word
	Hdoc  string
}

func (r Redirect) Pos() Pos {
	if r.N.Pos().Line > 0 {
		return r.N.Pos()
	}
	return r.OpPos
}
func (r Redirect) End() Pos { return r.Word.End() }

type Command struct {
	Args []Word
}

func (c Command) Pos() Pos { return wordFirstPos(c.Args) }
func (c Command) End() Pos { return wordLastEnd(c.Args) }

type Subshell struct {
	Lparen, Rparen Pos
	Stmts          []Stmt
}

func (s Subshell) Pos() Pos { return s.Lparen }
func (s Subshell) End() Pos { return posAfter(s.Rparen, RPAREN) }

type Block struct {
	Lbrace, Rbrace Pos
	Stmts          []Stmt
}

func (b Block) Pos() Pos { return b.Rbrace }
func (b Block) End() Pos { return posAfter(b.Rbrace, RBRACE) }

type IfStmt struct {
	If, Then, Fi Pos
	Cond         Node
	ThenStmts    []Stmt
	Elifs        []Elif
	Else         Pos
	ElseStmts    []Stmt
}

func (s IfStmt) Pos() Pos { return s.If }
func (s IfStmt) End() Pos { return posAfter(s.Fi, FI) }

type StmtCond struct {
	Stmts []Stmt
}

func (s StmtCond) Pos() Pos { return s.Stmts[0].Pos() }
func (s StmtCond) End() Pos { return stmtLastEnd(s.Stmts) }

type CStyleCond struct {
	Lparen, Rparen Pos
	Cond           Node
}

func (c CStyleCond) Pos() Pos { return c.Lparen }
func (c CStyleCond) End() Pos { return posAfter(c.Rparen, RPAREN) }

type Elif struct {
	Elif, Then Pos
	Cond       Node
	ThenStmts  []Stmt
}

type WhileStmt struct {
	While, Do, Done Pos
	Cond            Node
	DoStmts         []Stmt
}

func (w WhileStmt) Pos() Pos { return w.While }
func (w WhileStmt) End() Pos { return posAfter(w.Done, DONE) }

type UntilStmt struct {
	Until, Do, Done Pos
	Cond            Node
	DoStmts         []Stmt
}

func (u UntilStmt) Pos() Pos { return u.Until }
func (u UntilStmt) End() Pos { return posAfter(u.Done, DONE) }

type ForStmt struct {
	For, Do, Done Pos
	Cond          Node
	DoStmts       []Stmt
}

func (f ForStmt) Pos() Pos { return f.For }
func (f ForStmt) End() Pos { return posAfter(f.Done, DONE) }

type WordIter struct {
	Name Lit
	List []Word
}

func (w WordIter) Pos() Pos { return w.Name.Pos() }
func (w WordIter) End() Pos { return wordLastEnd(w.List) }

type CStyleLoop struct {
	Lparen, Rparen   Pos
	Init, Cond, Post Node
}

func (c CStyleLoop) Pos() Pos { return c.Lparen }
func (c CStyleLoop) End() Pos { return posAfter(c.Rparen, RPAREN) }

type UnaryExpr struct {
	OpPos Pos
	Op    Token
	Post  bool
	X     Node
}

func (u UnaryExpr) Pos() Pos {
	if u.Post {
		return u.X.Pos()
	}
	return u.OpPos
}
func (u UnaryExpr) End() Pos {
	if u.Post {
		return posAfter(u.OpPos, u.Op)
	}
	return u.X.End()
}

type BinaryExpr struct {
	OpPos Pos
	Op    Token
	X, Y  Node
}

func (b BinaryExpr) Pos() Pos { return b.X.Pos() }
func (b BinaryExpr) End() Pos { return b.Y.End() }

type FuncDecl struct {
	Position  Pos
	BashStyle bool
	Name      Lit
	Body      Stmt
}

func (f FuncDecl) Pos() Pos { return f.Position }
func (f FuncDecl) End() Pos { return f.Body.End() }

type Word struct {
	Parts []Node
}

func (w Word) Pos() Pos { return nodeFirstPos(w.Parts) }
func (w Word) End() Pos { return nodeLastEnd(w.Parts) }

type Lit struct {
	ValuePos Pos
	Value    string
}

func (l Lit) Pos() Pos { return l.ValuePos }
func (l Lit) End() Pos {
	end := l.ValuePos
	for _, b := range []byte(l.Value) {
		end = moveWith(end, b)
	}
	return end
}

type SglQuoted struct {
	Quote Pos
	Value string
}

func (q SglQuoted) Pos() Pos { return q.Quote }
func (q SglQuoted) End() Pos {
	end := posAfter(q.Quote, SQUOTE)
	for _, b := range []byte(q.Value) {
		end = moveWith(end, b)
	}
	return posAfter(end, SQUOTE)
}

type Quoted struct {
	QuotePos Pos
	Quote    Token
	Parts    []Node
}

func (q Quoted) Pos() Pos { return q.QuotePos }
func (q Quoted) End() Pos { return posAfter(nodeLastEnd(q.Parts), q.Quote) }

type CmdSubst struct {
	Left, Right Pos
	Backquotes  bool
	Stmts       []Stmt
}

func (c CmdSubst) Pos() Pos { return c.Left }
func (c CmdSubst) End() Pos { return posAfter(c.Right, RPAREN) }

type ParamExp struct {
	Dollar        Pos
	Short, Length bool
	Param         Lit
	Ind           *Index
	Repl          *Replace
	Exp           *Expansion
}

func (p ParamExp) Pos() Pos { return p.Dollar }
func (p ParamExp) End() Pos { return p.Dollar }

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
func (a ArithmExpr) End() Pos { return posAfter(a.Rparen, DRPAREN) }

type ParenExpr struct {
	Lparen, Rparen Pos
	X              Node
}

func (p ParenExpr) Pos() Pos { return p.Lparen }
func (p ParenExpr) End() Pos { return posAfter(p.Rparen, RPAREN) }

type CaseStmt struct {
	Case, Esac Pos
	Word       Word
	List       []PatternList
}

func (c CaseStmt) Pos() Pos { return c.Case }
func (c CaseStmt) End() Pos { return posAfter(c.Esac, ESAC) }

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
func (d DeclStmt) End() Pos {
	end := wordLastEnd(d.Opts)
	if len(d.Assigns) > 0 {
		assignEnd := d.Assigns[len(d.Assigns)-1].End()
		if posGreater(assignEnd, end) {
			end = assignEnd
		}
	}
	return end
}

type ArrayExpr struct {
	Lparen, Rparen Pos
	List           []Word
}

func (a ArrayExpr) Pos() Pos { return a.Lparen }
func (a ArrayExpr) End() Pos { return posAfter(a.Rparen, RPAREN) }

type CmdInput struct {
	Lss, Rparen Pos
	Stmts       []Stmt
}

func (c CmdInput) Pos() Pos { return c.Lss }
func (c CmdInput) End() Pos { return posAfter(c.Rparen, RPAREN) }

type EvalStmt struct {
	Eval Pos
	Stmt Stmt
}

func (e EvalStmt) Pos() Pos { return e.Eval }
func (e EvalStmt) End() Pos { return e.Stmt.End() }

type LetStmt struct {
	Let   Pos
	Exprs []Node
}

func (l LetStmt) Pos() Pos { return l.Let }
func (l LetStmt) End() Pos { return nodeLastEnd(l.Exprs) }

func posAfter(pos Pos, tok Token) Pos {
	if pos.Line == 0 {
		return pos
	}
	pos.Column += len(tok.String())
	return pos
}

var defaultPos = Pos{}

func stmtFirstPos(sts []Stmt) Pos {
	if len(sts) == 0 {
		return defaultPos
	}
	return sts[0].Pos()
}

func stmtLastEnd(sts []Stmt) Pos {
	if len(sts) == 0 {
		return defaultPos
	}
	return sts[len(sts)-1].End()
}

func nodeFirstPos(ns []Node) Pos {
	if len(ns) == 0 {
		return defaultPos
	}
	return ns[0].Pos()
}

func nodeLastEnd(ns []Node) Pos {
	if len(ns) == 0 {
		return defaultPos
	}
	return ns[len(ns)-1].End()
}

func wordFirstPos(ws []Word) Pos {
	if len(ws) == 0 {
		return defaultPos
	}
	return ws[0].Pos()
}

func wordLastEnd(ws []Word) Pos {
	if len(ws) == 0 {
		return defaultPos
	}
	return ws[len(ws)-1].End()
}
