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
	switch x := node.(type) {
	case *File:
		p.comments = x.Comments
		p.stmts(x.Stmts)
		p.commentsUpTo(0)
	case *Stmt:
		p.stmt(x)
	default:
		return fmt.Errorf("unsupported root node: %T", node)
	}
	p.newline()
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
	comments []*Comment

	// pendingHdocs is the list of pending heredocs to write.
	pendingHdocs []*Redirect

	nestedBinary bool
}

var (
	spaces = []byte("                                ")
	tabs   = []byte("\t\t\t\t\t\t\t\t")

	newl   = []byte("\n")
	bsNewl = []byte(" \\\n")
)

func (p *printer) space() {
	_, p.err = p.w.Write(spaces[:1])
	p.wantSpace = false
}

func (p *printer) spaces(n int) {
	for n > 0 {
		if n < len(spaces) {
			_, p.err = p.w.Write(spaces[:n])
			break
		}
		_, p.err = p.w.Write(spaces)
		n -= len(spaces)
	}
	p.wantSpace = false
}

func (p *printer) tabs(n int) {
	for n > 0 {
		if n < len(tabs) {
			_, p.err = p.w.Write(tabs[:n])
			break
		}
		_, p.err = p.w.Write(tabs)
		n -= len(tabs)
	}
	p.wantSpace = false
}

func (p *printer) spacesNewl(bs []byte) {
	_, p.err = p.w.Write(bs)
	p.wantSpace = false
	p.curLine++
}

func (p *printer) str(s string) {
	_, p.err = io.WriteString(p.w, s)
	p.wantSpace = true
	p.curLine += strings.Count(s, "\n")
}

func (p *printer) token(s string, spaceAfter bool) {
	p.wantSpace = spaceAfter
	_, p.err = io.WriteString(p.w, s)
}

func (p *printer) rsrvWord(s string) {
	if p.wantNewline {
		p.newline()
		p.indent()
	} else if p.wantSpace {
		p.space()
	}
	_, p.err = io.WriteString(p.w, s)
	p.wantSpace = true
}

func (p *printer) spacedTok(s string, spaceAfter bool) {
	if p.wantSpace {
		p.space()
	}
	p.token(s, spaceAfter)
}

func (p *printer) semiOrNewl(s string, pos Pos) {
	if !p.wantNewline {
		p.token(";", true)
	}
	p.rsrvWord(s)
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
		p.tabs(p.level)
	case p.c.Spaces > 0:
		p.spaces(p.c.Spaces*p.level)
	}
}

func (p *printer) newline() {
	p.wantNewline = false
	_, p.err = io.WriteString(p.w, "\n")
	p.wantSpace = false
	for _, r := range p.pendingHdocs {
		p.str(r.Hdoc.Value)
		p.unquotedWord(&r.Word)
		p.spacesNewl(newl)
	}
	p.pendingHdocs = nil
}

func (p *printer) newlines(pos Pos) {
	p.newline()
	if pos.Line > p.curLine+1 {
		// preserve single empty lines
		_, p.err = io.WriteString(p.w, "\n")
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

func (p *printer) sepTok(s string, pos Pos) {
	p.level++
	p.commentsUpTo(pos.Line)
	p.level--
	p.didSeparate(pos)
	if s == ")" {
		p.token(s, true)
	} else {
		p.spacedTok(s, true)
	}
}

func (p *printer) sepRsrv(s string, pos Pos, fallback bool) {
	p.level++
	p.commentsUpTo(pos.Line)
	p.level--
	if !p.didSeparate(pos) && fallback {
		p.token(";", true)
	}
	p.rsrvWord(s)
}

func (p *printer) hasInline(pos Pos) bool {
	if len(p.comments) < 1 {
		return false
	}
	for _, c := range p.comments {
		if c.Hash.Line == pos.Line {
			return true
		}
		if c.Hash.Line > pos.Line {
			return false
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
		p.spaces(p.wantSpaces+1)
	}
	_, p.err = io.WriteString(p.w, "#")
	_, p.err = io.WriteString(p.w, c.Text)
	p.comments = p.comments[1:]
	p.commentsUpTo(line)
}

func (p *printer) wordPart(wp WordPart) {
	switch x := wp.(type) {
	case *Lit:
		p.str(x.Value)
	case *SglQuoted:
		p.token("'", true)
		p.str(x.Value)
		p.token("'", true)
	case *Quoted:
		p.token(x.Quote.String(), true)
		for _, n := range x.Parts {
			p.wordPart(n)
		}
		p.token(quotedStop(x.Quote).String(), true)
	case *CmdSubst:
		if x.Backquotes {
			p.token("`", false)
		} else {
			p.token("$(", false)
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
			p.token("$", true)
			p.str(x.Param.Value)
			break
		}
		p.token("${", true)
		if x.Length {
			p.token("#", true)
		}
		p.str(x.Param.Value)
		if x.Ind != nil {
			p.token("[", true)
			p.word(x.Ind.Word)
			p.token("]", true)
		}
		if x.Repl != nil {
			if x.Repl.All {
				p.token("/", true)
			}
			p.token("/", true)
			p.word(x.Repl.Orig)
			p.token("/", true)
			p.word(x.Repl.With)
		} else if x.Exp != nil {
			p.token(x.Exp.Op.String(), true)
			p.word(x.Exp.Word)
		}
		p.token("}", true)
	case *ArithmExp:
		p.token("$((", false)
		p.arithm(x.X, false)
		p.token("))", true)
	case *ArrayExpr:
		p.token("(", false)
		p.wordJoin(x.List, false)
		p.sepTok(")", x.Rparen)
	case *ProcSubst:
		// avoid conflict with << and others
		p.spacedTok(x.Op.String(), false)
		p.nestedStmts(x.Stmts)
		p.token(")", true)
	}
}

func (p *printer) cond(cond Cond) {
	switch x := cond.(type) {
	case *StmtCond:
		p.nestedStmts(x.Stmts)
	case *CStyleCond:
		p.spacedTok("((", false)
		p.arithm(x.X, false)
		p.token("))", true)
	}
}

func (p *printer) loop(loop Loop) {
	switch x := loop.(type) {
	case *WordIter:
		p.str(x.Name.Value)
		if len(x.List) > 0 {
			p.rsrvWord("in")
			p.wordJoin(x.List, true)
		}
	case *CStyleLoop:
		p.token("((", false)
		p.arithm(x.Init, false)
		p.token(";", true)
		p.arithm(x.Cond, false)
		p.token(";", true)
		p.arithm(x.Post, false)
		p.token("))", true)
	}
}

func (p *printer) arithm(expr ArithmExpr, compact bool) {
	switch x := expr.(type) {
	case *Word:
		p.spacedWord(*x)
	case *BinaryExpr:
		if compact {
			p.arithm(x.X, true)
			p.token(x.Op.String(), false)
			p.arithm(x.Y, true)
		} else {
			p.arithm(x.X, false)
			if x.Op == COMMA {
				p.token(",", true)
			} else {
				p.spacedTok(x.Op.String(), true)
			}
			p.arithm(x.Y, false)
		}
	case *UnaryExpr:
		if x.Post {
			p.arithm(x.X, compact)
			p.token(x.Op.String(), true)
		} else {
			p.spacedTok(x.Op.String(), false)
			p.arithm(x.X, compact)
		}
	case *ParenExpr:
		p.spacedTok("(", false)
		p.arithm(x.X, false)
		p.token(")", true)
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
			p.str(x.Value)
		case *Quoted:
			for _, qp := range x.Parts {
				p.wordPart(qp)
			}
		case *Lit:
			if x.Value[0] == '\\' {
				p.str(x.Value[1:])
			} else {
				p.str(x.Value)
			}
		default:
			p.wordPart(wp)
		}
	}
}

func (p *printer) spacedWord(w Word) {
	if p.wantSpace {
		p.space()
	}
	p.word(w)
}

func (p *printer) wordJoin(ws []Word, needBackslash bool) {
	anyNewline := false
	for _, w := range ws {
		if p.curLine > 0 && w.Pos().Line > p.curLine {
			if needBackslash {
				p.spacesNewl(bsNewl)
			} else {
				p.spacesNewl(newl)
			}
			if !anyNewline {
				p.incLevel()
				anyNewline = true
			}
			p.indent()
		} else if p.wantSpace {
			p.space()
		}
		p.word(w)
	}
	if anyNewline {
		p.decLevel()
	}
}

func (p *printer) stmt(s *Stmt) {
	if s.Negated {
		p.rsrvWord("!")
	}
	p.assigns(s.Assigns)
	startRedirs := p.command(s.Cmd, s.Redirs)
	anyNewline := false
	for _, r := range s.Redirs[startRedirs:] {
		if p.curLine > 0 && r.OpPos.Line > p.curLine {
			p.spacesNewl(bsNewl)
			if !anyNewline {
				p.incLevel()
				anyNewline = true
			}
			p.indent()
		}
		p.didSeparate(r.OpPos)
		if p.wantSpace {
			p.space()
		}
		if r.N != nil {
			p.str(r.N.Value)
		}
		p.token(r.Op.String(), true)
		p.word(r.Word)
		if r.Op == SHL || r.Op == DHEREDOC {
			p.pendingHdocs = append(p.pendingHdocs, r)
		}
	}
	if anyNewline {
		p.decLevel()
	}
	if s.Background {
		p.spacedTok("&", true)
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
			if posGreater(r.Pos(), x.Args[1].Pos()) {
				break
			}
			if r.Op == SHL || r.Op == DHEREDOC {
				break
			}
			if p.wantSpace {
				p.space()
			}
			if r.N != nil {
				p.str(r.N.Value)
			}
			p.token(r.Op.String(), true)
			p.word(r.Word)
			startRedirs++
		}
		p.wordJoin(x.Args[1:], true)
	case *Block:
		p.rsrvWord("{")
		p.nestedStmts(x.Stmts)
		p.sepRsrv("}", x.Rbrace, true)
	case *IfClause:
		p.rsrvWord("if")
		p.cond(x.Cond)
		p.semiOrNewl("then", x.Then)
		p.nestedStmts(x.ThenStmts)
		for _, el := range x.Elifs {
			p.sepRsrv("elif", el.Elif, true)
			p.cond(el.Cond)
			p.semiOrNewl("then", el.Then)
			p.nestedStmts(el.ThenStmts)
		}
		if len(x.ElseStmts) > 0 {
			p.sepRsrv("else", x.Else, true)
			p.nestedStmts(x.ElseStmts)
		} else if x.Else.Line > 0 {
			p.curLine = x.Else.Line
		}
		p.sepRsrv("fi", x.Fi, true)
	case *Subshell:
		p.spacedTok("(", false)
		if startsWithLparen(x.Stmts) {
			p.space()
		}
		p.nestedStmts(x.Stmts)
		p.sepTok(")", x.Rparen)
	case *WhileClause:
		p.rsrvWord("while")
		p.cond(x.Cond)
		p.semiOrNewl("do", x.Do)
		p.nestedStmts(x.DoStmts)
		p.sepRsrv("done", x.Done, true)
	case *ForClause:
		p.rsrvWord("for ")
		p.loop(x.Loop)
		p.semiOrNewl("do", x.Do)
		p.nestedStmts(x.DoStmts)
		p.sepRsrv("done", x.Done, true)
	case *BinaryCmd:
		p.stmt(x.X)
		indent := !p.nestedBinary
		if indent {
			p.incLevel()
		}
		_, p.nestedBinary = x.Y.Cmd.(*BinaryCmd)
		if len(p.pendingHdocs) > 0 {
		} else if x.Y.Pos().Line > p.curLine {
			p.spacesNewl(bsNewl)
			p.indent()
		}
		p.curLine = x.Y.Pos().Line
		p.spacedTok(x.Op.String(), true)
		p.stmt(x.Y)
		if indent {
			p.decLevel()
		}
		p.nestedBinary = false
	case *FuncDecl:
		if x.BashStyle {
			p.rsrvWord("function ")
		}
		p.str(x.Name.Value)
		p.token("()", true)
		p.stmt(x.Body)
	case *CaseClause:
		p.rsrvWord("case ")
		p.word(x.Word)
		p.rsrvWord("in")
		p.incLevel()
		for _, pl := range x.List {
			p.didSeparate(wordFirstPos(pl.Patterns))
			for i, w := range pl.Patterns {
				if i > 0 {
					p.spacedTok("|", true)
				}
				p.spacedWord(w)
			}
			p.token(")", true)
			sep := p.nestedStmts(pl.Stmts)
			p.level++
			if !sep {
				p.curLine++
			} else if pl.OpPos.Line == p.curLine && pl.OpPos != x.Esac {
				p.curLine--
			}
			p.sepTok(pl.Op.String(), pl.OpPos)
			if pl.OpPos == x.Esac {
				p.curLine--
			}
			p.level--
		}
		p.decLevel()
		p.sepRsrv("esac", x.Esac, len(x.List) == 0)
	case *UntilClause:
		p.rsrvWord("until")
		p.cond(x.Cond)
		p.semiOrNewl("do", x.Do)
		p.nestedStmts(x.DoStmts)
		p.sepRsrv("done", x.Done, true)
	case *DeclClause:
		if x.Local {
			p.rsrvWord("local")
		} else {
			p.rsrvWord("declare")
		}
		for _, w := range x.Opts {
			p.spacedWord(w)
		}
		p.assigns(x.Assigns)
	case *EvalClause:
		p.rsrvWord("eval")
		if x.Stmt != nil {
			p.stmt(x.Stmt)
		}
	case *LetClause:
		p.rsrvWord("let")
		for _, n := range x.Exprs {
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

func (p *printer) stmts(stmts []*Stmt) bool {
	if len(stmts) == 0 {
		return false
	}
	if len(stmts) == 1 && stmts[0].Pos().Line == p.curLine {
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
			lastLine := s.Pos().Line
			for _, s2 := range stmts[i:] {
				pos := s2.Pos()
				if !p.hasInline(pos) || pos.Line > lastLine+1 {
					break
				}
				if l := stmtLen(s2); l > inlineIndent {
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

func unquotedWordStr(w *Word) string {
	var buf bytes.Buffer
	p := printer{w: &buf}
	p.unquotedWord(w)
	return buf.String()
}

func wordStr(w Word) string {
	var buf bytes.Buffer
	p := printer{w: &buf}
	p.word(w)
	return buf.String()
}

func stmtLen(s *Stmt) int {
	var buf bytes.Buffer
	p := printer{w: &buf}
	p.stmt(s)
	return buf.Len()
}

func (p *printer) nestedStmts(stmts []*Stmt) bool {
	p.incLevel()
	sep := p.stmts(stmts)
	p.decLevel()
	return sep
}

func (p *printer) assigns(assigns []*Assign) {
	anyNewline := false
	for _, a := range assigns {
		if p.curLine > 0 && a.Pos().Line > p.curLine {
			p.spacesNewl(bsNewl)
			if !anyNewline {
				p.incLevel()
				anyNewline = true
			}
			p.indent()
		} else if p.wantSpace {
			p.space()
		}
		if a.Name != nil {
			p.str(a.Name.Value)
			if a.Append {
				p.token("+=", true)
			} else {
				p.token("=", true)
			}
		}
		p.word(a.Value)
	}
	if anyNewline {
		p.decLevel()
	}
}
