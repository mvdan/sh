// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package ast

import (
	"github.com/mvdan/sh/internal"
	"github.com/mvdan/sh/token"
)

// Node represents an AST node.
type Node interface {
	// Pos returns the first character of the node
	Pos() token.Pos
	// End returns the character immediately after the node
	End() token.Pos
}

// File is a shell program.
type File struct {
	Name string

	Stmts    []*Stmt
	Comments []*Comment

	// Lines contains the offset of the first character for each
	// line (the first entry is always 0)
	Lines []int
}

func (f *File) Pos() token.Pos { return stmtFirstPos(f.Stmts) }
func (f *File) End() token.Pos { return stmtLastEnd(f.Stmts) }

func (f *File) Position(p token.Pos) (pos token.Position) {
	intp := int(p)
	pos.Offset = intp - 1
	if i := searchInts(f.Lines, intp); i >= 0 {
		pos.Line, pos.Column = i+1, intp-f.Lines[i]
	}
	return
}

// Inlined version of:
// sort.Search(len(a), func(i int) bool { return a[i] > x }) - 1
func searchInts(a []int, x int) int {
	i, j := 0, len(a)
	for i < j {
		h := i + (j-i)/2
		if a[h] <= x {
			i = h + 1
		} else {
			j = h
		}
	}
	return i - 1
}

func posMax(p1, p2 token.Pos) token.Pos {
	if p2 > p1 {
		return p2
	}
	return p1
}

// Comment represents a single comment on a single line.
type Comment struct {
	Hash token.Pos
	Text string
}

func (c *Comment) Pos() token.Pos { return c.Hash }
func (c *Comment) End() token.Pos { return posAddStr(c.Hash, c.Text) }

// Stmt represents a statement, otherwise known as a compound command.
// It is compromised of a command and other components that may come
// before or after it.
type Stmt struct {
	Cmd        Command
	Position   token.Pos
	Negated    bool
	Background bool
	Assigns    []*Assign
	Redirs     []*Redirect
}

func (s *Stmt) Pos() token.Pos { return s.Position }
func (s *Stmt) End() token.Pos {
	end := s.Position
	if s.Negated {
		end = posAdd(end, 1)
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

func (*CallExpr) commandNode()     {}
func (*IfClause) commandNode()     {}
func (*WhileClause) commandNode()  {}
func (*UntilClause) commandNode()  {}
func (*ForClause) commandNode()    {}
func (*CaseClause) commandNode()   {}
func (*Block) commandNode()        {}
func (*Subshell) commandNode()     {}
func (*BinaryCmd) commandNode()    {}
func (*FuncDecl) commandNode()     {}
func (*ArithmExp) commandNode()    {}
func (*TestClause) commandNode()   {}
func (*DeclClause) commandNode()   {}
func (*EvalClause) commandNode()   {}
func (*LetClause) commandNode()    {}
func (*CoprocClause) commandNode() {}

// Assign represents an assignment to a variable.
type Assign struct {
	Append bool
	Name   *Lit
	Value  Word
}

func (a *Assign) Pos() token.Pos {
	if a.Name == nil {
		return a.Value.Pos()
	}
	return a.Name.Pos()
}
func (a *Assign) End() token.Pos {
	if a.Name != nil {
		return posMax(a.Name.End(), a.Value.End())
	}
	return a.Value.End()
}

// Redirect represents an input/output redirection.
type Redirect struct {
	OpPos      token.Pos
	Op         token.Token
	N          *Lit
	Word, Hdoc Word
}

func (r *Redirect) Pos() token.Pos {
	if r.N != nil {
		return r.N.Pos()
	}
	return r.OpPos
}
func (r *Redirect) End() token.Pos { return r.Word.End() }

// CallExpr represents a command execution or function call.
type CallExpr struct {
	Args []Word
}

func (c *CallExpr) Pos() token.Pos { return c.Args[0].Pos() }
func (c *CallExpr) End() token.Pos { return c.Args[len(c.Args)-1].End() }

// Subshell represents a series of commands that should be executed in a
// nested shell environment.
type Subshell struct {
	Lparen, Rparen token.Pos
	Stmts          []*Stmt
}

func (s *Subshell) Pos() token.Pos { return s.Lparen }
func (s *Subshell) End() token.Pos { return posAdd(s.Rparen, 1) }

// Block represents a series of commands that should be executed in a
// nested scope.
type Block struct {
	Lbrace, Rbrace token.Pos
	Stmts          []*Stmt
}

func (b *Block) Pos() token.Pos { return b.Rbrace }
func (b *Block) End() token.Pos { return posAdd(b.Rbrace, 1) }

// IfClause represents an if statement.
type IfClause struct {
	If, Then, Fi token.Pos
	CondStmts    []*Stmt
	ThenStmts    []*Stmt
	Elifs        []*Elif
	Else         token.Pos
	ElseStmts    []*Stmt
}

func (c *IfClause) Pos() token.Pos { return c.If }
func (c *IfClause) End() token.Pos { return posAdd(c.Fi, 2) }

// Elif represents an "else if" case in an if clause.
type Elif struct {
	Elif, Then token.Pos
	CondStmts  []*Stmt
	ThenStmts  []*Stmt
}

// WhileClause represents a while clause.
type WhileClause struct {
	While, Do, Done token.Pos
	CondStmts       []*Stmt
	DoStmts         []*Stmt
}

func (w *WhileClause) Pos() token.Pos { return w.While }
func (w *WhileClause) End() token.Pos { return posAdd(w.Done, 4) }

// UntilClause represents an until clause.
type UntilClause struct {
	Until, Do, Done token.Pos
	CondStmts       []*Stmt
	DoStmts         []*Stmt
}

func (u *UntilClause) Pos() token.Pos { return u.Until }
func (u *UntilClause) End() token.Pos { return posAdd(u.Done, 4) }

// ForClause represents a for clause.
type ForClause struct {
	For, Do, Done token.Pos
	Loop          Loop
	DoStmts       []*Stmt
}

func (f *ForClause) Pos() token.Pos { return f.For }
func (f *ForClause) End() token.Pos { return posAdd(f.Done, 4) }

// Loop represents all nodes that can be loops in a for clause.
type Loop interface {
	Node
	loopNode()
}

func (*WordIter) loopNode()   {}
func (*CStyleLoop) loopNode() {}

// WordIter represents the iteration of a variable over a series of
// words in a for clause.
type WordIter struct {
	Name Lit
	List []Word
}

func (w *WordIter) Pos() token.Pos { return w.Name.Pos() }
func (w *WordIter) End() token.Pos { return posMax(w.Name.End(), wordLastEnd(w.List)) }

// CStyleLoop represents the behaviour of a for clause similar to the C
// language.
type CStyleLoop struct {
	Lparen, Rparen   token.Pos
	Init, Cond, Post ArithmExpr
}

func (c *CStyleLoop) Pos() token.Pos { return c.Lparen }
func (c *CStyleLoop) End() token.Pos { return posAdd(c.Rparen, 2) }

// UnaryExpr represents an unary expression over a node, either before
// or after it.
type UnaryExpr struct {
	OpPos token.Pos
	Op    token.Token
	Post  bool
	X     ArithmExpr
}

func (u *UnaryExpr) Pos() token.Pos {
	if u.Post {
		return u.X.Pos()
	}
	return u.OpPos
}
func (u *UnaryExpr) End() token.Pos {
	if u.Post {
		return posAdd(u.OpPos, 2)
	}
	return u.X.End()
}

// BinaryCmd represents a binary expression between two statements.
type BinaryCmd struct {
	OpPos token.Pos
	Op    token.Token
	X, Y  *Stmt
}

func (b *BinaryCmd) Pos() token.Pos { return b.X.Pos() }
func (b *BinaryCmd) End() token.Pos { return b.Y.End() }

// FuncDecl represents the declaration of a function.
type FuncDecl struct {
	Position  token.Pos
	BashStyle bool
	Name      Lit
	Body      *Stmt
}

func (f *FuncDecl) Pos() token.Pos { return f.Position }
func (f *FuncDecl) End() token.Pos { return f.Body.End() }

// Word represents a list of nodes that are contiguous to each other.
// The word is delimeted by word boundaries.
type Word struct {
	Parts []WordPart
}

func (w *Word) Pos() token.Pos { return partsFirstPos(w.Parts) }
func (w *Word) End() token.Pos { return partsLastEnd(w.Parts) }

// WordPart represents all nodes that can form a word.
type WordPart interface {
	Node
	wordPartNode()
}

func (*Lit) wordPartNode()       {}
func (*SglQuoted) wordPartNode() {}
func (*Quoted) wordPartNode()    {}
func (*ParamExp) wordPartNode()  {}
func (*CmdSubst) wordPartNode()  {}
func (*ArithmExp) wordPartNode() {}
func (*ProcSubst) wordPartNode() {}
func (*ArrayExpr) wordPartNode() {}
func (*ExtGlob) wordPartNode()   {}

// Lit represents an unquoted string consisting of characters that were
// not tokenized.
type Lit struct {
	ValuePos token.Pos
	Value    string
}

func (l *Lit) Pos() token.Pos { return l.ValuePos }
func (l *Lit) End() token.Pos { return posAddStr(l.ValuePos, l.Value) }

// SglQuoted represents a single-quoted string.
type SglQuoted struct {
	QuotePos token.Pos
	Quote    token.Token
	Value    string
}

func (q *SglQuoted) Pos() token.Pos { return q.QuotePos }
func (q *SglQuoted) End() token.Pos {
	pos := posAdd(q.QuotePos, 2+len(q.Value))
	if pos > 0 && q.Quote == token.DOLLSQ {
		pos++
	}
	return pos
}

// Quoted represents a list of nodes within double quotes.
type Quoted struct {
	QuotePos token.Pos
	Quote    token.Token
	Parts    []WordPart
}

func (q *Quoted) Pos() token.Pos { return q.QuotePos }
func (q *Quoted) End() token.Pos {
	if q.QuotePos == 0 {
		return 0
	}
	end := q.QuotePos
	if len(q.Parts) > 0 {
		end = partsLastEnd(q.Parts)
	} else if q.Quote == token.DOLLSQ || q.Quote == token.DOLLDQ {
		end += 2
	} else {
		end++
	}
	return posAdd(end, 1)
}

// CmdSubst represents a command substitution.
type CmdSubst struct {
	Left, Right token.Pos
	Backquotes  bool
	Stmts       []*Stmt
}

func (c *CmdSubst) Pos() token.Pos { return c.Left }
func (c *CmdSubst) End() token.Pos { return posAdd(c.Right, 1) }

// ParamExp represents a parameter expansion.
type ParamExp struct {
	Dollar        token.Pos
	Short, Length bool
	Param         Lit
	Ind           *Index
	Repl          *Replace
	Exp           *Expansion
}

func (p *ParamExp) Pos() token.Pos { return p.Dollar }
func (p *ParamExp) End() token.Pos {
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
	if !p.Short {
		end = posAdd(end, 1)
	}
	return end
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
	Op   token.Token
	Word Word
}

// ArithmExp represents an arithmetic expansion.
type ArithmExp struct {
	Token       token.Token
	Left, Right token.Pos
	X           ArithmExpr
}

func (a *ArithmExp) Pos() token.Pos { return a.Left }
func (a *ArithmExp) End() token.Pos { return posAdd(a.Right, 2) }

// ArithmExpr represents all nodes that form arithmetic expressions.
type ArithmExpr interface {
	Node
	arithmExprNode()
}

func (*BinaryExpr) arithmExprNode() {}
func (*UnaryExpr) arithmExprNode()  {}
func (*ParenExpr) arithmExprNode()  {}
func (*Word) arithmExprNode()       {}

// BinaryExpr represents a binary expression between two arithmetic
// expression.
type BinaryExpr struct {
	OpPos token.Pos
	Op    token.Token
	X, Y  ArithmExpr
}

func (b *BinaryExpr) Pos() token.Pos { return b.X.Pos() }
func (b *BinaryExpr) End() token.Pos { return b.Y.End() }

// ParenExpr represents an expression within parentheses inside an
// ArithmExp.
type ParenExpr struct {
	Lparen, Rparen token.Pos
	X              ArithmExpr
}

func (p *ParenExpr) Pos() token.Pos { return p.Lparen }
func (p *ParenExpr) End() token.Pos { return posAdd(p.Rparen, 1) }

// CaseClause represents a case (switch) clause.
type CaseClause struct {
	Case, Esac token.Pos
	Word       Word
	List       []*PatternList
}

func (c *CaseClause) Pos() token.Pos { return c.Case }
func (c *CaseClause) End() token.Pos { return posAdd(c.Esac, 4) }

// PatternList represents a pattern list (case) within a CaseClause.
type PatternList struct {
	Op       token.Token
	OpPos    token.Pos
	Patterns []Word
	Stmts    []*Stmt
}

// TestClause represents a Bash extended test clause.
type TestClause struct {
	Left, Right token.Pos
	X           ArithmExpr
}

func (t *TestClause) Pos() token.Pos { return t.Left }
func (t *TestClause) End() token.Pos { return posAdd(t.Right, 2) }

// DeclClause represents a Bash declare clause.
type DeclClause struct {
	Position token.Pos
	Variant  string
	Opts     []Word
	Assigns  []*Assign
}

func (d *DeclClause) Pos() token.Pos { return d.Position }
func (d *DeclClause) End() token.Pos {
	end := wordLastEnd(d.Opts)
	if len(d.Assigns) > 0 {
		assignEnd := d.Assigns[len(d.Assigns)-1].End()
		end = posMax(end, assignEnd)
	}
	return end
}

// ArrayExpr represents a Bash array expression.
type ArrayExpr struct {
	Lparen, Rparen token.Pos
	List           []Word
}

func (a *ArrayExpr) Pos() token.Pos { return a.Lparen }
func (a *ArrayExpr) End() token.Pos { return posAdd(a.Rparen, 1) }

// ExtGlob represents a Bash extended globbing expression. Note that
// these are parsed independently of whether shopt has been called or
// not.
type ExtGlob struct {
	Token   token.Token
	Pattern Lit
}

func (e *ExtGlob) Pos() token.Pos { return posAdd(e.Pattern.Pos(), -2) }
func (e *ExtGlob) End() token.Pos { return posAdd(e.Pattern.End(), 1) }

// ProcSubst represents a Bash process substitution.
type ProcSubst struct {
	OpPos, Rparen token.Pos
	Op            token.Token
	Stmts         []*Stmt
}

func (s *ProcSubst) Pos() token.Pos { return s.OpPos }
func (s *ProcSubst) End() token.Pos { return posAdd(s.Rparen, 1) }

// EvalClause represents a Bash eval clause.
type EvalClause struct {
	Eval token.Pos
	Stmt *Stmt
}

func (e *EvalClause) Pos() token.Pos { return e.Eval }
func (e *EvalClause) End() token.Pos {
	if e.Stmt == nil {
		return posAdd(e.Eval, 4)
	}
	return e.Stmt.End()
}

// CoprocClause represents a Bash coproc clause.
type CoprocClause struct {
	Coproc token.Pos
	Name   *Lit
	Stmt   *Stmt
}

func (c *CoprocClause) Pos() token.Pos { return c.Coproc }
func (c *CoprocClause) End() token.Pos { return c.Stmt.End() }

// LetClause represents a Bash let clause.
type LetClause struct {
	Let   token.Pos
	Exprs []ArithmExpr
}

func (l *LetClause) Pos() token.Pos { return l.Let }
func (l *LetClause) End() token.Pos { return l.Exprs[len(l.Exprs)-1].End() }

func posAdd(pos token.Pos, n int) token.Pos {
	if pos == internal.DefaultPos {
		return pos
	}
	return pos + token.Pos(n)
}

func posAddStr(pos token.Pos, s string) token.Pos {
	return posAdd(pos, len(s))
}

func stmtFirstPos(sts []*Stmt) token.Pos {
	if len(sts) == 0 {
		return internal.DefaultPos
	}
	return sts[0].Pos()
}

func stmtLastEnd(sts []*Stmt) token.Pos {
	if len(sts) == 0 {
		return internal.DefaultPos
	}
	return sts[len(sts)-1].End()
}

func partsFirstPos(ps []WordPart) token.Pos {
	if len(ps) == 0 {
		return internal.DefaultPos
	}
	return ps[0].Pos()
}

func partsLastEnd(ps []WordPart) token.Pos {
	if len(ps) == 0 {
		return internal.DefaultPos
	}
	return ps[len(ps)-1].End()
}

func wordLastEnd(ws []Word) token.Pos {
	if len(ws) == 0 {
		return internal.DefaultPos
	}
	return ws[len(ws)-1].End()
}
