// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"bufio"
	"bytes"
	"io"
	"sync"
)

// PrintConfig controls how the printing of an AST node will behave.
type PrintConfig struct {
	Spaces int // 0 (default) for tabs, >0 for number of spaces
}

var writerFree = sync.Pool{
	New: func() interface{} { return bufio.NewWriter(nil) },
}

// Fprint "pretty-prints" the given AST file to the given writer.
func (c PrintConfig) Fprint(w io.Writer, f *File) error {
	bw := writerFree.Get().(*bufio.Writer)
	bw.Reset(w)
	p := printer{
		Writer:   bw,
		f:        f,
		comments: f.Comments,
		c:        c,
	}
	p.stmts(f.Stmts)
	p.commentsUpTo(0)
	p.newline()
	err := bw.Flush()
	writerFree.Put(bw)
	return err
}

const maxPos = Pos(^uint(0) >> 1)

func (p *printer) incLine() {
	p.nlineIndex++
	if p.nlineIndex >= len(p.f.lines) {
		p.nline = maxPos
	} else {
		p.nline = Pos(p.f.lines[p.nlineIndex])
	}
}

func (p *printer) incLines(pos Pos) {
	for p.nline < pos {
		p.incLine()
	}
}

// Fprint "pretty-prints" the given AST file to the given writer. It
// calls PrintConfig.Fprint with its default settings.
func Fprint(w io.Writer, f *File) error {
	return PrintConfig{}.Fprint(w, f)
}

type printer struct {
	*bufio.Writer

	f *File
	c PrintConfig

	wantSpace   bool
	wantNewline bool
	wantSpaces  int

	// nline is the position of the next newline
	nline      Pos
	nlineIndex int
	// lastLevel is the last level of indentation that was used.
	lastLevel int
	// level is the current level of indentation.
	level int
	// levelIncs records which indentation level increments actually
	// took place, to revert them once their section ends.
	levelIncs []bool

	nestedBinary bool

	// comments is the list of pending comments to write.
	comments []*Comment

	// pendingHdocs is the list of pending heredocs to write.
	pendingHdocs []*Redirect
}

func (p *printer) space() {
	p.WriteByte(' ')
	p.wantSpace = false
}

func (p *printer) spaces(n int) {
	for i := 0; i < n; i++ {
		p.WriteByte(' ')
	}
}

func (p *printer) tabs(n int) {
	for i := 0; i < n; i++ {
		p.WriteByte('\t')
	}
}

func (p *printer) bslashNewl() {
	p.WriteString(" \\\n")
	p.wantSpace = false
	p.incLine()
}

func (p *printer) spacedRsrv(s string) {
	if p.wantSpace {
		p.WriteByte(' ')
	}
	p.WriteString(s)
	p.wantSpace = true
}

func (p *printer) spacedTok(s string, spaceAfter bool) {
	if p.wantSpace {
		p.WriteByte(' ')
	}
	p.wantSpace = spaceAfter
	p.WriteString(s)
}

func (p *printer) semiOrNewl(s string, pos Pos) {
	if p.wantNewline {
		p.newline()
		p.indent()
	} else {
		p.WriteString("; ")
	}
	p.incLines(pos)
	p.WriteString(s)
	p.wantSpace = true
}

func (p *printer) incLevel() {
	inc := false
	if p.level <= p.lastLevel {
		p.level++
		inc = true
	} else if last := &p.levelIncs[len(p.levelIncs)-1]; *last {
		*last = false
		inc = true
	}
	p.levelIncs = append(p.levelIncs, inc)
}

func (p *printer) decLevel() {
	if p.levelIncs[len(p.levelIncs)-1] {
		p.level--
	}
	p.levelIncs = p.levelIncs[:len(p.levelIncs)-1]
}

func (p *printer) indent() {
	p.lastLevel = p.level
	switch {
	case p.level == 0:
	case p.c.Spaces == 0:
		p.tabs(p.level)
	case p.c.Spaces > 0:
		p.spaces(p.c.Spaces * p.level)
	}
}

func (p *printer) newline() {
	p.wantNewline, p.wantSpace = false, false
	p.WriteByte('\n')
	p.incLine()
	for _, r := range p.pendingHdocs {
		p.word(r.Hdoc)
		p.incLines(r.Hdoc.End() + 1)
		p.unquotedWord(&r.Word)
		p.WriteByte('\n')
		p.incLine()
		p.wantSpace = false
	}
	p.pendingHdocs = nil
}

func (p *printer) newlines(pos Pos) {
	p.newline()
	if pos > p.nline {
		// preserve single empty lines
		p.WriteByte('\n')
		p.incLine()
	}
	p.indent()
}

func (p *printer) didSeparate(pos Pos) bool {
	p.commentsUpTo(pos)
	if p.wantNewline || pos > p.nline {
		p.newlines(pos)
		return true
	}
	return false
}

func (p *printer) sepTok(s string, pos Pos) {
	p.level++
	p.commentsUpTo(pos)
	p.level--
	p.didSeparate(pos)
	p.WriteString(s)
	p.wantSpace = true
}

func (p *printer) semiRsrv(s string, pos Pos, fallback bool) {
	p.level++
	p.commentsUpTo(pos)
	p.level--
	if !p.didSeparate(pos) && fallback {
		p.WriteString("; ")
	} else if p.wantSpace {
		p.WriteByte(' ')
	}
	p.WriteString(s)
	p.wantSpace = true
}

func (p *printer) hasInline(pos, nline Pos) bool {
	if len(p.comments) < 1 {
		return false
	}
	for _, c := range p.comments {
		if c.Hash > nline {
			return false
		}
		if c.Hash > pos {
			return true
		}
	}
	return false
}

func (p *printer) commentsUpTo(pos Pos) {
	if len(p.comments) < 1 {
		return
	}
	c := p.comments[0]
	if pos > 0 && c.Hash >= pos {
		return
	}
	p.wantNewline = false
	if p.nlineIndex == 0 {
		p.incLines(c.Hash)
	} else if p.wantNewline || c.Hash > p.nline {
		p.newlines(c.Hash)
	} else {
		p.spaces(p.wantSpaces + 1)
	}
	p.WriteByte('#')
	p.WriteString(c.Text)
	p.comments = p.comments[1:]
	p.commentsUpTo(pos)
}

func quotedOp(tok Token) string {
	switch tok {
	case DQUOTE:
		return `"`
	case DOLLSQ:
		return `$'`
	case SQUOTE:
		return `'`
	default: // DOLLDQ
		return `$"`
	}
}

func expansionOp(tok Token) string {
	switch tok {
	case COLON:
		return ":"
	case ADD:
		return "+"
	case CADD:
		return ":+"
	case SUB:
		return "-"
	case CSUB:
		return ":-"
	case QUEST:
		return "?"
	case CQUEST:
		return ":?"
	case ASSIGN:
		return "="
	case CASSIGN:
		return ":="
	case REM:
		return "%"
	case DREM:
		return "%%"
	case HASH:
		return "#"
	default: // DHASH
		return "##"
	}
}

func (p *printer) wordPart(wp WordPart) {
	switch x := wp.(type) {
	case *Lit:
		p.WriteString(x.Value)
	case *SglQuoted:
		p.WriteByte('\'')
		p.WriteString(x.Value)
		p.WriteByte('\'')
	case *Quoted:
		p.WriteString(quotedOp(x.Quote))
		for i, n := range x.Parts {
			p.wordPart(n)
			if i == len(x.Parts)-1 {
				p.incLines(n.End())
			}
		}
		p.WriteString(quotedOp(quotedStop(x.Quote)))
	case *CmdSubst:
		p.incLines(x.Pos())
		p.wantSpace = false
		if x.Backquotes {
			p.WriteByte('`')
		} else {
			p.WriteString("$(")
		}
		if startsWithLparen(x.Stmts) {
			p.space()
		}
		p.nestedStmts(x.Stmts)
		if x.Backquotes {
			p.wantSpace = false
			p.sepTok("`", x.Right)
		} else {
			p.sepTok(")", x.Right)
		}
	case *ParamExp:
		if x.Short {
			p.WriteByte('$')
			p.WriteString(x.Param.Value)
			break
		}
		p.WriteString("${")
		if x.Length {
			p.WriteByte('#')
		}
		p.WriteString(x.Param.Value)
		if x.Ind != nil {
			p.WriteByte('[')
			p.word(x.Ind.Word)
			p.WriteByte(']')
		}
		if x.Repl != nil {
			if x.Repl.All {
				p.WriteByte('/')
			}
			p.WriteByte('/')
			p.word(x.Repl.Orig)
			p.WriteByte('/')
			p.word(x.Repl.With)
		} else if x.Exp != nil {
			p.WriteString(expansionOp(x.Exp.Op))
			p.word(x.Exp.Word)
		}
		p.WriteByte('}')
	case *ArithmExp:
		p.WriteString("$((")
		p.arithm(x.X, false)
		p.WriteString("))")
	case *ArrayExpr:
		p.wantSpace = false
		p.WriteByte('(')
		p.wordJoin(x.List, false)
		p.sepTok(")", x.Rparen)
	case *ProcSubst:
		// avoid conflict with << and others
		if p.wantSpace {
			p.space()
		}
		switch x.Op {
		case CMDIN:
			p.WriteString("<(")
		case CMDOUT:
			p.WriteString(">(")
		}
		p.nestedStmts(x.Stmts)
		p.WriteByte(')')
	}
	p.wantSpace = true
}

func (p *printer) cond(cond Cond) {
	switch x := cond.(type) {
	case *StmtCond:
		p.nestedStmts(x.Stmts)
	case *CStyleCond:
		p.spacedTok("((", false)
		p.arithm(x.X, false)
		p.WriteString("))")
	}
}

func (p *printer) loop(loop Loop) {
	switch x := loop.(type) {
	case *WordIter:
		p.WriteString(x.Name.Value)
		if len(x.List) > 0 {
			p.WriteString(" in")
			p.wordJoin(x.List, true)
		}
	case *CStyleLoop:
		p.WriteString("((")
		p.arithm(x.Init, false)
		p.WriteString("; ")
		p.arithm(x.Cond, false)
		p.WriteString("; ")
		p.arithm(x.Post, false)
		p.WriteString("))")
	}
}

func binaryExprOp(tok Token) string {
	switch tok {
	case ASSIGN:
		return "="
	case ADD:
		return "+"
	case SUB:
		return "-"
	case REM:
		return "%"
	case MUL:
		return "*"
	case QUO:
		return "/"
	case AND:
		return "&"
	case OR:
		return "|"
	case LAND:
		return "&&"
	case LOR:
		return "||"
	case XOR:
		return "^"
	case POW:
		return "**"
	case EQL:
		return "=="
	case NEQ:
		return "!="
	case LEQ:
		return "<="
	case GEQ:
		return ">="
	case ADDASSGN:
		return "+="
	case SUBASSGN:
		return "-="
	case MULASSGN:
		return "*="
	case QUOASSGN:
		return "/="
	case REMASSGN:
		return "%="
	case ANDASSGN:
		return "&="
	case ORASSGN:
		return "|="
	case XORASSGN:
		return "^="
	case SHLASSGN:
		return "<<="
	case SHRASSGN:
		return ">>="
	case LSS:
		return "<"
	case GTR:
		return ">"
	case SHL:
		return "<<"
	case SHR:
		return ">>"
	case QUEST:
		return "?"
	case COLON:
		return ":"
	default: // COMMA
		return ","
	}
}

func unaryExprOp(tok Token) string {
	switch tok {
	case ADD:
		return "+"
	case SUB:
		return "-"
	case NOT:
		return "!"
	case INC:
		return "++"
	default: // DEC
		return "--"
	}
}
func (p *printer) arithm(expr ArithmExpr, compact bool) {
	p.wantSpace = false
	switch x := expr.(type) {
	case *Word:
		p.spacedWord(*x)
	case *BinaryExpr:
		if compact {
			p.arithm(x.X, true)
			p.WriteString(binaryExprOp(x.Op))
			p.arithm(x.Y, true)
		} else {
			p.arithm(x.X, false)
			if x.Op != COMMA {
				p.WriteByte(' ')
			}
			p.WriteString(binaryExprOp(x.Op))
			p.space()
			p.arithm(x.Y, false)
		}
	case *UnaryExpr:
		if x.Post {
			p.arithm(x.X, compact)
			p.WriteString(unaryExprOp(x.Op))
		} else {
			p.WriteString(unaryExprOp(x.Op))
			p.arithm(x.X, compact)
		}
	case *ParenExpr:
		p.WriteByte('(')
		p.arithm(x.X, false)
		p.WriteByte(')')
	}
}

func (p *printer) word(w Word) {
	for _, n := range w.Parts {
		p.wordPart(n)
	}
}

func (p *printer) unquotedWord(w *Word) {
	for _, wp := range w.Parts {
		switch x := wp.(type) {
		case *SglQuoted:
			p.WriteString(x.Value)
		case *Quoted:
			for _, qp := range x.Parts {
				p.wordPart(qp)
			}
		case *Lit:
			if x.Value[0] == '\\' {
				p.WriteString(x.Value[1:])
			} else {
				p.WriteString(x.Value)
			}
		default:
			p.wordPart(wp)
		}
	}
}

func (p *printer) spacedWord(w Word) {
	if p.wantSpace {
		p.WriteByte(' ')
	}
	for _, n := range w.Parts {
		p.wordPart(n)
	}
}

func (p *printer) wordJoin(ws []Word, backslash bool) {
	anyNewline := false
	for _, w := range ws {
		if w.Pos() > p.nline {
			if backslash {
				p.bslashNewl()
			} else {
				p.WriteByte('\n')
				p.incLine()
			}
			if !anyNewline {
				p.incLevel()
				anyNewline = true
			}
			p.indent()
		} else if p.wantSpace {
			p.space()
		}
		for _, n := range w.Parts {
			p.wordPart(n)
		}
	}
	if anyNewline {
		p.decLevel()
	}
}

func (p *printer) stmt(s *Stmt) {
	if s.Negated {
		p.spacedRsrv("!")
	}
	p.assigns(s.Assigns)
	startRedirs := p.command(s.Cmd, s.Redirs)
	anyNewline := false
	for _, r := range s.Redirs[startRedirs:] {
		if r.OpPos > p.nline {
			p.bslashNewl()
			if !anyNewline {
				p.incLevel()
				anyNewline = true
			}
			p.indent()
		}
		p.didSeparate(r.OpPos)
		if p.wantSpace {
			p.WriteByte(' ')
		}
		if r.N != nil {
			p.WriteString(r.N.Value)
		}
		p.WriteString(redirectOp(r.Op))
		p.wantSpace = true
		p.word(r.Word)
		if r.Op == SHL || r.Op == DHEREDOC {
			p.pendingHdocs = append(p.pendingHdocs, r)
		}
	}
	if anyNewline {
		p.decLevel()
	}
	if s.Background {
		p.WriteString(" &")
	}
}

func redirectOp(tok Token) string {
	switch tok {
	case LSS:
		return "<"
	case GTR:
		return ">"
	case SHL:
		return "<<"
	case SHR:
		return ">>"
	case RDRINOUT:
		return "<>"
	case DPLIN:
		return "<&"
	case DPLOUT:
		return ">&"
	case DHEREDOC:
		return "<<-"
	case WHEREDOC:
		return "<<<"
	case RDRALL:
		return "&>"
	default: // APPALL
		return "&>>"
	}
}

func binaryCmdOp(tok Token) string {
	switch tok {
	case OR:
		return "|"
	case LAND:
		return "&&"
	case LOR:
		return "||"
	default: // PIPEALL
		return "|&"
	}
}

func caseClauseOp(tok Token) string {
	switch tok {
	case DSEMICOLON:
		return ";;"
	case SEMIFALL:
		return ";&"
	default: // DSEMIFALL
		return ";;&"
	}
}

func (p *printer) command(cmd Command, redirs []*Redirect) (startRedirs int) {
	switch x := cmd.(type) {
	case *CallExpr:
		if len(x.Args) <= 1 {
			p.wordJoin(x.Args, true)
			return 0
		}
		p.wordJoin(x.Args[:1], true)
		for _, r := range redirs {
			if r.Pos() > x.Args[1].Pos() {
				break
			}
			if r.Op == SHL || r.Op == DHEREDOC {
				break
			}
			if p.wantSpace {
				p.space()
			}
			if r.N != nil {
				p.WriteString(r.N.Value)
			}
			p.WriteString(redirectOp(r.Op))
			p.wantSpace = true
			p.word(r.Word)
			startRedirs++
		}
		p.wordJoin(x.Args[1:], true)
	case *Block:
		p.spacedRsrv("{")
		p.nestedStmts(x.Stmts)
		p.semiRsrv("}", x.Rbrace, true)
	case *IfClause:
		p.spacedRsrv("if")
		p.cond(x.Cond)
		p.semiOrNewl("then", x.Then)
		p.nestedStmts(x.ThenStmts)
		for _, el := range x.Elifs {
			p.semiRsrv("elif", el.Elif, true)
			p.cond(el.Cond)
			p.semiOrNewl("then", el.Then)
			p.nestedStmts(el.ThenStmts)
		}
		if len(x.ElseStmts) > 0 {
			p.semiRsrv("else", x.Else, true)
			p.nestedStmts(x.ElseStmts)
		} else if x.Else > 0 {
			p.incLines(x.Else)
		}
		p.semiRsrv("fi", x.Fi, true)
	case *Subshell:
		p.spacedTok("(", false)
		if startsWithLparen(x.Stmts) {
			p.WriteByte(' ')
		}
		p.nestedStmts(x.Stmts)
		p.sepTok(")", x.Rparen)
	case *WhileClause:
		p.spacedRsrv("while")
		p.cond(x.Cond)
		p.semiOrNewl("do", x.Do)
		p.nestedStmts(x.DoStmts)
		p.semiRsrv("done", x.Done, true)
	case *ForClause:
		p.spacedRsrv("for ")
		p.loop(x.Loop)
		p.semiOrNewl("do", x.Do)
		p.nestedStmts(x.DoStmts)
		p.semiRsrv("done", x.Done, true)
	case *BinaryCmd:
		p.stmt(x.X)
		indent := !p.nestedBinary
		if indent {
			p.incLevel()
		}
		_, p.nestedBinary = x.Y.Cmd.(*BinaryCmd)
		if len(p.pendingHdocs) > 0 {
		} else if x.Y.Pos() > p.nline {
			p.bslashNewl()
			p.indent()
		}
		p.spacedTok(binaryCmdOp(x.Op), true)
		p.incLines(x.Y.Pos())
		p.stmt(x.Y)
		if indent {
			p.decLevel()
		}
		p.nestedBinary = false
	case *FuncDecl:
		if x.BashStyle {
			p.WriteString("function ")
		}
		p.WriteString(x.Name.Value)
		p.WriteString("() ")
		p.incLines(x.Body.Pos())
		p.stmt(x.Body)
	case *CaseClause:
		p.spacedRsrv("case ")
		p.word(x.Word)
		p.WriteString(" in")
		p.incLevel()
		for _, pl := range x.List {
			p.didSeparate(pl.Patterns[0].Pos())
			for i, w := range pl.Patterns {
				if i > 0 {
					p.spacedTok("|", true)
				}
				p.spacedWord(w)
			}
			p.WriteByte(')')
			sep := len(pl.Stmts) > 1 || (len(pl.Stmts) > 0 && pl.Stmts[0].Pos() > p.nline)
			p.nestedStmts(pl.Stmts)
			p.level++
			if sep {
				p.sepTok(caseClauseOp(pl.Op), pl.OpPos)
			} else {
				p.spacedTok(caseClauseOp(pl.Op), true)
			}
			p.level--
			if sep || pl.OpPos == x.Esac {
				p.wantNewline = true
			}
		}
		p.decLevel()
		p.semiRsrv("esac", x.Esac, len(x.List) == 0)
	case *UntilClause:
		p.spacedRsrv("until")
		p.cond(x.Cond)
		p.semiOrNewl("do", x.Do)
		p.nestedStmts(x.DoStmts)
		p.semiRsrv("done", x.Done, true)
	case *DeclClause:
		if x.Local {
			p.spacedRsrv("local")
		} else {
			p.spacedRsrv("declare")
		}
		for _, w := range x.Opts {
			p.spacedWord(w)
		}
		p.assigns(x.Assigns)
	case *EvalClause:
		p.spacedRsrv("eval")
		if x.Stmt != nil {
			p.stmt(x.Stmt)
		}
	case *LetClause:
		p.spacedRsrv("let")
		for _, n := range x.Exprs {
			p.space()
			p.arithm(n, true)
		}
	}
	return startRedirs
}

func startsWithLparen(stmts []*Stmt) bool {
	if len(stmts) < 1 {
		return false
	}
	_, ok := stmts[0].Cmd.(*Subshell)
	return ok
}

func (p *printer) stmts(stmts []*Stmt) {
	if len(stmts) == 0 {
		return
	}
	pos := stmts[0].Pos()
	if len(stmts) == 1 && pos <= p.nline {
		p.didSeparate(pos)
		p.stmt(stmts[0])
		return
	}
	inlineIndent := 0
	for i, s := range stmts {
		pos := s.Pos()
		ind := p.nlineIndex
		p.commentsUpTo(pos)
		if p.nlineIndex == 0 {
			p.incLines(pos)
		} else {
			p.newlines(pos)
		}
		p.incLines(pos)
		p.stmt(s)
		if !p.hasInline(pos, p.nline) {
			inlineIndent = 0
			continue
		}
		if ind < len(p.f.lines)-1 && s.End() > Pos(p.f.lines[ind+1]) {
			inlineIndent = 0
		}
		if inlineIndent == 0 {
			ind2 := p.nlineIndex
			nline2 := p.nline
			for _, s2 := range stmts[i:] {
				pos2 := s2.Pos()
				if pos2 > nline2 || !p.hasInline(pos2, nline2) {
					break
				}
				if l := stmtLen(p.f, s2); l > inlineIndent {
					inlineIndent = l
				}
				ind2++
				if ind2 >= len(p.f.lines) {
					nline2 = maxPos
				} else {
					nline2 = Pos(p.f.lines[ind2])
				}
			}
		}
		p.wantSpaces = inlineIndent - stmtLen(p.f, s)
	}
	p.wantNewline = true
}

var (
	printBuf  bytes.Buffer
	bufWriter = bufio.NewWriter(&printBuf)
)

func unquotedWordStr(f *File, w *Word) string {
	bufWriter.Reset(&printBuf)
	printBuf.Reset()
	p := printer{Writer: bufWriter, f: f}
	p.unquotedWord(w)
	bufWriter.Flush()
	return printBuf.String()
}

func wordStr(f *File, w Word) string {
	bufWriter.Reset(&printBuf)
	printBuf.Reset()
	p := printer{Writer: bufWriter, f: f}
	p.word(w)
	bufWriter.Flush()
	return printBuf.String()
}

func stmtLen(f *File, s *Stmt) int {
	bufWriter.Reset(&printBuf)
	printBuf.Reset()
	p := printer{Writer: bufWriter, f: f}
	p.incLines(s.Pos())
	p.stmt(s)
	return printBuf.Len() + bufWriter.Buffered()
}

func (p *printer) nestedStmts(stmts []*Stmt) {
	p.incLevel()
	p.stmts(stmts)
	p.decLevel()
}

func (p *printer) assigns(assigns []*Assign) {
	anyNewline := false
	for _, a := range assigns {
		if a.Pos() > p.nline {
			p.bslashNewl()
			if !anyNewline {
				p.incLevel()
				anyNewline = true
			}
			p.indent()
		} else if p.wantSpace {
			p.space()
		}
		if a.Name != nil {
			p.WriteString(a.Name.Value)
			if a.Append {
				p.WriteByte('+')
			}
			p.WriteByte('=')
		}
		p.word(a.Value)
		p.wantSpace = true
	}
	if anyNewline {
		p.decLevel()
	}
}
