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
func (c PrintConfig) Fprint(w io.Writer, n Node) error {
	p := printer{w: w, c: c}
	if f, ok := n.(File); ok {
		p.comments = f.Comments
	}
	p.node(n)
	return p.err
}

// Fprint "pretty-prints" the given AST node to the given writer. It
// calls PrintConfig.Fprint with its default settings.
func Fprint(w io.Writer, n Node) error {
	c := PrintConfig{}
	return c.Fprint(w, n)
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

	// stack of nodes leading to the current one
	stack []Node

	// pendingHdocs is the list of pending heredocs to write.
	pendingHdocs []Redirect
}

func (p *printer) nestedBinary() bool {
	if len(p.stack) < 3 {
		return false
	}
	_, ok := p.stack[len(p.stack)-3].(BinaryExpr)
	return ok
}

func (p *printer) compactArithm() bool {
	for i := len(p.stack) - 1; i >= 0; i-- {
		switch p.stack[i].(type) {
		case LetStmt:
			return true
		case ArithmExpr, ParenExpr:
			return false
		}
	}
	return false
}

var (
	// these never want a following space
	contiguousRight = map[Token]bool{
		DOLLPR:  true,
		LPAREN:  true,
		DLPAREN: true,
		CMDIN:   true,
		CMDOUT:  true,
		DOLLDP:  true,
	}
	// these never want a preceding space
	contiguousLeft = map[Token]bool{
		SEMICOLON: true,
		RPAREN:    true,
		DRPAREN:   true,
		COMMA:     true,
	}
)

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

func (p *printer) token(tok Token) {
	p.wantSpace = !contiguousRight[tok]
	_, p.err = fmt.Fprint(p.w, tok)
}

func (p *printer) spacedTok(tok Token) {
	if p.wantNewline {
		p.newline()
		p.indent()
	} else if contiguousLeft[tok] {
	} else if p.wantSpace {
		p.space(' ')
	}
	p.token(tok)
}

func (p *printer) spacedNode(node Node) {
	if node == nil {
		return
	}
	if p.wantSpace {
		p.space(' ')
	}
	p.node(node)
}

func (p *printer) spacedStr(s string) {
	if p.wantSpace {
		p.space(' ')
	}
	p.str(s)
}

func (p *printer) semiOrNewl(tok Token, pos Pos) {
	if !p.wantNewline {
		p.token(SEMICOLON)
	}
	p.spacedTok(tok)
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
		p.str(strFprint(unquote(r.Word), -1))
		p.str("\n")
	}
	p.pendingHdocs = nil
}

func (p *printer) newlines(pos Pos) {
	p.newline()
	if pos.Line > p.curLine+1 {
		// preserve single empty lines
		p.newline()
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
	} else if p.wantNewline || (p.curLine > 0 && pos.Line > p.curLine) {
		p.spacedStr("\\")
		p.newline()
		p.indent()
	}
	p.curLine = pos.Line
}

func (p *printer) separated(tok Token, pos Pos, fallback bool) {
	p.level++
	p.commentsUpTo(pos.Line)
	p.level--
	if !p.didSeparate(pos) && fallback {
		p.token(SEMICOLON)
	}
	p.spacedTok(tok)
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
		if p.wantSpaces == 0 {
			p.wantSpaces++
		}
		p.str(strings.Repeat(" ", p.wantSpaces))
	}
	_, p.err = fmt.Fprint(p.w, HASH, c.Text)
	p.comments = p.comments[1:]
	p.commentsUpTo(line)
}

func (p *printer) node(node Node) {
	p.stack = append(p.stack, node)
	switch x := node.(type) {
	case File:
		p.stmts(x.Stmts)
		p.commentsUpTo(0)
		p.newline()
	case Stmt:
		p.stmt(x)
	case Command:
		p.command(x)
	case Block:
		p.spacedTok(LBRACE)
		p.nestedStmts(x.Stmts)
		p.separated(RBRACE, x.Rbrace, true)
	case Lit:
		p.lit(x)
	case IfStmt:
		p.spacedTok(IF)
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
		p.spacedTok(LPAREN)
		p.nestedStmts(x.Stmts)
		p.separated(RPAREN, x.Rparen, false)
	case WhileStmt:
		p.spacedTok(WHILE)
		p.cond(x.Cond)
		p.semiOrNewl(DO, x.Do)
		p.nestedStmts(x.DoStmts)
		p.separated(DONE, x.Done, true)
	case ForStmt:
		p.spacedTok(FOR)
		p.cond(x.Cond)
		p.semiOrNewl(DO, x.Do)
		p.nestedStmts(x.DoStmts)
		p.separated(DONE, x.Done, true)
	case BinaryExpr:
		p.spacedNode(x.X)
		if !p.nestedBinary() {
			p.incLevel()
		}
		p.singleStmtSeparate(x.Y.Pos())
		p.spacedTok(x.Op)
		p.node(x.Y)
		if !p.nestedBinary() {
			p.decLevel()
		}
	case FuncDecl:
		if x.BashStyle {
			p.spacedTok(FUNCTION)
			p.space(' ')
		}
		p.lit(x.Name)
		p.token(LPAREN)
		p.token(RPAREN)
		p.space(' ')
		p.stmt(x.Body)
	case Word:
		p.word(x)
	case SglQuoted:
		p.token(SQUOTE)
		p.str(x.Value)
		p.token(SQUOTE)
	case Quoted:
		p.token(x.Quote)
		for _, n := range x.Parts {
			p.node(n)
		}
		p.token(quotedStop(x.Quote))
	case CmdSubst:
		if x.Backquotes {
			p.token(BQUOTE)
			p.wantSpace = false
		} else {
			p.token(DOLLPR)
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
			p.token(DOLLAR)
			p.lit(x.Param)
			break
		}
		p.token(DOLLBR)
		if x.Length {
			p.token(HASH)
		}
		p.lit(x.Param)
		if x.Ind != nil {
			p.token(LBRACK)
			p.word(x.Ind.Word)
			p.token(RBRACK)
		}
		if x.Repl != nil {
			if x.Repl.All {
				p.token(QUO)
			}
			p.token(QUO)
			p.word(x.Repl.Orig)
			p.token(QUO)
			p.word(x.Repl.With)
		} else if x.Exp != nil {
			p.token(x.Exp.Op)
			p.word(x.Exp.Word)
		}
		p.token(RBRACE)
	case UntilStmt:
		p.spacedTok(UNTIL)
		p.cond(x.Cond)
		p.semiOrNewl(DO, x.Do)
		p.nestedStmts(x.DoStmts)
		p.separated(DONE, x.Done, true)
	case ArithmExpr:
		p.token(DOLLDP)
		p.arithm(x.X)
		p.token(DRPAREN)
	case CaseStmt:
		p.spacedTok(CASE)
		p.spacedWord(x.Word)
		p.spacedTok(IN)
		p.incLevel()
		for _, pl := range x.List {
			p.didSeparate(wordFirstPos(pl.Patterns))
			for i, w := range pl.Patterns {
				if i > 0 {
					p.spacedTok(OR)
				}
				p.spacedWord(w)
			}
			p.token(RPAREN)
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
	case DeclStmt:
		if x.Local {
			p.spacedTok(LOCAL)
		} else {
			p.spacedTok(DECLARE)
		}
		for _, w := range x.Opts {
			p.spacedWord(w)
		}
		p.assigns(x.Assigns)
	case ArrayExpr:
		p.token(LPAREN)
		p.wordJoin(x.List, false)
		p.separated(RPAREN, x.Rparen, false)
	case ProcSubst:
		// avoid conflict with << and others
		p.spacedTok(x.Op)
		p.nestedStmts(x.Stmts)
		p.token(RPAREN)
	case EvalStmt:
		p.spacedTok(EVAL)
		p.space(' ')
		p.stmt(x.Stmt)
	case LetStmt:
		p.spacedTok(LET)
		for _, n := range x.Exprs {
			p.spacedArithm(n)
		}
	}
	p.stack = p.stack[:len(p.stack)-1]
}

func (p *printer) cond(node Node) {
	p.stack = append(p.stack, node)
	switch x := node.(type) {
	case StmtCond:
		p.nestedStmts(x.Stmts)
	case WordIter:
		if p.wantSpace {
			p.space(' ')
		}
		p.lit(x.Name)
		if len(x.List) > 0 {
			p.spacedTok(IN)
			p.wordJoin(x.List, true)
		}
	case CStyleCond:
		p.spacedTok(DLPAREN)
		p.arithm(x.X)
		p.spacedTok(DRPAREN)
	case CStyleLoop:
		p.spacedTok(DLPAREN)
		p.spacedArithm(x.Init)
		p.spacedTok(SEMICOLON)
		p.spacedArithm(x.Cond)
		p.spacedTok(SEMICOLON)
		p.spacedArithm(x.Post)
		p.spacedTok(DRPAREN)
	}
	p.stack = p.stack[:len(p.stack)-1]
}

func (p *printer) arithm(node Node) {
	p.stack = append(p.stack, node)
	switch x := node.(type) {
	case Word:
		p.word(x)
	case BinaryExpr:
		if p.compactArithm() {
			p.arithm(x.X)
			p.token(x.Op)
			p.arithm(x.Y)
		} else {
			p.spacedArithm(x.X)
			p.spacedTok(x.Op)
			p.spacedArithm(x.Y)
		}
	case UnaryExpr:
		if x.Post {
			p.arithm(x.X)
			p.token(x.Op)
		} else {
			p.token(x.Op)
			p.wantSpace = false
			p.arithm(x.X)
		}
	case ParenExpr:
		p.token(LPAREN)
		p.arithm(x.X)
		p.token(RPAREN)
	}
	p.stack = p.stack[:len(p.stack)-1]
}
func (p *printer) spacedArithm(node Node) {
	if p.wantSpace {
		p.space(' ')
	}
	p.arithm(node)
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
		p.spacedTok(NOT)
	}
	p.assigns(s.Assigns)
	startRedirs := 0
	cmd, ok := s.Node.(Command)
	if len(cmd.Args) > 1 {
		p.spacedWord(cmd.Args[0])
		for _, r := range s.Redirs {
			if posGreater(r.Pos(), cmd.Args[1].Pos()) {
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
			p.token(r.Op)
			p.word(r.Word)
			startRedirs++
		}
		p.wordJoin(cmd.Args[1:], true)
	} else if ok {
		p.command(cmd)
	} else {
		p.spacedNode(s.Node)
	}
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
		p.token(r.Op)
		p.word(r.Word)
		if r.Op == SHL || r.Op == DHEREDOC {
			p.pendingHdocs = append(p.pendingHdocs, r)
		}
	}
	if anyNewline {
		p.decLevel()
	}
	if s.Background {
		p.spacedTok(AND)
	}
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
				l := len(strFprint(s, 0))
				if l > inlineIndent {
					inlineIndent = l
				}
				lastLine = pos.Line
			}
		}
		l := len(strFprint(s, 0))
		p.wantSpaces = (inlineIndent - l) + 1
	}
	p.wantNewline = true
	return true
}

func strFprint(n Node, spaces int) string {
	var buf bytes.Buffer
	p := printer{w: &buf, c: PrintConfig{Spaces: spaces}}
	if f, ok := n.(File); ok {
		p.comments = f.Comments
	}
	p.node(n)
	return buf.String()
}

func (p *printer) nestedStmts(stmts []Stmt) bool {
	p.incLevel()
	sep := p.stmts(stmts)
	p.decLevel()
	return sep
}

func (p *printer) command(cmd Command) { p.wordJoin(cmd.Args, true) }

func (p *printer) assigns(assigns []Assign) {
	for _, a := range assigns {
		if p.wantSpace {
			p.space(' ')
		}
		if a.Name != nil {
			p.node(a.Name)
			if a.Append {
				p.token(ADD_ASSIGN)
			} else {
				p.token(ASSIGN)
			}
		}
		p.word(a.Value)
	}
}
