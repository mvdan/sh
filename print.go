// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"bufio"
	"bytes"
	"io"
	"sync"

	"github.com/mvdan/sh/ast"
	"github.com/mvdan/sh/token"
)

// PrintConfig controls how the printing of an AST node will behave.
type PrintConfig struct {
	Spaces int // 0 (default) for tabs, >0 for number of spaces
}

var printerFree = sync.Pool{
	New: func() interface{} {
		return &printer{Writer: bufio.NewWriter(nil)}
	},
}

// Fprint "pretty-prints" the given AST file to the given writer.
func (c PrintConfig) Fprint(w io.Writer, f *ast.File) error {
	p := printerFree.Get().(*printer)
	*p = printer{
		Writer:       p.Writer,
		helperBuf:    p.helperBuf,
		helperWriter: p.helperWriter,
		f:            f,
		comments:     f.Comments,
		c:            c,
	}
	p.Writer.Reset(w)
	p.stmts(f.Stmts)
	p.commentsUpTo(0)
	p.newline()
	err := p.Writer.Flush()
	printerFree.Put(p)
	return err
}

const maxPos = token.Pos(^uint(0) >> 1)

// Fprint "pretty-prints" the given AST file to the given writer. It
// calls PrintConfig.Fprint with its default settings.
func Fprint(w io.Writer, f *ast.File) error {
	return PrintConfig{}.Fprint(w, f)
}

type printer struct {
	*bufio.Writer

	f *ast.File
	c PrintConfig

	wantSpace   bool
	wantNewline bool
	wantSpaces  int

	// nline is the position of the next newline
	nline      token.Pos
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
	comments []*ast.Comment

	// pendingHdocs is the list of pending heredocs to write.
	pendingHdocs []*ast.Redirect

	// these are used in stmtLen to align comments
	helperBuf    *bytes.Buffer
	helperWriter *bufio.Writer
}

func (p *printer) incLine() {
	p.nlineIndex++
	if p.nlineIndex >= len(p.f.Lines) {
		p.nline = maxPos
	} else {
		p.nline = token.Pos(p.f.Lines[p.nlineIndex])
	}
}

func (p *printer) incLines(pos token.Pos) {
	for p.nline < pos {
		p.incLine()
	}
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

func (p *printer) semiOrNewl(s string, pos token.Pos) {
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
	hdocs := p.pendingHdocs
	p.pendingHdocs = p.pendingHdocs[:0]
	for _, r := range hdocs {
		p.word(r.Hdoc)
		p.incLines(r.Hdoc.End() + 1)
		p.unquotedWord(r.Word)
		p.WriteByte('\n')
		p.incLine()
		p.wantSpace = false
	}
}

func (p *printer) newlines(pos token.Pos) {
	p.newline()
	if pos > p.nline {
		// preserve single empty lines
		p.WriteByte('\n')
		p.incLine()
	}
	p.indent()
}

func (p *printer) didSeparate(pos token.Pos) bool {
	p.commentsUpTo(pos)
	if p.wantNewline || pos > p.nline {
		p.newlines(pos)
		return true
	}
	return false
}

func (p *printer) sepTok(s string, pos token.Pos) {
	p.level++
	p.commentsUpTo(pos)
	p.level--
	p.didSeparate(pos)
	p.WriteString(s)
	p.wantSpace = true
}

func (p *printer) semiRsrv(s string, pos token.Pos, fallback bool) {
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

func (p *printer) hasInline(pos, nline token.Pos) bool {
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

func (p *printer) commentsUpTo(pos token.Pos) {
	if len(p.comments) < 1 {
		return
	}
	c := p.comments[0]
	if pos > 0 && c.Hash >= pos {
		return
	}
	p.comments = p.comments[1:]
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
	p.commentsUpTo(pos)
}

func (p *printer) quotedOp(tok token.Token) {
	switch tok {
	case token.DQUOTE:
		p.WriteByte('"')
	case token.DOLLSQ:
		p.WriteString(`$'`)
	case token.SQUOTE:
		p.WriteByte('\'')
	default: // token.DOLLDQ
		p.WriteString(`$"`)
	}
}

func (p *printer) expansionOp(tok token.Token) {
	switch tok {
	case token.COLON:
		p.WriteByte(':')
	case token.ADD:
		p.WriteByte('+')
	case token.CADD:
		p.WriteString(":+")
	case token.SUB:
		p.WriteByte('-')
	case token.CSUB:
		p.WriteString(":-")
	case token.QUEST:
		p.WriteByte('?')
	case token.CQUEST:
		p.WriteString(":?")
	case token.ASSIGN:
		p.WriteByte('=')
	case token.CASSIGN:
		p.WriteString(":=")
	case token.REM:
		p.WriteByte('%')
	case token.DREM:
		p.WriteString("%%")
	case token.HASH:
		p.WriteByte('#')
	default: // token.DHASH
		p.WriteString("##")
	}
}

func (p *printer) wordPart(wp ast.WordPart) {
	switch x := wp.(type) {
	case *ast.Lit:
		p.WriteString(x.Value)
	case *ast.SglQuoted:
		p.WriteByte('\'')
		p.WriteString(x.Value)
		p.WriteByte('\'')
	case *ast.Quoted:
		p.quotedOp(x.Quote)
		for i, n := range x.Parts {
			p.wordPart(n)
			if i == len(x.Parts)-1 {
				p.incLines(n.End())
			}
		}
		p.quotedOp(quotedStop(x.Quote))
	case *ast.CmdSubst:
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
	case *ast.ParamExp:
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
			p.expansionOp(x.Exp.Op)
			p.word(x.Exp.Word)
		}
		p.WriteByte('}')
	case *ast.ArithmExp:
		if x.Token == token.DOLLDP {
			p.WriteString("$((")
		} else { // token.DLPAREN
			p.WriteString("((")
		}
		p.arithm(x.X, false)
		p.WriteString("))")
	case *ast.ArrayExpr:
		p.wantSpace = false
		p.WriteByte('(')
		p.wordJoin(x.List, false)
		p.sepTok(")", x.Rparen)
	case *ast.ProcSubst:
		// avoid conflict with << and others
		if p.wantSpace {
			p.space()
		}
		if x.Op == token.CMDIN {
			p.WriteString("<(")
		} else { // token.CMDOUT
			p.WriteString(">(")
		}
		p.nestedStmts(x.Stmts)
		p.WriteByte(')')
	}
	p.wantSpace = true
}

func (p *printer) cond(cond ast.Cond) {
	switch x := cond.(type) {
	case *ast.StmtCond:
		p.nestedStmts(x.Stmts)
	case *ast.CStyleCond:
		p.spacedTok("((", false)
		p.arithm(x.X, false)
		p.WriteString("))")
	}
}

func (p *printer) loop(loop ast.Loop) {
	switch x := loop.(type) {
	case *ast.WordIter:
		p.WriteString(x.Name.Value)
		if len(x.List) > 0 {
			p.WriteString(" in")
			p.wordJoin(x.List, true)
		}
	case *ast.CStyleLoop:
		p.WriteString("((")
		p.arithm(x.Init, false)
		p.WriteString("; ")
		p.arithm(x.Cond, false)
		p.WriteString("; ")
		p.arithm(x.Post, false)
		p.WriteString("))")
	}
}

func (p *printer) binaryExprOp(tok token.Token) {
	switch tok {
	case token.ASSIGN:
		p.WriteByte('=')
	case token.ADD:
		p.WriteByte('+')
	case token.SUB:
		p.WriteByte('-')
	case token.REM:
		p.WriteByte('%')
	case token.MUL:
		p.WriteByte('*')
	case token.QUO:
		p.WriteByte('/')
	case token.AND:
		p.WriteByte('&')
	case token.OR:
		p.WriteByte('|')
	case token.LAND:
		p.WriteString("&&")
	case token.LOR:
		p.WriteString("||")
	case token.XOR:
		p.WriteByte('^')
	case token.POW:
		p.WriteString("**")
	case token.EQL:
		p.WriteString("==")
	case token.NEQ:
		p.WriteString("!=")
	case token.LEQ:
		p.WriteString("<=")
	case token.GEQ:
		p.WriteString(">=")
	case token.ADDASSGN:
		p.WriteString("+=")
	case token.SUBASSGN:
		p.WriteString("-=")
	case token.MULASSGN:
		p.WriteString("*=")
	case token.QUOASSGN:
		p.WriteString("/=")
	case token.REMASSGN:
		p.WriteString("%=")
	case token.ANDASSGN:
		p.WriteString("&=")
	case token.ORASSGN:
		p.WriteString("|=")
	case token.XORASSGN:
		p.WriteString("^=")
	case token.SHLASSGN:
		p.WriteString("<<=")
	case token.SHRASSGN:
		p.WriteString(">>=")
	case token.LSS:
		p.WriteByte('<')
	case token.GTR:
		p.WriteByte('>')
	case token.SHL:
		p.WriteString("<<")
	case token.SHR:
		p.WriteString(">>")
	case token.QUEST:
		p.WriteByte('?')
	case token.COLON:
		p.WriteByte(':')
	default: // token.COMMA
		p.WriteByte(',')
	}
}

func (p *printer) unaryExprOp(tok token.Token) {
	switch tok {
	case token.ADD:
		p.WriteByte('+')
	case token.SUB:
		p.WriteByte('-')
	case token.NOT:
		p.WriteByte('!')
	case token.INC:
		p.WriteString("++")
	default: // token.DEC
		p.WriteString("--")
	}
}

func (p *printer) arithm(expr ast.ArithmExpr, compact bool) {
	p.wantSpace = false
	switch x := expr.(type) {
	case *ast.Word:
		p.word(*x)
	case *ast.BinaryExpr:
		if compact {
			p.arithm(x.X, true)
			p.binaryExprOp(x.Op)
			p.arithm(x.Y, true)
		} else {
			p.arithm(x.X, false)
			if x.Op != token.COMMA {
				p.WriteByte(' ')
			}
			p.binaryExprOp(x.Op)
			p.space()
			p.arithm(x.Y, false)
		}
	case *ast.UnaryExpr:
		if x.Post {
			p.arithm(x.X, compact)
			p.unaryExprOp(x.Op)
		} else {
			p.unaryExprOp(x.Op)
			p.arithm(x.X, compact)
		}
	case *ast.ParenExpr:
		p.WriteByte('(')
		p.arithm(x.X, false)
		p.WriteByte(')')
	}
}

func (p *printer) word(w ast.Word) {
	for _, n := range w.Parts {
		p.wordPart(n)
	}
}

func (p *printer) unquotedWord(w ast.Word) {
	for _, wp := range w.Parts {
		switch x := wp.(type) {
		case *ast.SglQuoted:
			p.WriteString(x.Value)
		case *ast.Quoted:
			for _, qp := range x.Parts {
				p.wordPart(qp)
			}
		case *ast.Lit:
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

func (p *printer) wordJoin(ws []ast.Word, backslash bool) {
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

func (p *printer) stmt(s *ast.Stmt) {
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
		p.redirectOp(r.Op)
		p.wantSpace = true
		p.word(r.Word)
		if r.Op == token.SHL || r.Op == token.DHEREDOC {
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

func (p *printer) redirectOp(tok token.Token) {
	switch tok {
	case token.LSS:
		p.WriteByte('<')
	case token.GTR:
		p.WriteByte('>')
	case token.SHL:
		p.WriteString("<<")
	case token.SHR:
		p.WriteString(">>")
	case token.RDRINOUT:
		p.WriteString("<>")
	case token.DPLIN:
		p.WriteString("<&")
	case token.DPLOUT:
		p.WriteString(">&")
	case token.DHEREDOC:
		p.WriteString("<<-")
	case token.WHEREDOC:
		p.WriteString("<<<")
	case token.RDRALL:
		p.WriteString("&>")
	default: // token.APPALL
		p.WriteString("&>>")
	}
}

func binaryCmdOp(tok token.Token) string {
	switch tok {
	case token.OR:
		return "|"
	case token.LAND:
		return "&&"
	case token.LOR:
		return "||"
	default: // token.PIPEALL
		return "|&"
	}
}

func caseClauseOp(tok token.Token) string {
	switch tok {
	case token.DSEMICOLON:
		return ";;"
	case token.SEMIFALL:
		return ";&"
	default: // token.DSEMIFALL
		return ";;&"
	}
}

func (p *printer) command(cmd ast.Command, redirs []*ast.Redirect) (startRedirs int) {
	switch x := cmd.(type) {
	case *ast.CallExpr:
		if len(x.Args) <= 1 {
			p.wordJoin(x.Args, true)
			return 0
		}
		p.wordJoin(x.Args[:1], true)
		for _, r := range redirs {
			if r.Pos() > x.Args[1].Pos() || r.Op == token.SHL || r.Op == token.DHEREDOC {
				break
			}
			if p.wantSpace {
				p.space()
			}
			if r.N != nil {
				p.WriteString(r.N.Value)
			}
			p.redirectOp(r.Op)
			p.wantSpace = true
			p.word(r.Word)
			startRedirs++
		}
		p.wordJoin(x.Args[1:], true)
	case *ast.Block:
		p.spacedRsrv("{")
		p.nestedStmts(x.Stmts)
		p.semiRsrv("}", x.Rbrace, true)
	case *ast.IfClause:
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
	case *ast.Subshell:
		p.spacedTok("(", false)
		if startsWithLparen(x.Stmts) {
			p.WriteByte(' ')
		}
		p.nestedStmts(x.Stmts)
		p.sepTok(")", x.Rparen)
	case *ast.WhileClause:
		p.spacedRsrv("while")
		p.cond(x.Cond)
		p.semiOrNewl("do", x.Do)
		p.nestedStmts(x.DoStmts)
		p.semiRsrv("done", x.Done, true)
	case *ast.ForClause:
		p.spacedRsrv("for ")
		p.loop(x.Loop)
		p.semiOrNewl("do", x.Do)
		p.nestedStmts(x.DoStmts)
		p.semiRsrv("done", x.Done, true)
	case *ast.BinaryCmd:
		p.stmt(x.X)
		indent := !p.nestedBinary
		if indent {
			p.incLevel()
		}
		_, p.nestedBinary = x.Y.Cmd.(*ast.BinaryCmd)
		if len(p.pendingHdocs) == 0 && x.Y.Pos() > p.nline {
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
	case *ast.FuncDecl:
		if x.BashStyle {
			p.WriteString("function ")
		}
		p.WriteString(x.Name.Value)
		p.WriteString("() ")
		p.incLines(x.Body.Pos())
		p.stmt(x.Body)
	case *ast.CaseClause:
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
				if p.wantSpace {
					p.WriteByte(' ')
				}
				for _, n := range w.Parts {
					p.wordPart(n)
				}
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
	case *ast.UntilClause:
		p.spacedRsrv("until")
		p.cond(x.Cond)
		p.semiOrNewl("do", x.Do)
		p.nestedStmts(x.DoStmts)
		p.semiRsrv("done", x.Done, true)
	case *ast.DeclClause:
		if x.Local {
			p.spacedRsrv("local")
		} else {
			p.spacedRsrv("declare")
		}
		for _, w := range x.Opts {
			p.WriteByte(' ')
			p.word(w)
		}
		p.assigns(x.Assigns)
	case *ast.EvalClause:
		p.spacedRsrv("eval")
		if x.Stmt != nil {
			p.stmt(x.Stmt)
		}
	case *ast.LetClause:
		p.spacedRsrv("let")
		for _, n := range x.Exprs {
			p.space()
			p.arithm(n, true)
		}
	}
	return startRedirs
}

func startsWithLparen(stmts []*ast.Stmt) bool {
	if len(stmts) < 1 {
		return false
	}
	_, ok := stmts[0].Cmd.(*ast.Subshell)
	return ok
}

func (p *printer) stmts(stmts []*ast.Stmt) {
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
		if p.nlineIndex > 0 {
			p.newlines(pos)
		}
		p.incLines(pos)
		p.stmt(s)
		if !p.hasInline(pos, p.nline) {
			inlineIndent = 0
			continue
		}
		if ind < len(p.f.Lines)-1 && s.End() > token.Pos(p.f.Lines[ind+1]) {
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
				if l := p.stmtLen(s2); l > inlineIndent {
					inlineIndent = l
				}
				ind2++
				if ind2 >= len(p.f.Lines) {
					nline2 = maxPos
				} else {
					nline2 = token.Pos(p.f.Lines[ind2])
				}
			}
		}
		p.wantSpaces = inlineIndent - p.stmtLen(s)
	}
	p.wantNewline = true
}

func (p *printer) stmtLen(s *ast.Stmt) int {
	if p.helperWriter == nil {
		p.helperBuf = new(bytes.Buffer)
		p.helperWriter = bufio.NewWriter(p.helperBuf)
	} else {
		p.helperWriter.Reset(p.helperBuf)
		p.helperBuf.Reset()
	}
	p2 := printer{Writer: p.helperWriter, f: p.f}
	p2.incLines(s.Pos())
	p2.stmt(s)
	return p.helperBuf.Len() + p.helperWriter.Buffered()
}

func (p *printer) nestedStmts(stmts []*ast.Stmt) {
	p.incLevel()
	p.stmts(stmts)
	p.decLevel()
}

func (p *printer) assigns(assigns []*ast.Assign) {
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
