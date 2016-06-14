// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"bytes"
	"fmt"
	"io"
	"strings"
)

// PrintConfig controls how the printing of an AST node will behave.
type PrintConfig struct {
	Spaces int // 0 (default) for tabs, >0 for number of spaces
}

// Fprint "pretty-prints" the given AST node to the given writer.
func (c PrintConfig) Fprint(w io.Writer, node Node) error {
	p := printer{w: w, c: c}
	if f, ok := node.(File); ok {
		p.comments = f.Comments
	}
	p.node(node)
	return p.err
}

// Fprint "pretty-prints" the given AST node to the given writer. It
// calls PrintConfig.Fprint with its default settings.
func Fprint(w io.Writer, node Node) error {
	c := PrintConfig{}
	return c.Fprint(w, node)
}

type printer struct {
	w   io.Writer
	c   PrintConfig
	err error

	wantSpace   bool
	wantSpaces  int
	wantNewline bool

	// curLine is the line that is currently being printed (counted
	// in original lines).
	curLine int
	// lastLevel is the last level of indentation that was used.
	lastLevel int
	// level is the current level of indentation.
	level int
	// levelIncs records which indentation level increments actually
	// took place, to revert them once their section ends.
	levelIncs []bool

	// comments is the list of pending comments to write.
	comments []Comment

	// pendingHdocs is the list of pending heredocs to write.
	pendingHdocs []Redirect

	nestedBinary bool
}

func (p *printer) space(b byte) {
	if p.err != nil {
		return
	}
	_, p.err = p.w.Write([]byte{b})
	p.wantSpace = false
}

func (p *printer) str(s string) {
	if len(s) > 0 {
		last := s[len(s)-1]
		p.wantSpace = !space(last)
	}
	_, p.err = io.WriteString(p.w, s)
	p.curLine += strings.Count(s, "\n")
}

func (p *printer) token(tok Token, spaceAfter bool) {
	p.wantSpace = spaceAfter
	_, p.err = fmt.Fprint(p.w, tok)
}

func (p *printer) spacedTok(tok Token, spaceAfter bool) {
	if p.wantNewline {
		p.newline()
		p.indent()
	} else if p.wantSpace {
		p.space(' ')
	}
	p.token(tok, spaceAfter)
}

func (p *printer) spacedStr(s string) {
	if p.wantSpace {
		p.space(' ')
	}
	p.str(s)
}

func (p *printer) semiOrNewl(tok Token, pos Pos) {
	if !p.wantNewline {
		p.token(SEMICOLON, true)
	}
	p.spacedTok(tok, true)
	p.curLine = pos.Line
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
	inc := p.levelIncs[len(p.levelIncs)-1]
	p.levelIncs = p.levelIncs[:len(p.levelIncs)-1]
	if inc {
		p.level--
	}
}

func (p *printer) indent() {
	p.lastLevel = p.level
	switch {
	case p.level == 0:
	case p.c.Spaces == 0:
		p.str(strings.Repeat("\t", p.level))
	case p.c.Spaces > 0:
		p.str(strings.Repeat(" ", p.c.Spaces*p.level))
	}
}

func (p *printer) newline() {
	p.wantNewline = false
	p.space('\n')
	for _, r := range p.pendingHdocs {
		p.lit(*r.Hdoc)
		p.word(unquote(r.Word))
		p.str("\n")
	}
	p.pendingHdocs = nil
}

func (p *printer) newlines(pos Pos) {
	p.newline()
	if pos.Line > p.curLine+1 {
		// preserve single empty lines
		p.space('\n')
	}
	p.indent()
	p.curLine = pos.Line
}

func (p *printer) alwaysSeparate(pos Pos) {
	p.commentsUpTo(pos.Line)
	if p.curLine > 0 {
		p.newlines(pos)
	} else {
		p.curLine = pos.Line
	}
}

func (p *printer) didSeparate(pos Pos) bool {
	p.commentsUpTo(pos.Line)
	if p.wantNewline || (p.curLine > 0 && pos.Line > p.curLine) {
		p.newlines(pos)
		return true
	}
	if p.curLine == 0 {
		p.curLine = pos.Line
		return true
	}
	p.curLine = pos.Line
	return false
}

func (p *printer) singleStmtSeparate(pos Pos) {
	if len(p.pendingHdocs) > 0 {
	} else if pos.Line > p.curLine {
		p.spacedStr("\\\n")
		p.indent()
	}
	p.curLine = pos.Line
}

func (p *printer) separated(tok Token, pos Pos, fallback bool) {
	p.level++
	p.commentsUpTo(pos.Line)
	p.level--
	if !p.didSeparate(pos) && fallback {
		p.token(SEMICOLON, true)
	}
	if tok == RPAREN {
		p.token(tok, true)
	} else {
		p.spacedTok(tok, true)
	}
}

func (p *printer) hasInline(pos Pos) bool {
	if len(p.comments) < 1 {
		return false
	}
	for _, c := range p.comments {
		if c.Hash.Line == pos.Line {
			return true
		}
	}
	return false
}

func (p *printer) commentsUpTo(line int) {
	if len(p.comments) < 1 {
		return
	}
	c := p.comments[0]
	if line > 0 && c.Hash.Line >= line {
		return
	}
	p.wantNewline = false
	if !p.didSeparate(c.Hash) {
		p.str(strings.Repeat(" ", p.wantSpaces+1))
	}
	_, p.err = fmt.Fprint(p.w, HASH, c.Text)
	p.comments = p.comments[1:]
	p.commentsUpTo(line)
}

func (p *printer) node(node Node) {
	switch x := node.(type) {
	case File:
		p.stmts(x.Stmts)
		p.commentsUpTo(0)
		p.newline()
	case Lit:
		p.lit(x)
	case Word:
		p.word(x)
	case SglQuoted:
		p.token(SQUOTE, true)
		p.str(x.Value)
		p.token(SQUOTE, true)
	case Quoted:
		p.token(x.Quote, true)
		for _, n := range x.Parts {
			p.node(n)
		}
		p.token(quotedStop(x.Quote), true)
	case CmdSubst:
		if x.Backquotes {
			p.token(BQUOTE, false)
		} else {
			p.token(DOLLPR, false)
		}
		p.nestedStmts(x.Stmts)
		if x.Backquotes {
			p.wantSpace = false
			p.separated(BQUOTE, x.Right, false)
		} else {
			p.separated(RPAREN, x.Right, false)
		}
	case ParamExp:
		if x.Short {
			p.token(DOLLAR, true)
			p.lit(x.Param)
			break
		}
		p.token(DOLLBR, true)
		if x.Length {
			p.token(HASH, true)
		}
		p.lit(x.Param)
		if x.Ind != nil {
			p.token(LBRACK, true)
			p.word(x.Ind.Word)
			p.token(RBRACK, true)
		}
		if x.Repl != nil {
			if x.Repl.All {
				p.token(QUO, true)
			}
			p.token(QUO, true)
			p.word(x.Repl.Orig)
			p.token(QUO, true)
			p.word(x.Repl.With)
		} else if x.Exp != nil {
			p.token(x.Exp.Op, true)
			p.word(x.Exp.Word)
		}
		p.token(RBRACE, true)
	case ArithmExp:
		p.token(DOLLDP, false)
		p.arithm(x.X, false)
		p.token(DRPAREN, true)
	case ArrayExpr:
		p.token(LPAREN, false)
		p.wordJoin(x.List, false)
		p.separated(RPAREN, x.Rparen, false)
	case ProcSubst:
		// avoid conflict with << and others
		p.spacedTok(x.Op, false)
		p.nestedStmts(x.Stmts)
		p.token(RPAREN, true)
	}
}

func (p *printer) cond(node Node) {
	switch x := node.(type) {
	case StmtCond:
		p.nestedStmts(x.Stmts)
	case WordIter:
		p.space(' ')
		p.lit(x.Name)
		if len(x.List) > 0 {
			p.spacedTok(IN, true)
			p.wordJoin(x.List, true)
		}
	case CStyleCond:
		p.spacedTok(DLPAREN, false)
		p.arithm(x.X, false)
		p.token(DRPAREN, true)
	case CStyleLoop:
		p.spacedTok(DLPAREN, false)
		p.arithm(x.Init, false)
		p.token(SEMICOLON, true)
		p.arithm(x.Cond, false)
		p.token(SEMICOLON, true)
		p.arithm(x.Post, false)
		p.token(DRPAREN, true)
	}
}

func (p *printer) arithm(expr ArithmExpr, compact bool) {
	switch x := expr.(type) {
	case Word:
		p.spacedWord(x)
	case BinaryExpr:
		if compact {
			p.arithm(x.X, true)
			p.token(x.Op, false)
			p.arithm(x.Y, true)
		} else {
			p.arithm(x.X, false)
			if x.Op == COMMA {
				p.token(x.Op, true)
			} else {
				p.spacedTok(x.Op, true)
			}
			p.arithm(x.Y, false)
		}
	case UnaryExpr:
		if x.Post {
			p.arithm(x.X, compact)
			p.token(x.Op, true)
		} else {
			p.spacedTok(x.Op, false)
			p.arithm(x.X, compact)
		}
	case ParenExpr:
		p.spacedTok(LPAREN, false)
		p.arithm(x.X, false)
		p.token(RPAREN, true)
	}
}

func (p *printer) word(w Word) {
	for _, n := range w.Parts {
		p.node(n)
	}
}

func (p *printer) spacedWord(w Word) {
	if p.wantSpace {
		p.space(' ')
	}
	p.word(w)
}

func (p *printer) lit(l Lit) { p.str(l.Value) }

func (p *printer) wordJoin(ws []Word, needBackslash bool) {
	anyNewline := false
	for _, w := range ws {
		if p.curLine > 0 && w.Pos().Line > p.curLine {
			if needBackslash {
				p.spacedStr("\\\n")
			} else {
				p.str("\n")
			}
			if !anyNewline {
				p.incLevel()
				anyNewline = true
			}
			p.indent()
		} else if p.wantSpace {
			p.space(' ')
		}
		p.word(w)
	}
	if anyNewline {
		p.decLevel()
	}
}

func (p *printer) stmt(s Stmt) {
	if s.Negated {
		p.spacedTok(NOT, true)
	}
	p.assigns(s.Assigns)
	startRedirs := p.command(s.Cmd, s.Redirs)
	anyNewline := false
	for _, r := range s.Redirs[startRedirs:] {
		if p.curLine > 0 && r.OpPos.Line > p.curLine {
			p.spacedStr("\\\n")
			if !anyNewline {
				p.incLevel()
				anyNewline = true
			}
			p.indent()
		}
		p.didSeparate(r.OpPos)
		if p.wantSpace {
			p.space(' ')
		}
		if r.N != nil {
			p.lit(*r.N)
		}
		p.token(r.Op, true)
		p.word(r.Word)
		if r.Op == SHL || r.Op == DHEREDOC {
			p.pendingHdocs = append(p.pendingHdocs, r)
		}
	}
	if anyNewline {
		p.decLevel()
	}
	if s.Background {
		p.spacedTok(AND, true)
	}
}

func (p *printer) command(cmd Command, redirs []Redirect) (startRedirs int) {
	switch x := cmd.(type) {
	case CallExpr:
		if len(x.Args) <= 1 {
			p.call(x)
			return 0
		}
		p.spacedWord(x.Args[0])
		for _, r := range redirs {
			if posGreater(r.Pos(), x.Args[1].Pos()) {
				break
			}
			if r.Op == SHL || r.Op == DHEREDOC {
				break
			}
			if p.wantSpace {
				p.space(' ')
			}
			if r.N != nil {
				p.lit(*r.N)
			}
			p.token(r.Op, true)
			p.word(r.Word)
			startRedirs++
		}
		p.wordJoin(x.Args[1:], true)
	case Block:
		p.spacedTok(LBRACE, true)
		p.nestedStmts(x.Stmts)
		p.separated(RBRACE, x.Rbrace, true)
	case IfClause:
		p.spacedTok(IF, true)
		p.cond(x.Cond)
		p.semiOrNewl(THEN, x.Then)
		p.nestedStmts(x.ThenStmts)
		for _, el := range x.Elifs {
			p.separated(ELIF, el.Elif, true)
			p.cond(el.Cond)
			p.semiOrNewl(THEN, el.Then)
			p.nestedStmts(el.ThenStmts)
		}
		if len(x.ElseStmts) > 0 {
			p.separated(ELSE, x.Else, true)
			p.nestedStmts(x.ElseStmts)
		} else if x.Else.Line > 0 {
			p.curLine = x.Else.Line
		}
		p.separated(FI, x.Fi, true)
	case Subshell:
		p.spacedTok(LPAREN, false)
		p.nestedStmts(x.Stmts)
		p.separated(RPAREN, x.Rparen, false)
	case WhileClause:
		p.spacedTok(WHILE, true)
		p.cond(x.Cond)
		p.semiOrNewl(DO, x.Do)
		p.nestedStmts(x.DoStmts)
		p.separated(DONE, x.Done, true)
	case ForClause:
		p.spacedTok(FOR, true)
		p.cond(x.Cond)
		p.semiOrNewl(DO, x.Do)
		p.nestedStmts(x.DoStmts)
		p.separated(DONE, x.Done, true)
	case BinaryCmd:
		p.stmt(x.X)
		indent := !p.nestedBinary
		if indent {
			p.incLevel()
		}
		_, p.nestedBinary = x.Y.Cmd.(BinaryCmd)
		p.singleStmtSeparate(x.Y.Pos())
		p.spacedTok(x.Op, true)
		p.stmt(x.Y)
		if indent {
			p.decLevel()
		}
		p.nestedBinary = false
	case FuncDecl:
		if x.BashStyle {
			p.spacedTok(FUNCTION, true)
			p.space(' ')
		}
		p.lit(x.Name)
		p.token(LPAREN, false)
		p.token(RPAREN, true)
		p.stmt(x.Body)
	case CaseClause:
		p.spacedTok(CASE, true)
		p.spacedWord(x.Word)
		p.spacedTok(IN, true)
		p.incLevel()
		for _, pl := range x.List {
			p.didSeparate(wordFirstPos(pl.Patterns))
			for i, w := range pl.Patterns {
				if i > 0 {
					p.spacedTok(OR, true)
				}
				p.spacedWord(w)
			}
			p.token(RPAREN, true)
			sep := p.nestedStmts(pl.Stmts)
			p.level++
			if !sep {
				p.curLine++
			} else if pl.Dsemi.Line == p.curLine && pl.Dsemi != x.Esac {
				p.curLine--
			}
			p.separated(DSEMICOLON, pl.Dsemi, false)
			if pl.Dsemi == x.Esac {
				p.curLine--
			}
			p.level--
		}
		p.decLevel()
		p.separated(ESAC, x.Esac, len(x.List) == 0)
	case UntilClause:
		p.spacedTok(UNTIL, true)
		p.cond(x.Cond)
		p.semiOrNewl(DO, x.Do)
		p.nestedStmts(x.DoStmts)
		p.separated(DONE, x.Done, true)
	case DeclClause:
		if x.Local {
			p.spacedTok(LOCAL, true)
		} else {
			p.spacedTok(DECLARE, true)
		}
		for _, w := range x.Opts {
			p.spacedWord(w)
		}
		p.assigns(x.Assigns)
	case EvalClause:
		p.spacedTok(EVAL, true)
		p.stmt(x.Stmt)
	case LetClause:
		p.spacedTok(LET, true)
		for _, n := range x.Exprs {
			p.arithm(n, true)
		}
	}
	return startRedirs
}

func (p *printer) stmts(stmts []Stmt) bool {
	if len(stmts) == 0 {
		return false
	}
	sameLine := stmtFirstPos(stmts).Line == p.curLine
	if len(stmts) == 1 && sameLine {
		s := stmts[0]
		p.didSeparate(s.Pos())
		p.stmt(s)
		return false
	}
	inlineIndent := 0
	lastLine := stmts[0].Pos().Line
	for i, s := range stmts {
		pos := s.Pos()
		p.alwaysSeparate(pos)
		p.stmt(s)
		if pos.Line > lastLine+1 {
			inlineIndent = 0
		}
		lastLine = pos.Line
		if !p.hasInline(pos) {
			inlineIndent = 0
			continue
		}
		if inlineIndent == 0 {
			lastLine := stmts[i].Pos().Line
			for _, s := range stmts[i:] {
				pos := s.Pos()
				if !p.hasInline(pos) || pos.Line > lastLine+1 {
					break
				}
				if l := stmtLen(s); l > inlineIndent {
					inlineIndent = l
				}
				lastLine = pos.Line
			}
		}
		p.wantSpaces = inlineIndent - stmtLen(s)
	}
	p.wantNewline = true
	return true
}

func wordStr(w Word) string {
	var buf bytes.Buffer
	p := printer{w: &buf}
	p.word(w)
	return buf.String()
}

func stmtLen(s Stmt) int {
	var buf bytes.Buffer
	p := printer{w: &buf}
	p.stmt(s)
	return buf.Len()
}

func (p *printer) nestedStmts(stmts []Stmt) bool {
	p.incLevel()
	sep := p.stmts(stmts)
	p.decLevel()
	return sep
}

func (p *printer) call(ce CallExpr) { p.wordJoin(ce.Args, true) }

func (p *printer) assigns(assigns []Assign) {
	for _, a := range assigns {
		if p.wantSpace {
			p.space(' ')
		}
		if a.Name != nil {
			p.lit(*a.Name)
			if a.Append {
				p.token(ADD_ASSIGN, true)
			} else {
				p.token(ASSIGN, true)
			}
		}
		p.word(a.Value)
	}
}
