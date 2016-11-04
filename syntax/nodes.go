// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

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

	Stmts    []*Stmt
	Comments []*Comment

	// Lines contains the offset of the first character for each
	// line (the first entry is always 0)
	Lines []int
}

func (f *File) Pos() Pos { return stmtFirstPos(f.Stmts) }
func (f *File) End() Pos { return stmtLastEnd(f.Stmts) }

func (f *File) Position(p Pos) (pos Position) {
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

func posMax(p1, p2 Pos) Pos {
	if p2 > p1 {
		return p2
	}
	return p1
}

// Comment represents a single comment on a single line.
type Comment struct {
	Hash Pos
	Text string
}

func (c *Comment) Pos() Pos { return c.Hash }
func (c *Comment) End() Pos { return posAddStr(c.Hash, c.Text) }

// Stmt represents a statement, otherwise known as a compound command.
// It is compromised of a command and other components that may come
// before or after it.
type Stmt struct {
	Cmd        Command
	Position   Pos
	SemiPos    Pos
	Negated    bool
	Background bool
	Assigns    []*Assign
	Redirs     []*Redirect
}

func (s *Stmt) Pos() Pos { return s.Position }
func (s *Stmt) End() Pos {
	if s.SemiPos > 0 {
		return s.SemiPos + 1
	}
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
func (*ArithmCmd) commandNode()    {}
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

func (a *Assign) Pos() Pos {
	if a.Name == nil {
		return a.Value.Pos()
	}
	return a.Name.Pos()
}

func (a *Assign) End() Pos {
	if a.Name != nil {
		return posMax(a.Name.End(), a.Value.End())
	}
	return a.Value.End()
}

// Redirect represents an input/output redirection.
type Redirect struct {
	OpPos      Pos
	Op         RedirOperator
	N          *Lit
	Word, Hdoc Word
}

func (r *Redirect) Pos() Pos {
	if r.N != nil {
		return r.N.Pos()
	}
	return r.OpPos
}
func (r *Redirect) End() Pos { return r.Word.End() }

// CallExpr represents a command execution or function call.
type CallExpr struct {
	Args []Word
}

func (c *CallExpr) Pos() Pos { return c.Args[0].Pos() }
func (c *CallExpr) End() Pos { return c.Args[len(c.Args)-1].End() }

// Subshell represents a series of commands that should be executed in a
// nested shell environment.
type Subshell struct {
	Lparen, Rparen Pos
	Stmts          []*Stmt
}

func (s *Subshell) Pos() Pos { return s.Lparen }
func (s *Subshell) End() Pos { return posAdd(s.Rparen, 1) }

// Block represents a series of commands that should be executed in a
// nested scope.
type Block struct {
	Lbrace, Rbrace Pos
	Stmts          []*Stmt
}

func (b *Block) Pos() Pos { return b.Rbrace }
func (b *Block) End() Pos { return posAdd(b.Rbrace, 1) }

// IfClause represents an if statement.
type IfClause struct {
	If, Then, Fi Pos
	CondStmts    []*Stmt
	ThenStmts    []*Stmt
	Elifs        []*Elif
	Else         Pos
	ElseStmts    []*Stmt
}

func (c *IfClause) Pos() Pos { return c.If }
func (c *IfClause) End() Pos { return posAdd(c.Fi, 2) }

// Elif represents an "else if" case in an if clause.
type Elif struct {
	Elif, Then Pos
	CondStmts  []*Stmt
	ThenStmts  []*Stmt
}

// WhileClause represents a while clause.
type WhileClause struct {
	While, Do, Done Pos
	CondStmts       []*Stmt
	DoStmts         []*Stmt
}

func (w *WhileClause) Pos() Pos { return w.While }
func (w *WhileClause) End() Pos { return posAdd(w.Done, 4) }

// UntilClause represents an until clause.
type UntilClause struct {
	Until, Do, Done Pos
	CondStmts       []*Stmt
	DoStmts         []*Stmt
}

func (u *UntilClause) Pos() Pos { return u.Until }
func (u *UntilClause) End() Pos { return posAdd(u.Done, 4) }

// ForClause represents a for clause.
type ForClause struct {
	For, Do, Done Pos
	Loop          Loop
	DoStmts       []*Stmt
}

func (f *ForClause) Pos() Pos { return f.For }
func (f *ForClause) End() Pos { return posAdd(f.Done, 4) }

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

func (w *WordIter) Pos() Pos { return w.Name.Pos() }
func (w *WordIter) End() Pos { return posMax(w.Name.End(), wordLastEnd(w.List)) }

// CStyleLoop represents the behaviour of a for clause similar to the C
// language.
//
// This node will never appear when in PosixConformant mode.
type CStyleLoop struct {
	Lparen, Rparen   Pos
	Init, Cond, Post ArithmExpr
}

func (c *CStyleLoop) Pos() Pos { return c.Lparen }
func (c *CStyleLoop) End() Pos { return posAdd(c.Rparen, 2) }

// BinaryCmd represents a binary expression between two statements.
type BinaryCmd struct {
	OpPos Pos
	Op    BinCmdOperator
	X, Y  *Stmt
}

func (b *BinaryCmd) Pos() Pos { return b.X.Pos() }
func (b *BinaryCmd) End() Pos { return b.Y.End() }

// FuncDecl represents the declaration of a function.
type FuncDecl struct {
	Position  Pos
	BashStyle bool
	Name      Lit
	Body      *Stmt
}

func (f *FuncDecl) Pos() Pos { return f.Position }
func (f *FuncDecl) End() Pos { return f.Body.End() }

// Word represents a list of nodes that are contiguous to each other.
// The word is delimeted by word boundaries.
type Word struct {
	Parts []WordPart
}

func (w *Word) Pos() Pos { return partsFirstPos(w.Parts) }
func (w *Word) End() Pos { return partsLastEnd(w.Parts) }

// WordPart represents all nodes that can form a word.
type WordPart interface {
	Node
	wordPartNode()
}

func (*Lit) wordPartNode()       {}
func (*SglQuoted) wordPartNode() {}
func (*DblQuoted) wordPartNode() {}
func (*ParamExp) wordPartNode()  {}
func (*CmdSubst) wordPartNode()  {}
func (*ArithmExp) wordPartNode() {}
func (*ProcSubst) wordPartNode() {}
func (*ArrayExpr) wordPartNode() {}
func (*ExtGlob) wordPartNode()   {}

// Lit represents an unquoted string consisting of characters that were
// not tokenized.
type Lit struct {
	ValuePos Pos
	Value    string
}

func (l *Lit) Pos() Pos { return l.ValuePos }
func (l *Lit) End() Pos { return posAddStr(l.ValuePos, l.Value) }

// SglQuoted represents a string within single quotes.
type SglQuoted struct {
	Position Pos
	Dollar   bool
	Value    string
}

func (q *SglQuoted) Pos() Pos { return q.Position }
func (q *SglQuoted) End() Pos {
	pos := posAdd(q.Position, 2+len(q.Value))
	if pos > 0 && q.Dollar {
		pos++
	}
	return pos
}

// DblQuoted represents a list of nodes within double quotes.
type DblQuoted struct {
	Position Pos
	Dollar   bool
	Parts    []WordPart
}

func (q *DblQuoted) Pos() Pos { return q.Position }
func (q *DblQuoted) End() Pos {
	if q.Position == 0 {
		return defaultPos
	}
	end := q.Position
	if len(q.Parts) > 0 {
		end = partsLastEnd(q.Parts)
	} else if q.Dollar {
		end += 2
	} else {
		end++
	}
	return posAdd(end, 1)
}

// CmdSubst represents a command substitution.
type CmdSubst struct {
	Left, Right Pos
	Stmts       []*Stmt
}

func (c *CmdSubst) Pos() Pos { return c.Left }
func (c *CmdSubst) End() Pos { return posAdd(c.Right, 1) }

// ParamExp represents a parameter expansion.
type ParamExp struct {
	Dollar, Rbrace Pos
	Short, Length  bool
	Param          Lit
	Ind            *Index
	Slice          *Slice
	Repl           *Replace
	Exp            *Expansion
}

func (p *ParamExp) Pos() Pos { return p.Dollar }
func (p *ParamExp) End() Pos {
	if p.Rbrace > 0 {
		return p.Rbrace + 1
	}
	return p.Param.End()
}

// Index represents access to an array via an index inside a ParamExp.
//
// This node will never appear when in PosixConformant mode.
type Index struct {
	Word Word
}

// Slice represents character slicing inside a ParamExp.
//
// This node will never appear when in PosixConformant mode.
type Slice struct {
	Offset, Length Word
}

// Replace represents a search and replace inside a ParamExp.
type Replace struct {
	All        bool
	Orig, With Word
}

// Expansion represents string manipulation in a ParamExp other than
// those covered by Replace.
type Expansion struct {
	Op   ParExpOperator
	Word Word
}

// ArithmExp represents an arithmetic expansion.
type ArithmExp struct {
	Left, Right Pos
	Bracket     bool
	X           ArithmExpr
}

func (a *ArithmExp) Pos() Pos { return a.Left }
func (a *ArithmExp) End() Pos {
	if a.Bracket {
		return posAdd(a.Right, 1)
	}
	return posAdd(a.Right, 2)
}

// ArithmCmd represents an arithmetic command.
//
// This node will never appear when in PosixConformant mode.
type ArithmCmd struct {
	Left, Right Pos
	X           ArithmExpr
}

func (a *ArithmCmd) Pos() Pos { return a.Left }
func (a *ArithmCmd) End() Pos { return posAdd(a.Right, 2) }

// ArithmExpr represents all nodes that form arithmetic expressions.
type ArithmExpr interface {
	Node
	arithmExprNode()
}

func (*BinaryArithm) arithmExprNode() {}
func (*UnaryArithm) arithmExprNode()  {}
func (*ParenArithm) arithmExprNode()  {}
func (*Word) arithmExprNode()         {}

// BinaryArithm represents a binary expression between two arithmetic
// expression.
type BinaryArithm struct {
	OpPos Pos
	Op    Token
	X, Y  ArithmExpr
}

func (b *BinaryArithm) Pos() Pos { return b.X.Pos() }
func (b *BinaryArithm) End() Pos { return b.Y.End() }

// UnaryArithm represents an unary expression over a node, either before
// or after it.
type UnaryArithm struct {
	OpPos Pos
	Op    Token
	Post  bool
	X     ArithmExpr
}

func (u *UnaryArithm) Pos() Pos {
	if u.Post {
		return u.X.Pos()
	}
	return u.OpPos
}

func (u *UnaryArithm) End() Pos {
	if u.Post {
		return posAdd(u.OpPos, 2)
	}
	return u.X.End()
}

// ParenArithm represents an expression within parentheses inside an
// ArithmExp.
type ParenArithm struct {
	Lparen, Rparen Pos
	X              ArithmExpr
}

func (p *ParenArithm) Pos() Pos { return p.Lparen }
func (p *ParenArithm) End() Pos { return posAdd(p.Rparen, 1) }

// CaseClause represents a case (switch) clause.
type CaseClause struct {
	Case, Esac Pos
	Word       Word
	List       []*PatternList
}

func (c *CaseClause) Pos() Pos { return c.Case }
func (c *CaseClause) End() Pos { return posAdd(c.Esac, 4) }

// PatternList represents a pattern list (case) within a CaseClause.
type PatternList struct {
	Op       CaseOperator
	OpPos    Pos
	Patterns []Word
	Stmts    []*Stmt
}

// TestClause represents a Bash extended test clause.
//
// This node will never appear when in PosixConformant mode.
type TestClause struct {
	Left, Right Pos
	X           TestExpr
}

func (t *TestClause) Pos() Pos { return t.Left }
func (t *TestClause) End() Pos { return posAdd(t.Right, 2) }

// TestExpr represents all nodes that form arithmetic expressions.
type TestExpr interface {
	Node
	testExprNode()
}

func (*BinaryTest) testExprNode() {}
func (*UnaryTest) testExprNode()  {}
func (*ParenTest) testExprNode()  {}
func (*Word) testExprNode()       {}

// BinaryTest represents a binary expression between two arithmetic
// expression.
type BinaryTest struct {
	OpPos Pos
	Op    BinTestOperator
	X, Y  TestExpr
}

func (b *BinaryTest) Pos() Pos { return b.X.Pos() }
func (b *BinaryTest) End() Pos { return b.Y.End() }

// UnaryTest represents an unary expression over a node, either before
// or after it.
type UnaryTest struct {
	OpPos Pos
	Op    UnTestOperator
	X     TestExpr
}

func (u *UnaryTest) Pos() Pos { return u.OpPos }
func (u *UnaryTest) End() Pos { return u.X.End() }

// ParenTest represents an expression within parentheses inside an
// TestExp.
type ParenTest struct {
	Lparen, Rparen Pos
	X              TestExpr
}

func (p *ParenTest) Pos() Pos { return p.Lparen }
func (p *ParenTest) End() Pos { return posAdd(p.Rparen, 1) }

// DeclClause represents a Bash declare clause.
//
// This node will never appear when in PosixConformant mode.
type DeclClause struct {
	Position Pos
	Variant  string
	Opts     []Word
	Assigns  []*Assign
}

func (d *DeclClause) Pos() Pos { return d.Position }
func (d *DeclClause) End() Pos {
	end := wordLastEnd(d.Opts)
	if len(d.Assigns) > 0 {
		assignEnd := d.Assigns[len(d.Assigns)-1].End()
		end = posMax(end, assignEnd)
	}
	return end
}

// ArrayExpr represents a Bash array expression.
//
// This node will never appear when in PosixConformant mode.
type ArrayExpr struct {
	Lparen, Rparen Pos
	List           []Word
}

func (a *ArrayExpr) Pos() Pos { return a.Lparen }
func (a *ArrayExpr) End() Pos { return posAdd(a.Rparen, 1) }

// ExtGlob represents a Bash extended globbing expression. Note that
// these are parsed independently of whether shopt has been called or
// not.
//
// This node will never appear when in PosixConformant mode.
type ExtGlob struct {
	Op      GlobOperator
	Pattern Lit
}

func (e *ExtGlob) Pos() Pos { return posAdd(e.Pattern.Pos(), -2) }
func (e *ExtGlob) End() Pos { return posAdd(e.Pattern.End(), 1) }

// ProcSubst represents a Bash process substitution.
//
// This node will never appear when in PosixConformant mode.
type ProcSubst struct {
	OpPos, Rparen Pos
	Op            ProcOperator
	Stmts         []*Stmt
}

func (s *ProcSubst) Pos() Pos { return s.OpPos }
func (s *ProcSubst) End() Pos { return posAdd(s.Rparen, 1) }

// EvalClause represents a Bash eval clause.
//
// This node will never appear when in PosixConformant mode.
type EvalClause struct {
	Eval Pos
	Stmt *Stmt
}

func (e *EvalClause) Pos() Pos { return e.Eval }
func (e *EvalClause) End() Pos {
	if e.Stmt == nil {
		return posAdd(e.Eval, 4)
	}
	return e.Stmt.End()
}

// CoprocClause represents a Bash coproc clause.
//
// This node will never appear when in PosixConformant mode.
type CoprocClause struct {
	Coproc Pos
	Name   *Lit
	Stmt   *Stmt
}

func (c *CoprocClause) Pos() Pos { return c.Coproc }
func (c *CoprocClause) End() Pos { return c.Stmt.End() }

// LetClause represents a Bash let clause.
//
// This node will never appear when in PosixConformant mode.
type LetClause struct {
	Let   Pos
	Exprs []ArithmExpr
}

func (l *LetClause) Pos() Pos { return l.Let }
func (l *LetClause) End() Pos { return l.Exprs[len(l.Exprs)-1].End() }

func posAdd(pos Pos, n int) Pos {
	if pos == defaultPos {
		return pos
	}
	return pos + Pos(n)
}

func posAddStr(pos Pos, s string) Pos {
	return posAdd(pos, len(s))
}

func stmtFirstPos(sts []*Stmt) Pos {
	if len(sts) == 0 {
		return defaultPos
	}
	return sts[0].Pos()
}

func stmtLastEnd(sts []*Stmt) Pos {
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

func wordLastEnd(ws []Word) Pos {
	if len(ws) == 0 {
		return defaultPos
	}
	return ws[len(ws)-1].End()
}
