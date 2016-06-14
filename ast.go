// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

// Node represents an AST node.
type Node interface {
	// Pos returns the first character of the node
	Pos() Pos
	// End returns the character immediately after the node
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

// Comment represents a single comment on a single line.
type Comment struct {
	Hash Pos
	Text string
}

// Stmt represents a statement, otherwise known as a compound command.
// It is compromised of a node, like Command or IfStmt, and other
// components that may come before or after it.
type Stmt struct {
	Node
	Position   Pos
	Negated    bool
	Background bool
	Assigns    []Assign
	Redirs     []Redirect
}

func (s Stmt) Pos() Pos { return s.Position }
func (s Stmt) End() Pos {
	end := s.Position
	if s.Negated {
		end = posAfter(end, NOT)
	}
	if s.Node != nil {
		end = s.Node.End()
	}
	if len(s.Assigns) > 0 {
		assEnd := s.Assigns[len(s.Assigns)-1].End()
		end = posMax(end, assEnd)
	}
	if len(s.Redirs) > 0 {
		redEnd := s.Redirs[len(s.Redirs)-1].End()
		end = posMax(end, redEnd)
	}
	return end
}

// Assign represents an assignment to a variable.
type Assign struct {
	Append bool
	Name   *Lit
	Value  Word
}

func (a Assign) Pos() Pos {
	if a.Name == nil {
		return a.Value.Pos()
	}
	return a.Name.Pos()
}
func (a Assign) End() Pos {
	if a.Name != nil {
		return posMax(a.Name.End(), a.Value.End())
	}
	return a.Value.End()
}

// Redirect represents an input/output redirection.
type Redirect struct {
	OpPos Pos
	Op    Token
	N     *Lit
	Word  Word
	Hdoc  *Lit
}

func (r Redirect) Pos() Pos {
	if r.N != nil {
		return r.N.Pos()
	}
	return r.OpPos
}
func (r Redirect) End() Pos { return r.Word.End() }

// CallExpr represents a command execution or function call.
type CallExpr struct {
	Args []Word
}

func (c CallExpr) Pos() Pos { return wordFirstPos(c.Args) }
func (c CallExpr) End() Pos { return wordLastEnd(c.Args) }

// Subshell represents a series of commands that should be executed in a
// nested shell environment.
type Subshell struct {
	Lparen, Rparen Pos
	Stmts          []Stmt
}

func (s Subshell) Pos() Pos { return s.Lparen }
func (s Subshell) End() Pos { return posAfter(s.Rparen, RPAREN) }

// Block represents a series of commands that should be executed in a
// nested scope.
type Block struct {
	Lbrace, Rbrace Pos
	Stmts          []Stmt
}

func (b Block) Pos() Pos { return b.Rbrace }
func (b Block) End() Pos { return posAfter(b.Rbrace, RBRACE) }

// IfStmt represents an if statement.
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

// StmtCond represents a condition that evaluates to the result of a
// series of statements.
type StmtCond struct {
	Stmts []Stmt
}

func (s StmtCond) Pos() Pos { return s.Stmts[0].Pos() }
func (s StmtCond) End() Pos { return stmtLastEnd(s.Stmts) }

// CStyleCond represents a condition that evaluates to the result of an
// arithmetic expression.
type CStyleCond struct {
	Lparen, Rparen Pos
	X              Node
}

func (c CStyleCond) Pos() Pos { return c.Lparen }
func (c CStyleCond) End() Pos { return posAfter(c.Rparen, RPAREN) }

// Elif represents an "else if" case in an if statement.
type Elif struct {
	Elif, Then Pos
	Cond       Node
	ThenStmts  []Stmt
}

// WhileStmt represents a while statement.
type WhileStmt struct {
	While, Do, Done Pos
	Cond            Node
	DoStmts         []Stmt
}

func (w WhileStmt) Pos() Pos { return w.While }
func (w WhileStmt) End() Pos { return posAfter(w.Done, DONE) }

// UntilStmt represents an until statement.
type UntilStmt struct {
	Until, Do, Done Pos
	Cond            Node
	DoStmts         []Stmt
}

func (u UntilStmt) Pos() Pos { return u.Until }
func (u UntilStmt) End() Pos { return posAfter(u.Done, DONE) }

// ForStmt represents a for statement.
type ForStmt struct {
	For, Do, Done Pos
	Cond          Node
	DoStmts       []Stmt
}

func (f ForStmt) Pos() Pos { return f.For }
func (f ForStmt) End() Pos { return posAfter(f.Done, DONE) }

// WordIter represents the iteration of a variable over a series of
// words in a for statement.
type WordIter struct {
	Name Lit
	List []Word
}

func (w WordIter) Pos() Pos { return w.Name.Pos() }
func (w WordIter) End() Pos { return posMax(w.Name.End(), wordLastEnd(w.List)) }

// CStyleLoop represents the behaviour of a for statement similar to the
// C language.
type CStyleLoop struct {
	Lparen, Rparen   Pos
	Init, Cond, Post Node
}

func (c CStyleLoop) Pos() Pos { return c.Lparen }
func (c CStyleLoop) End() Pos { return posAfter(c.Rparen, RPAREN) }

// UnaryExpr represents an unary expression over a node, either before
// or after it.
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

// BinaryExpr represents a binary expression between two nodes.
type BinaryExpr struct {
	OpPos Pos
	Op    Token
	X, Y  Node
}

func (b BinaryExpr) Pos() Pos { return b.X.Pos() }
func (b BinaryExpr) End() Pos { return b.Y.End() }

// FuncDecl represents the declaration of a function.
type FuncDecl struct {
	Position  Pos
	BashStyle bool
	Name      Lit
	Body      Stmt
}

func (f FuncDecl) Pos() Pos { return f.Position }
func (f FuncDecl) End() Pos { return f.Body.End() }

// Word represents a list of nodes that are contiguous to each other and
// are delimeted by word boundaries.
type Word struct {
	Parts []Node
}

func (w Word) Pos() Pos { return nodeFirstPos(w.Parts) }
func (w Word) End() Pos { return nodeLastEnd(w.Parts) }

// Lit represents an unquoted string consisting of characters that were
// not tokenized.
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

// SglQuoted represents a single-quoted string.
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

// Quoted represents a quoted list of nodes. Single quotes are
// represented separately as SglQuoted.
type Quoted struct {
	QuotePos Pos
	Quote    Token
	Parts    []Node
}

func (q Quoted) Pos() Pos { return q.QuotePos }
func (q Quoted) End() Pos { return posAfter(nodeLastEnd(q.Parts), q.Quote) }

// CmdSubst represents a command substitution.
type CmdSubst struct {
	Left, Right Pos
	Backquotes  bool
	Stmts       []Stmt
}

func (c CmdSubst) Pos() Pos { return c.Left }
func (c CmdSubst) End() Pos { return posAfter(c.Right, RPAREN) }

// ParamExp represents a parameter expansion.
type ParamExp struct {
	Dollar        Pos
	Short, Length bool
	Param         Lit
	Ind           *Index
	Repl          *Replace
	Exp           *Expansion
}

func (p ParamExp) Pos() Pos { return p.Dollar }
func (p ParamExp) End() Pos {
	end := p.Param.End()
	if p.Ind != nil {
		end = posMax(end, p.Ind.Word.End())
	}
	if p.Repl != nil {
		end = posMax(end, p.Repl.With.End())
	}
	if p.Exp != nil {
		end = posMax(end, p.Exp.Word.End())
	}
	return posAfter(end, RBRACE)
}

// Index represents access to an array via an index inside a ParamExp.
type Index struct {
	Word Word
}

// Replace represents a search and replace inside a ParamExp.
type Replace struct {
	All        bool
	Orig, With Word
}

// Expansion represents string manipulation in a ParamExp other than
// those covered by Replace.
type Expansion struct {
	Op   Token
	Word Word
}

// ArithmExpr represents an arithmetic expression.
type ArithmExpr struct {
	Dollar, Rparen Pos
	X              Node
}

func (a ArithmExpr) Pos() Pos { return a.Dollar }
func (a ArithmExpr) End() Pos { return posAfter(a.Rparen, DRPAREN) }

// ParenExpr represents an expression within parentheses inside an
// ArithmExpr.
type ParenExpr struct {
	Lparen, Rparen Pos
	X              Node
}

func (p ParenExpr) Pos() Pos { return p.Lparen }
func (p ParenExpr) End() Pos { return posAfter(p.Rparen, RPAREN) }

// CaseStmt represents a case (switch) statement.
type CaseStmt struct {
	Case, Esac Pos
	Word       Word
	List       []PatternList
}

func (c CaseStmt) Pos() Pos { return c.Case }
func (c CaseStmt) End() Pos { return posAfter(c.Esac, ESAC) }

// PatternList represents a pattern list (case) within a CaseStmt.
type PatternList struct {
	Dsemi    Pos
	Patterns []Word
	Stmts    []Stmt
}

// DeclStmt represents a Bash declare statement.
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
		end = posMax(end, assignEnd)
	}
	return end
}

// ArrayExpr represents a Bash array expression.
type ArrayExpr struct {
	Lparen, Rparen Pos
	List           []Word
}

func (a ArrayExpr) Pos() Pos { return a.Lparen }
func (a ArrayExpr) End() Pos { return posAfter(a.Rparen, RPAREN) }

// ProcSubst represents a Bash process substitution.
type ProcSubst struct {
	OpPos, Rparen Pos
	Op            Token
	Stmts         []Stmt
}

func (s ProcSubst) Pos() Pos { return s.OpPos }
func (s ProcSubst) End() Pos { return posAfter(s.Rparen, RPAREN) }

// EvalStmt represents a Bash eval statement.
type EvalStmt struct {
	Eval Pos
	Stmt Stmt
}

func (e EvalStmt) Pos() Pos { return e.Eval }
func (e EvalStmt) End() Pos { return e.Stmt.End() }

// LetStmt represents a Bash let statement.
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
