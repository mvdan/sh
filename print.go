// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"bytes"
	"fmt"
	"io"
	"strings"
)

type PrintConfig struct {
	Spaces int // 0 (default) for tabs, >0 for number of spaces
}

func (c PrintConfig) Fprint(w io.Writer, n Node) error {
	p := printer{w: w, c: c}
	if f, ok := n.(File); ok {
		p.comments = f.Comments
	}
	p.node(n)
	return p.err
}

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

	curLine int
	level   int

	comments []Comment

	stack []Node

	pendingHdocs []Redirect
}

func (p *printer) nestedBinary() bool {
	if len(p.stack) < 3 {
		return false
	}
	_, ok := p.stack[len(p.stack)-3].(BinaryExpr)
	return ok
}

func (p *printer) inArithm() bool {
	for i := len(p.stack) - 1; i >= 0; i-- {
		switch p.stack[i].(type) {
		case ArithmExpr, LetStmt, CStyleCond, CStyleLoop:
			return true
		case Stmt:
			return false
		}
	}
	return false
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
	if b == '\n' {
		for _, r := range p.pendingHdocs {
			p.nonSpaced(*r.Hdoc, wordStr(unquote(r.Word)), "\n")
		}
		p.pendingHdocs = nil
	}
}

func (p *printer) nonSpaced(a ...interface{}) {
	for _, v := range a {
		if p.err != nil {
			break
		}
		switch x := v.(type) {
		case string:
			if len(x) > 0 {
				last := x[len(x)-1]
				p.wantSpace = !space[last]
			}
			_, p.err = io.WriteString(p.w, x)
			p.curLine += strings.Count(x, "\n")
		case Comment:
			p.wantSpace = true
			_, p.err = fmt.Fprint(p.w, HASH, x.Text)
		case Token:
			p.wantSpace = !contiguousRight[x]
			_, p.err = fmt.Fprint(p.w, x)
		case Node:
			p.node(x)
		}
	}
}

func (p *printer) spaced(a ...interface{}) {
	for _, v := range a {
		if v == nil {
			continue
		}
		if p.wantNewline {
			p.space('\n')
			p.indent()
			p.wantNewline = false
		} else if t, ok := v.(Token); ok && contiguousLeft[t] {
		} else if p.wantSpace {
			p.space(' ')
		}
		p.nonSpaced(v)
	}
}

func (p *printer) semiOrNewl(v interface{}) {
	if !p.wantNewline {
		p.nonSpaced(SEMICOLON)
	}
	p.spaced(v)
}

func (p *printer) indent() {
	for i := 0; i < p.level; i++ {
		if p.c.Spaces == 0 {
			p.space('\t')
			continue
		}
		for j := 0; j < p.c.Spaces; j++ {
			p.space(' ')
		}
	}
}

func (p *printer) newline() {
	p.wantNewline = false
	p.space('\n')
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
	p.curLine = pos.Line
	return false
}

func (p *printer) singleStmtSeparate(pos Pos) {
	if len(p.pendingHdocs) > 0 {
	} else if p.wantNewline || (p.curLine > 0 && pos.Line > p.curLine) {
		p.spaced("\\")
		p.newline()
		p.indent()
	}
	p.curLine = pos.Line
}

func (p *printer) separated(v interface{}, pos Pos, fallback bool) {
	p.level++
	p.commentsUpTo(pos.Line)
	p.level--
	if !p.didSeparate(pos) && fallback {
		p.nonSpaced(SEMICOLON)
	}
	p.spaced(v)
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
	if !p.didSeparate(c.Hash) && p.wantSpaces > 0 {
		p.nonSpaced(strings.Repeat(" ", p.wantSpaces+1))
	}
	p.spaced(c)
	p.comments = p.comments[1:]
	p.commentsUpTo(line)
}

func (p *printer) node(n Node) {
	p.stack = append(p.stack, n)
	switch x := n.(type) {
	case File:
		p.stmts(x.Stmts)
		p.commentsUpTo(0)
		p.space('\n')
	case Stmt:
		if x.Negated {
			p.spaced(NOT)
		}
		for _, a := range x.Assigns {
			p.spaced(a)
		}
		startRedirs := 0
		if c, ok := x.Node.(Command); ok && len(c.Args) > 1 {
			p.spaced(c.Args[0])
			for _, r := range x.Redirs {
				if posGreater(r.Pos(), c.Args[1].Pos()) {
					break
				}
				if r.Op == SHL || r.Op == DHEREDOC {
					break
				}
				p.spaced(r.N)
				p.nonSpaced(r.Op, r.Word)
				startRedirs++
			}
			p.wordJoin(c.Args[1:], true, true)
		} else {
			p.spaced(x.Node)
		}
		anyNewline := false
		for _, r := range x.Redirs[startRedirs:] {
			if p.curLine > 0 && r.OpPos.Line > p.curLine {
				p.spaced("\\\n")
				if !anyNewline {
					p.level++
					anyNewline = true
				}
				p.indent()
			}
			p.didSeparate(r.OpPos)
			p.spaced(r.N)
			p.nonSpaced(r.Op, r.Word)
			if r.Op == SHL || r.Op == DHEREDOC {
				p.pendingHdocs = append(p.pendingHdocs, r)
			}
		}
		if x.Background {
			p.spaced(AND)
		}
	case Assign:
		if x.Name != nil {
			p.spaced(x.Name)
			if x.Append {
				p.nonSpaced(ADD_ASSIGN)
			} else {
				p.nonSpaced(ASSIGN)
			}
		}
		p.nonSpaced(x.Value)
	case Command:
		p.wordJoin(x.Args, true, true)
	case Subshell:
		p.spaced(LPAREN)
		if len(x.Stmts) == 0 {
			// avoid conflict with ()
			p.space(' ')
		}
		p.nestedStmts(x.Stmts)
		p.separated(RPAREN, x.Rparen, false)
	case Block:
		p.spaced(LBRACE)
		p.nestedStmts(x.Stmts)
		p.separated(RBRACE, x.Rbrace, true)
	case IfStmt:
		p.spaced(IF)
		p.nonSpaced(x.Cond)
		p.semiOrNewl(THEN)
		p.curLine = x.Then.Line
		p.nestedStmts(x.ThenStmts)
		for _, el := range x.Elifs {
			p.separated(ELIF, el.Elif, true)
			p.nonSpaced(el.Cond)
			p.semiOrNewl(THEN)
			p.nestedStmts(el.ThenStmts)
		}
		if len(x.ElseStmts) > 0 {
			p.separated(ELSE, x.Else, true)
			p.nestedStmts(x.ElseStmts)
		}
		p.separated(FI, x.Fi, true)
	case StmtCond:
		p.nestedStmts(x.Stmts)
	case CStyleCond:
		p.spaced(DLPAREN, x.Cond, DRPAREN)
	case WhileStmt:
		p.spaced(WHILE)
		p.nonSpaced(x.Cond)
		p.semiOrNewl(DO)
		p.curLine = x.Do.Line
		p.nestedStmts(x.DoStmts)
		p.separated(DONE, x.Done, true)
	case UntilStmt:
		p.spaced(UNTIL)
		p.nonSpaced(x.Cond)
		p.semiOrNewl(DO)
		p.curLine = x.Do.Line
		p.nestedStmts(x.DoStmts)
		p.separated(DONE, x.Done, true)
	case ForStmt:
		p.spaced(FOR)
		p.nonSpaced(x.Cond)
		p.semiOrNewl(DO)
		p.curLine = x.Do.Line
		p.nestedStmts(x.DoStmts)
		p.separated(DONE, x.Done, true)
	case WordIter:
		p.spaced(x.Name)
		if len(x.List) > 0 {
			p.spaced(IN)
			p.wordJoin(x.List, false, true)
		}
	case CStyleLoop:
		p.spaced(DLPAREN, x.Init, SEMICOLON, x.Cond,
			SEMICOLON, x.Post, DRPAREN)
	case UnaryExpr:
		if x.Post {
			p.nonSpaced(x.X, x.Op)
		} else {
			p.nonSpaced(x.Op)
			p.wantSpace = false
			p.nonSpaced(x.X)
		}
	case BinaryExpr:
		switch {
		case p.compactArithm():
			p.nonSpaced(x.X, x.Op, x.Y)
		case p.inArithm():
			p.spaced(x.X, x.Op, x.Y)
		default:
			p.spaced(x.X)
			if !p.nestedBinary() {
				p.level++
			}
			p.singleStmtSeparate(x.Y.Pos())
			p.spaced(x.Op)
			p.nonSpaced(x.Y)
			if !p.nestedBinary() {
				p.level--
			}
		}
	case FuncDecl:
		if x.BashStyle {
			p.spaced(FUNCTION)
		}
		p.spaced(x.Name)
		p.nonSpaced(LPAREN, RPAREN)
		p.spaced(x.Body)
	case Word:
		for _, n := range x.Parts {
			p.nonSpaced(n)
		}
	case *Lit:
		if x != nil {
			p.nonSpaced(x.Value)
		}
	case Lit:
		p.nonSpaced(x.Value)
	case SglQuoted:
		p.nonSpaced(SQUOTE, x.Value, SQUOTE)
	case Quoted:
		p.nonSpaced(x.Quote)
		for _, n := range x.Parts {
			p.nonSpaced(n)
		}
		p.nonSpaced(quotedStop(x.Quote))
	case CmdSubst:
		if x.Backquotes {
			p.nonSpaced(BQUOTE)
			p.wantSpace = false
		} else {
			p.nonSpaced(DOLLPR)
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
			p.nonSpaced(DOLLAR, x.Param)
			break
		}
		p.nonSpaced(DOLLBR)
		if x.Length {
			p.nonSpaced(HASH)
		}
		p.nonSpaced(x.Param)
		if x.Ind != nil {
			p.nonSpaced(LBRACK, x.Ind.Word, RBRACK)
		}
		if x.Repl != nil {
			if x.Repl.All {
				p.nonSpaced(QUO)
			}
			p.nonSpaced(QUO, x.Repl.Orig, QUO, x.Repl.With)
		} else if x.Exp != nil {
			p.nonSpaced(x.Exp.Op, x.Exp.Word)
		}
		p.nonSpaced(RBRACE)
	case ArithmExpr:
		p.nonSpaced(DOLLDP, x.X, DRPAREN)
	case ParenExpr:
		p.nonSpaced(LPAREN, x.X, RPAREN)
	case CaseStmt:
		p.spaced(CASE, x.Word, IN)
		p.level++
		for _, pl := range x.List {
			p.didSeparate(wordFirstPos(pl.Patterns))
			for i, w := range pl.Patterns {
				if i > 0 {
					p.spaced(OR)
				}
				p.spaced(w)
			}
			p.nonSpaced(RPAREN)
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
		p.level--
		p.separated(ESAC, x.Esac, len(x.List) == 0)
	case DeclStmt:
		if x.Local {
			p.spaced(LOCAL)
		} else {
			p.spaced(DECLARE)
		}
		for _, w := range x.Opts {
			p.spaced(w)
		}
		for _, a := range x.Assigns {
			p.spaced(a)
		}
	case ArrayExpr:
		p.nonSpaced(LPAREN)
		p.wordJoin(x.List, true, false)
		p.separated(RPAREN, x.Rparen, false)
	case CmdInput:
		// avoid conflict with <<
		p.spaced(CMDIN)
		p.nestedStmts(x.Stmts)
		p.nonSpaced(RPAREN)
	case EvalStmt:
		p.spaced(EVAL, x.Stmt)
	case LetStmt:
		p.spaced(LET)
		for _, n := range x.Exprs {
			p.spaced(n)
		}
	}
	p.stack = p.stack[:len(p.stack)-1]
}

func (p *printer) wordJoin(ws []Word, keepNewlines, needBackslash bool) {
	anyNewline := false
	for _, w := range ws {
		if keepNewlines && p.curLine > 0 && w.Pos().Line > p.curLine {
			if needBackslash {
				p.spaced("\\")
			}
			p.nonSpaced("\n")
			if !anyNewline {
				p.level++
				anyNewline = true
			}
			p.indent()
		}
		p.spaced(w)
	}
	if anyNewline {
		p.level--
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
		p.node(s)
		return false
	}
	inlineIndent := 0
	lastLine := stmts[0].Pos().Line
	for i, s := range stmts {
		pos := s.Pos()
		p.alwaysSeparate(pos)
		p.node(s)
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
		p.wantSpaces = inlineIndent - l
	}
	inlineIndent = 0
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
	p.level++
	sep := p.stmts(stmts)
	p.level--
	return sep
}
