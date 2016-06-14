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
	Cmd        Command
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
	if s.Cmd != nil {
		end = s.Cmd.End()
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

// Command represents all nodes that are simple commands, which are
// directly placed in a Stmt.
type Command interface {
	Node
	commandNode()
}

func (CallExpr) commandNode()    {}
func (IfClause) commandNode()    {}
func (WhileClause) commandNode() {}
func (UntilClause) commandNode() {}
func (ForClause) commandNode()   {}
func (CaseClause) commandNode()  {}
func (Block) commandNode()       {}
func (Subshell) commandNode()    {}
func (BinaryCmd) commandNode()   {}
func (FuncDecl) commandNode()    {}
func (DeclClause) commandNode()  {}
func (EvalClause) commandNode()  {}
func (LetClause) commandNode()   {}

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

// IfClause represents an if statement.
type IfClause struct {
	If, Then, Fi Pos
	Cond         Cond
	ThenStmts    []Stmt
	Elifs        []Elif
	Else         Pos
	ElseStmts    []Stmt
}

func (c IfClause) Pos() Pos { return c.If }
func (c IfClause) End() Pos { return posAfter(c.Fi, FI) }

// Cond represents all nodes that can be conditions in an if, while or
// until clause.
type Cond interface {
	Node
	condNode()
}

func (StmtCond) condNode()   {}
func (CStyleCond) condNode() {}

// StmtCond represents a condition that evaluates to the result of a
// series of statements.
type StmtCond struct {
	Stmts []Stmt
}

func (c StmtCond) Pos() Pos { return c.Stmts[0].Pos() }
func (c StmtCond) End() Pos { return stmtLastEnd(c.Stmts) }

// CStyleCond represents a condition that evaluates to the result of an
// arithmetic expression.
type CStyleCond struct {
	Lparen, Rparen Pos
	X              ArithmExpr
}

func (c CStyleCond) Pos() Pos { return c.Lparen }
func (c CStyleCond) End() Pos { return posAfter(c.Rparen, RPAREN) }

// Elif represents an "else if" case in an if clause.
type Elif struct {
	Elif, Then Pos
	Cond       Cond
	ThenStmts  []Stmt
}

// WhileClause represents a while clause.
type WhileClause struct {
	While, Do, Done Pos
	Cond            Cond
	DoStmts         []Stmt
}

func (w WhileClause) Pos() Pos { return w.While }
func (w WhileClause) End() Pos { return posAfter(w.Done, DONE) }

// UntilClause represents an until clause.
type UntilClause struct {
	Until, Do, Done Pos
	Cond            Cond
	DoStmts         []Stmt
}

func (u UntilClause) Pos() Pos { return u.Until }
func (u UntilClause) End() Pos { return posAfter(u.Done, DONE) }

// ForClause represents a for clause.
type ForClause struct {
	For, Do, Done Pos
	Loop          Loop
	DoStmts       []Stmt
}

func (f ForClause) Pos() Pos { return f.For }
func (f ForClause) End() Pos { return posAfter(f.Done, DONE) }

// Loop represents all nodes that can be loops in a for clause.
type Loop interface {
	Node
	loopNode()
}

func (WordIter) loopNode()   {}
func (CStyleLoop) loopNode() {}

// WordIter represents the iteration of a variable over a series of
// words in a for clause.
type WordIter struct {
	Name Lit
	List []Word
}

func (w WordIter) Pos() Pos { return w.Name.Pos() }
func (w WordIter) End() Pos { return posMax(w.Name.End(), wordLastEnd(w.List)) }

// CStyleLoop represents the behaviour of a for clause similar to the C
// language.
type CStyleLoop struct {
	Lparen, Rparen   Pos
	Init, Cond, Post ArithmExpr
}

func (c CStyleLoop) Pos() Pos { return c.Lparen }
func (c CStyleLoop) End() Pos { return posAfter(c.Rparen, RPAREN) }

// UnaryExpr represents an unary expression over a node, either before
// or after it.
type UnaryExpr struct {
	OpPos Pos
	Op    Token
	Post  bool
	X     ArithmExpr
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

// BinaryCmd represents a binary expression between two statements.
type BinaryCmd struct {
	OpPos Pos
	Op    Token
	X, Y  Stmt
}

func (b BinaryCmd) Pos() Pos { return b.X.Pos() }
func (b BinaryCmd) End() Pos { return b.Y.End() }

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
	Parts []WordPart
}

func (w Word) Pos() Pos { return partsFirstPos(w.Parts) }
func (w Word) End() Pos { return partsLastEnd(w.Parts) }

// WordPart represents all nodes that can form a word.
type WordPart interface {
	Node
	wordPartNode()
}

func (Lit) wordPartNode()       {}
func (SglQuoted) wordPartNode() {}
func (Quoted) wordPartNode()    {}
func (ParamExp) wordPartNode()  {}
func (CmdSubst) wordPartNode()  {}
func (ArithmExp) wordPartNode() {}
func (ProcSubst) wordPartNode() {}
func (ArrayExpr) wordPartNode() {} // TODO: remove?

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
	Parts    []WordPart
}

func (q Quoted) Pos() Pos { return q.QuotePos }
func (q Quoted) End() Pos { return posAfter(partsLastEnd(q.Parts), q.Quote) }

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

// ArithmExp represents an arithmetic expansion.
type ArithmExp struct {
	Dollar, Rparen Pos
	X              ArithmExpr
}

func (a ArithmExp) Pos() Pos { return a.Dollar }
func (a ArithmExp) End() Pos { return posAfter(a.Rparen, DRPAREN) }

// ArithmExpr represents all nodes that form arithmetic expressions.
type ArithmExpr interface {
	Node
	arithmExprNode()
}

func (BinaryExpr) arithmExprNode() {}
func (UnaryExpr) arithmExprNode()  {}
func (ParenExpr) arithmExprNode()  {}
func (Word) arithmExprNode()       {}

// BinaryExpr represents a binary expression between two arithmetic
// expression.
type BinaryExpr struct {
	OpPos Pos
	Op    Token
	X, Y  ArithmExpr
}

func (b BinaryExpr) Pos() Pos { return b.X.Pos() }
func (b BinaryExpr) End() Pos { return b.Y.End() }

// ParenExpr represents an expression within parentheses inside an
// ArithmExp.
type ParenExpr struct {
	Lparen, Rparen Pos
	X              ArithmExpr
}

func (p ParenExpr) Pos() Pos { return p.Lparen }
func (p ParenExpr) End() Pos { return posAfter(p.Rparen, RPAREN) }

// CaseClause represents a case (switch) clause.
type CaseClause struct {
	Case, Esac Pos
	Word       Word
	List       []PatternList
}

func (c CaseClause) Pos() Pos { return c.Case }
func (c CaseClause) End() Pos { return posAfter(c.Esac, ESAC) }

// PatternList represents a pattern list (case) within a CaseClause.
type PatternList struct {
	Dsemi    Pos
	Patterns []Word
	Stmts    []Stmt
}

// DeclClause represents a Bash declare clause.
type DeclClause struct {
	Declare Pos
	Local   bool
	Opts    []Word
	Assigns []Assign
}

func (d DeclClause) Pos() Pos { return d.Declare }
func (d DeclClause) End() Pos {
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

// EvalClause represents a Bash eval clause.
type EvalClause struct {
	Eval Pos
	Stmt Stmt
}

func (e EvalClause) Pos() Pos { return e.Eval }
func (e EvalClause) End() Pos { return e.Stmt.End() }

// LetClause represents a Bash let clause.
type LetClause struct {
	Let   Pos
	Exprs []ArithmExpr
}

func (l LetClause) Pos() Pos { return l.Let }
func (l LetClause) End() Pos {
	if len(l.Exprs) == 0 {
		return defaultPos
	}
	return l.Exprs[len(l.Exprs)-1].End()
}

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

func partsFirstPos(ps []WordPart) Pos {
	if len(ps) == 0 {
		return defaultPos
	}
	return ps[0].Pos()
}

func partsLastEnd(ps []WordPart) Pos {
	if len(ps) == 0 {
		return defaultPos
	}
	return ps[len(ps)-1].End()
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
