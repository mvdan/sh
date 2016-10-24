// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package parser

import (
	"bytes"
	"fmt"
	"strconv"
	"sync"

	"github.com/mvdan/sh/ast"
	"github.com/mvdan/sh/token"
)

// Mode controls the parser behaviour via a set of flags.
type Mode uint

const (
	ParseComments   Mode = 1 << iota // add comments to the AST
	PosixConformant                  // match the POSIX standard where it differs from bash
)

var parserFree = sync.Pool{
	New: func() interface{} {
		return &parser{helperBuf: new(bytes.Buffer)}
	},
}

// Parse reads and parses a shell program with an optional name. It
// returns the parsed program if no issues were encountered. Otherwise,
// an error is returned.
func Parse(src []byte, name string, mode Mode) (*ast.File, error) {
	p := parserFree.Get().(*parser)
	p.reset()
	alloc := &struct {
		f ast.File
		l [16]int
	}{}
	p.f = &alloc.f
	p.f.Name = name
	p.f.Lines = alloc.l[:1]
	p.src, p.mode = src, mode
	p.next()
	p.f.Stmts = p.stmts()
	parserFree.Put(p)
	return p.f, p.err
}

type parser struct {
	src []byte

	f    *ast.File
	mode Mode

	spaced, newLine bool

	err error

	tok token.Token
	val string

	pos  token.Pos
	npos int

	quote quoteState
	asPos int

	// list of pending heredoc bodies
	buriedHdocs int
	heredocs    []*ast.Redirect
	hdocStop    []byte

	helperBuf *bytes.Buffer

	litBatch    []ast.Lit
	wpsBatch    []ast.WordPart
	stmtBatch   []ast.Stmt
	stListBatch []*ast.Stmt

	litBuf [32]byte
}

func (p *parser) lit(pos token.Pos, val string) *ast.Lit {
	if len(p.litBatch) == 0 {
		p.litBatch = make([]ast.Lit, 32)
	}
	l := &p.litBatch[0]
	l.ValuePos = pos
	l.Value = val
	p.litBatch = p.litBatch[1:]
	return l
}

func (p *parser) singleWps(wp ast.WordPart) []ast.WordPart {
	if len(p.wpsBatch) == 0 {
		p.wpsBatch = make([]ast.WordPart, 64)
	}
	wps := p.wpsBatch[:1:1]
	p.wpsBatch = p.wpsBatch[1:]
	wps[0] = wp
	return wps
}

func (p *parser) wps() []ast.WordPart {
	const c = 4
	if len(p.wpsBatch) < c {
		p.wpsBatch = make([]ast.WordPart, 64)
	}
	wps := p.wpsBatch[:0:c]
	p.wpsBatch = p.wpsBatch[c:]
	return wps
}

func (p *parser) stmt(pos token.Pos) *ast.Stmt {
	if len(p.stmtBatch) == 0 {
		p.stmtBatch = make([]ast.Stmt, 16)
	}
	s := &p.stmtBatch[0]
	s.Position = pos
	p.stmtBatch = p.stmtBatch[1:]
	return s
}

func (p *parser) stList() []*ast.Stmt {
	if len(p.stListBatch) == 0 {
		p.stListBatch = make([]*ast.Stmt, 128)
	}
	stmts := p.stListBatch[:0:4]
	p.stListBatch = p.stListBatch[4:]
	return stmts
}

type quoteState int

const (
	noState quoteState = 1 << iota
	subCmd
	subCmdBckquo
	sglQuotes
	dblQuotes
	hdocWord
	hdocBody
	hdocBodyTabs
	arithmExpr
	arithmExprLet
	arithmExprCmd
	arithmExprBrack
	testRegexp
	switchCase
	paramExpName
	paramExpInd
	paramExpRepl
	paramExpExp

	allRegTokens  = noState | subCmd | subCmdBckquo | hdocWord | switchCase
	allArithmExpr = arithmExpr | arithmExprLet | arithmExprCmd | arithmExprBrack
	allRbrack     = arithmExprBrack | paramExpInd
)

func (p *parser) bash() bool { return p.mode&PosixConformant == 0 }

func (p *parser) reset() {
	p.spaced, p.newLine = false, false
	p.err = nil
	p.npos = 0
	p.tok, p.quote = token.ILLEGAL, noState
	p.heredocs = p.heredocs[:0]
	p.buriedHdocs = 0
}

type saveState struct {
	quote       quoteState
	buriedHdocs int
}

func (p *parser) preNested(quote quoteState) (s saveState) {
	s.quote = p.quote
	s.buriedHdocs = p.buriedHdocs
	p.buriedHdocs = len(p.heredocs)
	p.quote = quote
	return
}

func (p *parser) postNested(s saveState) {
	p.quote, p.buriedHdocs = s.quote, s.buriedHdocs
}

func (p *parser) unquotedWordBytes(w ast.Word) ([]byte, bool) {
	p.helperBuf.Reset()
	didUnquote := false
	for _, wp := range w.Parts {
		if p.unquotedWordPart(p.helperBuf, wp) {
			didUnquote = true
		}
	}
	return p.helperBuf.Bytes(), didUnquote
}

func (p *parser) unquotedWordPart(b *bytes.Buffer, wp ast.WordPart) bool {
	switch x := wp.(type) {
	case *ast.Lit:
		if x.Value[0] == '\\' {
			b.WriteString(x.Value[1:])
			return true
		}
		b.WriteString(x.Value)
		return false
	case *ast.SglQuoted:
		b.WriteString(x.Value)
		return true
	case *ast.Quoted:
		for _, wp2 := range x.Parts {
			p.unquotedWordPart(b, wp2)
		}
		return true
	default:
		// catch-all for unusual cases such as ParamExp
		b.Write(p.src[wp.Pos()-1 : wp.End()-1])
		return false
	}
}

func (p *parser) doHeredocs() {
	p.tok = token.ILLEGAL
	old := p.quote
	hdocs := p.heredocs[p.buriedHdocs:]
	p.heredocs = p.heredocs[:p.buriedHdocs]
	for i, r := range hdocs {
		if r.Op == token.DHEREDOC {
			p.quote = hdocBodyTabs
		} else {
			p.quote = hdocBody
		}
		var quoted bool
		p.hdocStop, quoted = p.unquotedWordBytes(r.Word)
		if i > 0 && p.npos < len(p.src) && p.src[p.npos] == '\n' {
			p.npos++
			p.f.Lines = append(p.f.Lines, p.npos)
		}
		if !quoted {
			p.next()
			r.Hdoc = p.word()
			continue
		}
		r.Hdoc = p.hdocLitWord()

	}
	p.quote = old
}

func (p *parser) got(tok token.Token) bool {
	if p.tok == tok {
		p.next()
		return true
	}
	return false
}

func (p *parser) gotRsrv(val string) bool {
	if p.tok == token.LITWORD && p.val == val {
		p.next()
		return true
	}
	return false
}

func (p *parser) gotSameLine(tok token.Token) bool {
	if !p.newLine && p.tok == tok {
		p.next()
		return true
	}
	return false
}

func readableStr(s string) string {
	// don't quote tokens like & or }
	if s[0] >= 'a' && s[0] <= 'z' {
		return strconv.Quote(s)
	}
	return s
}

func (p *parser) followErr(pos token.Pos, left, right string) {
	leftStr := readableStr(left)
	p.posErr(pos, "%s must be followed by %s", leftStr, right)
}

func (p *parser) follow(lpos token.Pos, left string, tok token.Token) token.Pos {
	pos := p.pos
	if !p.got(tok) {
		p.followErr(lpos, left, tok.String())
	}
	return pos
}

func (p *parser) followRsrv(lpos token.Pos, left, val string) token.Pos {
	pos := p.pos
	if !p.gotRsrv(val) {
		p.followErr(lpos, left, fmt.Sprintf(`%q`, val))
	}
	return pos
}

func (p *parser) followStmts(left string, lpos token.Pos, stops ...string) []*ast.Stmt {
	if p.gotSameLine(token.SEMICOLON) {
		return nil
	}
	sts := p.stmts(stops...)
	if len(sts) < 1 && !p.newLine {
		p.followErr(lpos, left, "a statement list")
	}
	return sts
}

func (p *parser) followWordTok(tok token.Token, pos token.Pos) ast.Word {
	w := p.word()
	if w.Parts == nil {
		p.followErr(pos, tok.String(), "a word")
	}
	return w
}

func (p *parser) followWord(s string, pos token.Pos) ast.Word {
	w := p.word()
	if w.Parts == nil {
		p.followErr(pos, s, "a word")
	}
	return w
}

func (p *parser) stmtEnd(n ast.Node, start, end string) token.Pos {
	pos := p.pos
	if !p.gotRsrv(end) {
		p.posErr(n.Pos(), `%s statement must end with %q`, start, end)
	}
	return pos
}

func (p *parser) quoteErr(lpos token.Pos, quote token.Token) {
	p.posErr(lpos, `reached %s without closing quote %s`, p.tok, quote)
}

func (p *parser) matchingErr(lpos token.Pos, left, right token.Token) {
	p.posErr(lpos, `reached %s without matching %s with %s`, p.tok, left, right)
}

func (p *parser) matched(lpos token.Pos, left, right token.Token) token.Pos {
	pos := p.pos
	if !p.got(right) {
		p.matchingErr(lpos, left, right)
	}
	return pos
}

func (p *parser) errPass(err error) {
	if p.err == nil {
		p.err = err
		p.tok = token.EOF
	}
}

// ParseError represents an error found when parsing a source file.
type ParseError struct {
	token.Position
	Filename, Text string
}

func (e *ParseError) Error() string {
	prefix := ""
	if e.Filename != "" {
		prefix = e.Filename + ":"
	}
	return fmt.Sprintf("%s%d:%d: %s", prefix, e.Line, e.Column, e.Text)
}

func (p *parser) posErr(pos token.Pos, format string, a ...interface{}) {
	p.errPass(&ParseError{
		Position: p.f.Position(pos),
		Filename: p.f.Name,
		Text:     fmt.Sprintf(format, a...),
	})
}

func (p *parser) curErr(format string, a ...interface{}) {
	p.posErr(p.pos, format, a...)
}

func (p *parser) stmts(stops ...string) (sts []*ast.Stmt) {
	q := p.quote
	gotEnd := true
	for p.tok != token.EOF {
		switch p.tok {
		case token.LITWORD:
			for _, stop := range stops {
				if p.val == stop {
					return
				}
			}
		case token.RPAREN:
			if q == subCmd {
				return
			}
		case token.BQUOTE:
			if q == subCmdBckquo {
				return
			}
		case token.DSEMICOLON, token.SEMIFALL, token.DSEMIFALL:
			if q == switchCase {
				return
			}
			p.curErr("%s can only be used in a case clause", p.tok)
		}
		if !p.newLine && !gotEnd {
			p.curErr("statements must be separated by &, ; or a newline")
		}
		if p.tok == token.EOF {
			break
		}
		if s, end := p.getStmt(true); s == nil {
			p.invalidStmtStart()
		} else {
			if sts == nil {
				sts = p.stList()
			}
			sts = append(sts, s)
			gotEnd = end
		}
	}
	return
}

func (p *parser) invalidStmtStart() {
	switch p.tok {
	case token.SEMICOLON, token.AND, token.OR, token.LAND, token.LOR:
		p.curErr("%s can only immediately follow a statement", p.tok)
	case token.RPAREN:
		p.curErr("%s can only be used to close a subshell", p.tok)
	default:
		p.curErr("%s is not a valid start for a statement", p.tok)
	}
}

func (p *parser) word() ast.Word {
	if p.tok == token.LITWORD {
		w := ast.Word{Parts: p.singleWps(p.lit(p.pos, p.val))}
		p.next()
		return w
	}
	return ast.Word{Parts: p.wordParts()}
}

func (p *parser) gotLit(l *ast.Lit) bool {
	l.ValuePos = p.pos
	if p.tok == token.LIT || p.tok == token.LITWORD {
		l.Value = p.val
		p.next()
		return true
	}
	return false
}

func (p *parser) wordParts() (wps []ast.WordPart) {
	for {
		n := p.wordPart()
		if n == nil {
			return
		}
		if wps == nil {
			wps = p.wps()
		}
		wps = append(wps, n)
		if p.spaced {
			return
		}
	}
}

func (p *parser) wordPart() ast.WordPart {
	switch p.tok {
	case token.LIT, token.LITWORD:
		l := p.lit(p.pos, p.val)
		p.next()
		return l
	case token.DOLLBR:
		return p.paramExp()
	case token.DOLLDP, token.DOLLBK:
		left := p.tok
		ar := &ast.ArithmExp{Token: p.tok, Left: p.pos}
		old := p.preNested(arithmExpr)
		if ar.Token == token.DOLLBK {
			// treat deprecated $[ as $((
			ar.Token = token.DOLLDP
			p.quote = arithmExprBrack
		} else if !p.couldBeArithm() {
			p.postNested(old)
			p.npos = int(ar.Left) + 1
			p.tok = token.DOLLPR
			p.pos = ar.Left
			wp := p.wordPart()
			if p.err != nil {
				p.err = nil
				p.matchingErr(ar.Left, left, token.DRPAREN)
			}
			return wp
		}
		p.next()
		ar.X = p.arithmExpr(ar.Token, ar.Left, 0, false)
		if left == token.DOLLBK {
			if p.tok != token.RBRACK {
				p.matchingErr(ar.Left, left, token.RBRACK)
			}
			p.postNested(old)
			ar.Right = p.pos
			p.next()
		} else {
			ar.Right = p.arithmEnd(left, ar.Left, old)
		}
		return ar
	case token.DOLLPR:
		if p.quote == hdocWord {
			p.curErr("nested statements not allowed in heredoc words")
		}
		cs := &ast.CmdSubst{Left: p.pos}
		old := p.preNested(subCmd)
		p.next()
		cs.Stmts = p.stmts()
		p.postNested(old)
		cs.Right = p.matched(cs.Left, token.LPAREN, token.RPAREN)
		return cs
	case token.DOLLAR:
		var b byte
		if p.npos >= len(p.src) {
			p.tok = token.EOF
		} else {
			b = p.src[p.npos]
		}
		if p.tok == token.EOF || wordBreak(b) || b == '"' || b == '\'' || b == '`' || b == '[' {
			l := p.lit(p.pos, "$")
			p.next()
			return l
		}
		pe := &ast.ParamExp{Dollar: p.pos, Short: true}
		p.pos++
		switch b {
		case '@', '*', '#', '$', '?', '!', '0', '-':
			p.npos++
			p.tok, p.val = token.LIT, string(b)
		default:
			p.advanceLitOther(p.quote)
		}
		p.gotLit(&pe.Param)
		return pe
	case token.CMDIN, token.CMDOUT:
		ps := &ast.ProcSubst{Op: p.tok, OpPos: p.pos}
		old := p.preNested(subCmd)
		p.next()
		ps.Stmts = p.stmts()
		p.postNested(old)
		ps.Rparen = p.matched(ps.OpPos, ps.Op, token.RPAREN)
		return ps
	case token.SQUOTE:
		sq := &ast.SglQuoted{Quote: p.tok, QuotePos: p.pos}
		bs, found := p.readUntil('\'')
		rem := bs
		for {
			i := bytes.IndexByte(rem, '\n')
			if i < 0 {
				p.npos += len(rem)
				break
			}
			p.npos += i + 1
			p.f.Lines = append(p.f.Lines, p.npos)
			rem = rem[i+1:]
		}
		p.npos++
		if !found {
			p.posErr(sq.Pos(), `reached EOF without closing quote %s`, token.SQUOTE)
		}
		sq.Value = string(bs)
		p.next()
		return sq
	case token.DOLLSQ:
		sq := &ast.SglQuoted{Quote: p.tok, QuotePos: p.pos}
		old := p.quote
		p.quote = sglQuotes
		p.next()
		if p.tok == token.SQUOTE {
			p.quote = old
		} else {
			sq.Value = p.val
			p.quote = old
			p.next()
		}
		if !p.got(token.SQUOTE) {
			p.quoteErr(sq.Pos(), token.SQUOTE)
		}
		return sq
	case token.DQUOTE:
		if p.quote == dblQuotes {
			return nil
		}
		fallthrough
	case token.DOLLDQ:
		q := &ast.Quoted{Quote: p.tok, QuotePos: p.pos}
		old := p.quote
		p.quote = dblQuotes
		p.next()
		if p.tok == token.LITWORD {
			q.Parts = p.singleWps(p.lit(p.pos, p.val))
			p.next()
		} else {
			q.Parts = p.wordParts()
		}
		p.quote = old
		if !p.got(token.DQUOTE) {
			p.quoteErr(q.Pos(), token.DQUOTE)
		}
		return q
	case token.BQUOTE:
		switch p.quote {
		case hdocWord:
			p.curErr("nested statements not allowed in heredoc words")
		case subCmdBckquo:
			return nil
		}
		cs := &ast.CmdSubst{Backquotes: true, Left: p.pos}
		old := p.preNested(subCmdBckquo)
		p.next()
		cs.Stmts = p.stmts()
		p.postNested(old)
		cs.Right = p.pos
		if !p.got(token.BQUOTE) {
			p.quoteErr(cs.Pos(), token.BQUOTE)
		}
		return cs
	case token.GQUEST, token.GMUL, token.GADD, token.GAT, token.GNOT:
		eg := &ast.ExtGlob{Token: p.tok}
		eg.Pattern.ValuePos = token.Pos(p.npos + 1)
		start := p.npos
		lparens := 0
		for _, b := range p.src[start:] {
			p.npos++
			if b == '(' {
				lparens++
			} else if b == ')' {
				if lparens--; lparens < 0 {
					break
				}
			}
		}
		eg.Pattern.Value = string(p.src[start : p.npos-1])
		p.next()
		if lparens != -1 {
			p.matchingErr(p.pos, eg.Token, token.RPAREN)
		}
		return eg
	}
	return nil
}

func (p *parser) couldBeArithm() (could bool) {
	// save state
	oldTok := p.tok
	oldNpos := p.npos
	oldLines := len(p.f.Lines)
	p.next()
	lparens := 0
	for p.tok != token.EOF {
		if p.peekArithmEnd() {
			could = true
			break
		}
		if p.tok == token.LPAREN {
			lparens++
		} else if p.tok == token.RPAREN {
			if lparens--; lparens < 0 {
				break
			}
		}
		p.next()
	}
	// recover state
	p.tok = oldTok
	p.npos = oldNpos
	p.f.Lines = p.f.Lines[:oldLines]
	return
}

func arithmOpLevel(tok token.Token) int {
	switch tok {
	case token.COMMA:
		return 0
	case token.ADDASSGN, token.SUBASSGN, token.MULASSGN, token.QUOASSGN,
		token.REMASSGN, token.ANDASSGN, token.ORASSGN, token.XORASSGN,
		token.SHLASSGN, token.SHRASSGN:
		return 1
	case token.ASSIGN:
		return 2
	case token.QUEST, token.COLON:
		return 3
	case token.LOR:
		return 4
	case token.LAND:
		return 5
	case token.AND, token.OR, token.XOR:
		return 5
	case token.EQL, token.NEQ:
		return 6
	case token.LSS, token.GTR, token.LEQ, token.GEQ:
		return 7
	case token.SHL, token.SHR:
		return 8
	case token.ADD, token.SUB:
		return 9
	case token.MUL, token.QUO, token.REM:
		return 10
	case token.POW:
		return 11
	}
	return -1
}

func (p *parser) arithmExpr(ftok token.Token, fpos token.Pos, level int, compact bool) ast.ArithmExpr {
	if p.tok == token.EOF || p.peekArithmEnd() {
		return nil
	}
	var left ast.ArithmExpr
	if level > 11 {
		left = p.arithmExprBase(ftok, fpos, compact)
	} else {
		left = p.arithmExpr(ftok, fpos, level+1, compact)
	}
	if compact && p.spaced {
		return left
	}
	newLevel := arithmOpLevel(p.tok)
	if newLevel < 0 {
		switch p.tok {
		case token.LIT, token.LITWORD:
			p.curErr("not a valid arithmetic operator: %s", p.val)
			newLevel = 0
		case token.RPAREN, token.EOF:
		default:
			if p.quote == arithmExpr {
				p.curErr("not a valid arithmetic operator: %v", p.tok)
				newLevel = 0
			}
		}
	}
	if newLevel < 0 || newLevel < level {
		return left
	}
	b := &ast.BinaryExpr{
		OpPos: p.pos,
		Op:    p.tok,
		X:     left,
	}
	if p.next(); compact && p.spaced {
		p.followErr(b.OpPos, b.Op.String(), "an expression")
	}
	if b.Y = p.arithmExpr(b.Op, b.OpPos, newLevel, compact); b.Y == nil {
		p.followErr(b.OpPos, b.Op.String(), "an expression")
	}
	return b
}

func (p *parser) arithmExprBase(ftok token.Token, fpos token.Pos, compact bool) ast.ArithmExpr {
	var x ast.ArithmExpr
	switch p.tok {
	case token.INC, token.DEC, token.NOT:
		pre := &ast.UnaryExpr{OpPos: p.pos, Op: p.tok}
		p.next()
		pre.X = p.arithmExprBase(pre.Op, pre.OpPos, compact)
		return pre
	case token.LPAREN:
		pe := &ast.ParenExpr{Lparen: p.pos}
		p.next()
		if pe.X = p.arithmExpr(token.LPAREN, pe.Lparen, 0, false); pe.X == nil {
			p.posErr(pe.Lparen, "parentheses must enclose an expression")
		}
		pe.Rparen = p.matched(pe.Lparen, token.LPAREN, token.RPAREN)
		x = pe
	case token.ADD, token.SUB:
		ue := &ast.UnaryExpr{OpPos: p.pos, Op: p.tok}
		if p.next(); compact && p.spaced {
			p.followErr(ue.OpPos, ue.Op.String(), "an expression")
		}
		if ue.X = p.arithmExpr(ue.Op, ue.OpPos, 0, compact); ue.X == nil {
			p.followErr(ue.OpPos, ue.Op.String(), "an expression")
		}
		x = ue
	default:
		w := p.followWordTok(ftok, fpos)
		x = &w
	}
	if compact && p.spaced {
		return x
	}
	if p.tok == token.INC || p.tok == token.DEC {
		u := &ast.UnaryExpr{
			Post:  true,
			OpPos: p.pos,
			Op:    p.tok,
			X:     x,
		}
		p.next()
		return u
	}
	return x
}

func (p *parser) gotParamLit(l *ast.Lit) bool {
	l.ValuePos = p.pos
	switch p.tok {
	case token.LIT, token.LITWORD:
		l.Value = p.val
	case token.DOLLAR:
		l.Value = "$"
	case token.QUEST:
		l.Value = "?"
	case token.HASH:
		l.Value = "#"
	case token.SUB:
		l.Value = "-"
	default:
		return false
	}
	p.next()
	return true
}

func (p *parser) paramExp() *ast.ParamExp {
	pe := &ast.ParamExp{Dollar: p.pos}
	old := p.preNested(paramExpName)
	p.next()
	switch p.tok {
	case token.DHASH:
		p.tok = token.HASH
		p.npos--
		fallthrough
	case token.HASH:
		if p.npos < len(p.src) && p.src[p.npos] != '}' {
			pe.Length = true
			p.next()
		}
	}
	if !p.gotParamLit(&pe.Param) && !pe.Length {
		p.posErr(pe.Dollar, "parameter expansion requires a literal")
	}
	if p.tok == token.RBRACE {
		p.postNested(old)
		p.next()
		return pe
	}
	if p.tok == token.LBRACK {
		lpos := p.pos
		p.quote = paramExpInd
		p.next()
		pe.Ind = &ast.Index{Word: p.word()}
		p.quote = paramExpName
		p.matched(lpos, token.LBRACK, token.RBRACK)
	}
	switch p.tok {
	case token.RBRACE:
		p.postNested(old)
		p.next()
		return pe
	case token.QUO, token.DQUO:
		pe.Repl = &ast.Replace{All: p.tok == token.DQUO}
		p.quote = paramExpRepl
		p.next()
		pe.Repl.Orig = p.word()
		if p.tok == token.QUO {
			p.quote = paramExpExp
			p.next()
			pe.Repl.With = p.word()
		}
	default:
		pe.Exp = &ast.Expansion{Op: p.tok}
		p.quote = paramExpExp
		p.next()
		pe.Exp.Word = p.word()
	}
	p.postNested(old)
	p.matched(pe.Dollar, token.DOLLBR, token.RBRACE)
	return pe
}

func (p *parser) peekArithmEnd() bool {
	return p.tok == token.RPAREN && p.npos < len(p.src) && p.src[p.npos] == ')'
}

func (p *parser) arithmEnd(ltok token.Token, lpos token.Pos, old saveState) token.Pos {
	if p.peekArithmEnd() {
		p.npos++
	} else {
		p.matchingErr(lpos, ltok, token.DRPAREN)
	}
	p.postNested(old)
	pos := p.pos
	p.next()
	return pos
}

func stopToken(tok token.Token) bool {
	return tok == token.EOF || tok == token.SEMICOLON || tok == token.AND ||
		tok == token.OR || tok == token.LAND || tok == token.LOR ||
		tok == token.PIPEALL || tok == token.DSEMICOLON ||
		tok == token.SEMIFALL || tok == token.DSEMIFALL ||
		tok == token.RPAREN
}

func (p *parser) validIdent() bool {
	if p.asPos <= 0 {
		return false
	}
	s := p.val[:p.asPos]
	for i, c := range s {
		switch {
		case 'a' <= c && c <= 'z':
		case 'A' <= c && c <= 'Z':
		case c == '_':
		case i > 0 && '0' <= c && c <= '9':
		case i > 0 && (c == '[' || c == ']') && p.bash():
		default:
			return false
		}
	}
	return true
}

func (p *parser) getAssign() *ast.Assign {
	asPos := p.asPos
	as := &ast.Assign{Name: p.lit(p.pos, p.val[:asPos])}
	if p.val[asPos] == '+' {
		as.Append = true
		asPos++
	}
	start := p.lit(p.pos+1, p.val[asPos+1:])
	if start.Value != "" {
		start.ValuePos += token.Pos(asPos)
		as.Value.Parts = p.singleWps(start)
	}
	p.next()
	if p.spaced {
		return as
	}
	if start.Value == "" && p.tok == token.LPAREN && p.bash() {
		ae := &ast.ArrayExpr{Lparen: p.pos}
		p.next()
		for p.tok != token.EOF && p.tok != token.RPAREN {
			if w := p.word(); w.Parts == nil {
				p.curErr("array elements must be words")
			} else {
				ae.List = append(ae.List, w)
			}
		}
		ae.Rparen = p.matched(ae.Lparen, token.LPAREN, token.RPAREN)
		as.Value.Parts = p.singleWps(ae)
	} else if !p.newLine && !stopToken(p.tok) {
		if w := p.word(); start.Value == "" {
			as.Value = w
		} else {
			as.Value.Parts = append(as.Value.Parts, w.Parts...)
		}
	}
	return as
}

func litRedir(src []byte, npos int) bool {
	return npos < len(src) && (src[npos] == '>' || src[npos] == '<')
}

func (p *parser) peekRedir() bool {
	switch p.tok {
	case token.LITWORD:
		return litRedir(p.src, p.npos)
	case token.GTR, token.SHR, token.LSS, token.DPLIN, token.DPLOUT,
		token.CLBOUT, token.RDRINOUT, token.SHL, token.DHEREDOC,
		token.WHEREDOC, token.RDRALL, token.APPALL:
		return true
	}
	return false
}

func (p *parser) doRedirect(s *ast.Stmt) {
	r := &ast.Redirect{}
	var l ast.Lit
	if p.gotLit(&l) {
		r.N = &l
	}
	r.Op, r.OpPos = p.tok, p.pos
	p.next()
	switch r.Op {
	case token.SHL, token.DHEREDOC:
		old := p.quote
		p.quote = hdocWord
		if p.newLine {
			p.curErr("heredoc stop word must be on the same line")
		}
		p.heredocs = append(p.heredocs, r)
		r.Word = p.followWordTok(r.Op, r.OpPos)
		p.quote = old
		p.next()
	default:
		if p.newLine {
			p.curErr("redirect word must be on the same line")
		}
		r.Word = p.followWordTok(r.Op, r.OpPos)
	}
	s.Redirs = append(s.Redirs, r)
}

func (p *parser) getStmt(readEnd bool) (s *ast.Stmt, gotEnd bool) {
	s = p.stmt(p.pos)
	if p.gotRsrv("!") {
		s.Negated = true
	}
preLoop:
	for {
		switch p.tok {
		case token.LIT, token.LITWORD:
			if p.validIdent() {
				s.Assigns = append(s.Assigns, p.getAssign())
			} else if litRedir(p.src, p.npos) {
				p.doRedirect(s)
			} else {
				break preLoop
			}
		case token.GTR, token.SHR, token.LSS, token.DPLIN, token.DPLOUT,
			token.CLBOUT, token.RDRINOUT, token.SHL, token.DHEREDOC,
			token.WHEREDOC, token.RDRALL, token.APPALL:
			p.doRedirect(s)
		default:
			break preLoop
		}
		switch {
		case p.newLine, p.tok == token.EOF:
			return
		case p.tok == token.SEMICOLON:
			if readEnd {
				p.next()
				gotEnd = true
			}
			return
		}
	}
	if s = p.gotStmtPipe(s); s == nil {
		return
	}
	switch p.tok {
	case token.LAND, token.LOR:
		b := &ast.BinaryCmd{OpPos: p.pos, Op: p.tok, X: s}
		p.next()
		if b.Y, _ = p.getStmt(false); b.Y == nil {
			p.followErr(b.OpPos, b.Op.String(), "a statement")
		}
		s = p.stmt(s.Position)
		s.Cmd = b
		if readEnd && p.gotSameLine(token.SEMICOLON) {
			gotEnd = true
		}
	case token.AND:
		p.next()
		s.Background = true
		gotEnd = true
	case token.SEMICOLON:
		if !p.newLine && readEnd {
			p.next()
			gotEnd = true
		}
	}
	return
}

func bashDeclareWord(s string) bool {
	return s == "declare" || s == "local" || s == "export" ||
		s == "readonly" || s == "typeset" || s == "nameref"
}

func (p *parser) gotStmtPipe(s *ast.Stmt) *ast.Stmt {
	switch p.tok {
	case token.LPAREN:
		s.Cmd = p.subshell()
	case token.DLPAREN:
		s.Cmd = p.arithmExpCmd()
	case token.LITWORD:
		switch {
		case p.val == "}":
			p.curErr("%s can only be used to close a block", p.val)
		case p.val == "{":
			s.Cmd = p.block()
		case p.val == "if":
			s.Cmd = p.ifClause()
		case p.val == "while":
			s.Cmd = p.whileClause()
		case p.val == "until":
			s.Cmd = p.untilClause()
		case p.val == "for":
			s.Cmd = p.forClause()
		case p.val == "case":
			s.Cmd = p.caseClause()
		case p.bash() && p.val == "[[":
			s.Cmd = p.testClause()
		case p.bash() && bashDeclareWord(p.val):
			s.Cmd = p.declClause()
		case p.bash() && p.val == "eval":
			s.Cmd = p.evalClause()
		case p.bash() && p.val == "coproc":
			s.Cmd = p.coprocClause()
		case p.bash() && p.val == "let":
			s.Cmd = p.letClause()
		case p.bash() && p.val == "function":
			s.Cmd = p.bashFuncDecl()
		default:
			name := ast.Lit{ValuePos: p.pos, Value: p.val}
			p.next()
			if p.gotSameLine(token.LPAREN) {
				p.follow(name.ValuePos, "foo(", token.RPAREN)
				s.Cmd = p.funcDecl(name, name.ValuePos)
			} else {
				s.Cmd = p.callExpr(s, ast.Word{
					Parts: p.singleWps(&name),
				})
			}
		}
	case token.LIT, token.DOLLBR, token.DOLLDP, token.DOLLPR, token.DOLLAR,
		token.CMDIN, token.CMDOUT, token.SQUOTE, token.DOLLSQ,
		token.DQUOTE, token.DOLLDQ, token.BQUOTE, token.DOLLBK,
		token.GQUEST, token.GMUL, token.GADD, token.GAT, token.GNOT:
		w := ast.Word{Parts: p.wordParts()}
		if p.gotSameLine(token.LPAREN) && p.err == nil {
			rawName := string(p.src[w.Pos()-1 : w.End()-1])
			p.posErr(w.Pos(), "invalid func name: %q", rawName)
		}
		s.Cmd = p.callExpr(s, w)
	}
	for !p.newLine && p.peekRedir() {
		p.doRedirect(s)
	}
	if s.Cmd == nil && len(s.Redirs) == 0 && !s.Negated && len(s.Assigns) == 0 {
		return nil
	}
	if p.tok == token.OR || p.tok == token.PIPEALL {
		b := &ast.BinaryCmd{OpPos: p.pos, Op: p.tok, X: s}
		p.next()
		if b.Y = p.gotStmtPipe(p.stmt(p.pos)); b.Y == nil {
			p.followErr(b.OpPos, b.Op.String(), "a statement")
		}
		s = p.stmt(s.Position)
		s.Cmd = b
	}
	return s
}

func (p *parser) subshell() *ast.Subshell {
	s := &ast.Subshell{Lparen: p.pos}
	old := p.preNested(subCmd)
	p.next()
	s.Stmts = p.stmts()
	p.postNested(old)
	s.Rparen = p.matched(s.Lparen, token.LPAREN, token.RPAREN)
	return s
}

func (p *parser) arithmExpCmd() ast.Command {
	ar := &ast.ArithmExp{Token: p.tok, Left: p.pos}
	old := p.preNested(arithmExprCmd)
	if !p.couldBeArithm() {
		p.postNested(old)
		p.npos = int(ar.Left)
		p.tok = token.LPAREN
		p.pos = ar.Left
		s := p.subshell()
		if p.err != nil {
			p.err = nil
			p.matchingErr(ar.Left, token.DLPAREN, token.DRPAREN)
		}
		return s
	}
	p.next()
	ar.X = p.arithmExpr(ar.Token, ar.Left, 0, false)
	ar.Right = p.arithmEnd(ar.Token, ar.Left, old)
	return ar
}

func (p *parser) block() *ast.Block {
	b := &ast.Block{Lbrace: p.pos}
	p.next()
	b.Stmts = p.stmts("}")
	b.Rbrace = p.pos
	if !p.gotRsrv("}") {
		p.matchingErr(b.Lbrace, token.LBRACE, token.RBRACE)
	}
	return b
}

func (p *parser) ifClause() *ast.IfClause {
	ic := &ast.IfClause{If: p.pos}
	p.next()
	ic.CondStmts = p.followStmts("if", ic.If, "then")
	ic.Then = p.followRsrv(ic.If, "if <cond>", "then")
	ic.ThenStmts = p.followStmts("then", ic.Then, "fi", "elif", "else")
	elifPos := p.pos
	for p.gotRsrv("elif") {
		elf := &ast.Elif{Elif: elifPos}
		elf.CondStmts = p.followStmts("elif", elf.Elif, "then")
		elf.Then = p.followRsrv(elf.Elif, "elif <cond>", "then")
		elf.ThenStmts = p.followStmts("then", elf.Then, "fi", "elif", "else")
		ic.Elifs = append(ic.Elifs, elf)
		elifPos = p.pos
	}
	if elsePos := p.pos; p.gotRsrv("else") {
		ic.Else = elsePos
		ic.ElseStmts = p.followStmts("else", ic.Else, "fi")
	}
	ic.Fi = p.stmtEnd(ic, "if", "fi")
	return ic
}

func (p *parser) whileClause() *ast.WhileClause {
	wc := &ast.WhileClause{While: p.pos}
	p.next()
	wc.CondStmts = p.followStmts("while", wc.While, "do")
	wc.Do = p.followRsrv(wc.While, "while <cond>", "do")
	wc.DoStmts = p.followStmts("do", wc.Do, "done")
	wc.Done = p.stmtEnd(wc, "while", "done")
	return wc
}

func (p *parser) untilClause() *ast.UntilClause {
	uc := &ast.UntilClause{Until: p.pos}
	p.next()
	uc.CondStmts = p.followStmts("until", uc.Until, "do")
	uc.Do = p.followRsrv(uc.Until, "until <cond>", "do")
	uc.DoStmts = p.followStmts("do", uc.Do, "done")
	uc.Done = p.stmtEnd(uc, "until", "done")
	return uc
}

func (p *parser) forClause() *ast.ForClause {
	fc := &ast.ForClause{For: p.pos}
	p.next()
	fc.Loop = p.loop(fc.For)
	fc.Do = p.followRsrv(fc.For, "for foo [in words]", "do")
	fc.DoStmts = p.followStmts("do", fc.Do, "done")
	fc.Done = p.stmtEnd(fc, "for", "done")
	return fc
}

func (p *parser) loop(forPos token.Pos) ast.Loop {
	if p.tok == token.DLPAREN {
		cl := &ast.CStyleLoop{Lparen: p.pos}
		old := p.preNested(arithmExprCmd)
		p.next()
		if p.tok == token.DSEMICOLON {
			p.npos--
			p.tok = token.SEMICOLON
		}
		if p.tok != token.SEMICOLON {
			cl.Init = p.arithmExpr(token.DLPAREN, cl.Lparen, 0, false)
		}
		scPos := p.pos
		p.follow(p.pos, "expression", token.SEMICOLON)
		if p.tok != token.SEMICOLON {
			cl.Cond = p.arithmExpr(token.SEMICOLON, scPos, 0, false)
		}
		scPos = p.pos
		p.follow(p.pos, "expression", token.SEMICOLON)
		if p.tok != token.SEMICOLON {
			cl.Post = p.arithmExpr(token.SEMICOLON, scPos, 0, false)
		}
		cl.Rparen = p.arithmEnd(token.DLPAREN, cl.Lparen, old)
		p.gotSameLine(token.SEMICOLON)
		return cl
	}
	wi := &ast.WordIter{}
	if !p.gotLit(&wi.Name) {
		p.followErr(forPos, "for", "a literal")
	}
	if p.gotRsrv("in") {
		for !p.newLine && p.tok != token.EOF && p.tok != token.SEMICOLON {
			if w := p.word(); w.Parts == nil {
				p.curErr("word list can only contain words")
			} else {
				wi.List = append(wi.List, w)
			}
		}
		p.gotSameLine(token.SEMICOLON)
	} else if !p.newLine && !p.got(token.SEMICOLON) {
		p.followErr(forPos, "for foo", `"in", ; or a newline`)
	}
	return wi
}

func (p *parser) caseClause() *ast.CaseClause {
	cc := &ast.CaseClause{Case: p.pos}
	p.next()
	cc.Word = p.followWord("case", cc.Case)
	p.followRsrv(cc.Case, "case x", "in")
	cc.List = p.patLists()
	cc.Esac = p.stmtEnd(cc, "case", "esac")
	return cc
}

func (p *parser) patLists() (pls []*ast.PatternList) {
	for p.tok != token.EOF && !(p.tok == token.LITWORD && p.val == "esac") {
		pl := &ast.PatternList{}
		p.got(token.LPAREN)
		for p.tok != token.EOF {
			if w := p.word(); w.Parts == nil {
				p.curErr("case patterns must consist of words")
			} else {
				pl.Patterns = append(pl.Patterns, w)
			}
			if p.tok == token.RPAREN {
				break
			}
			if !p.got(token.OR) {
				p.curErr("case patterns must be separated with |")
			}
		}
		old := p.preNested(switchCase)
		p.next()
		pl.Stmts = p.stmts("esac")
		p.postNested(old)
		pl.OpPos = p.pos
		if p.tok != token.DSEMICOLON && p.tok != token.SEMIFALL && p.tok != token.DSEMIFALL {
			pl.Op = token.DSEMICOLON
			pls = append(pls, pl)
			break
		}
		pl.Op = p.tok
		p.next()
		pls = append(pls, pl)
	}
	return
}

func (p *parser) testClause() *ast.TestClause {
	tc := &ast.TestClause{Left: p.pos}
	p.next()
	if p.tok == token.EOF || p.gotRsrv("]]") {
		p.posErr(tc.Left, `test clause requires at least one expression`)
	}
	tc.X = p.testExpr(token.DLBRCK, tc.Left)
	tc.Right = p.pos
	if !p.gotRsrv("]]") {
		p.matchingErr(tc.Left, token.DLBRCK, token.DRBRCK)
	}
	return tc
}

func (p *parser) testExpr(ftok token.Token, fpos token.Pos) ast.ArithmExpr {
	if p.tok == token.EOF || (p.tok == token.LITWORD && p.val == "]]") {
		return nil
	}
	if p.tok == token.LITWORD {
		if op := testUnaryOp(p.val); op != token.ILLEGAL {
			p.tok = op
		}
	}
	if p.tok == token.NOT {
		u := &ast.UnaryExpr{OpPos: p.pos, Op: p.tok}
		p.next()
		u.X = p.testExpr(u.Op, u.OpPos)
		return u
	}
	var left ast.ArithmExpr
	switch p.tok {
	case token.TEXISTS, token.TREGFILE, token.TDIRECT, token.TCHARSP,
		token.TBLCKSP, token.TNMPIPE, token.TSOCKET, token.TSMBLINK,
		token.TSGIDSET, token.TSUIDSET, token.TREAD, token.TWRITE,
		token.TEXEC, token.TNOEMPTY, token.TFDTERM, token.TEMPSTR,
		token.TNEMPSTR, token.TOPTSET, token.TVARSET, token.TNRFVAR:
		u := &ast.UnaryExpr{OpPos: p.pos, Op: p.tok}
		p.next()
		w := p.followWordTok(ftok, fpos)
		u.X = &w
		left = u
	case token.LPAREN:
		pe := &ast.ParenExpr{Lparen: p.pos}
		p.next()
		if pe.X = p.testExpr(token.LPAREN, pe.Lparen); pe.X == nil {
			p.posErr(pe.Lparen, "parentheses must enclose an expression")
		}
		pe.Rparen = p.matched(pe.Lparen, token.LPAREN, token.RPAREN)
		left = pe
	case token.RPAREN:
		return nil
	default:
		w := p.followWordTok(ftok, fpos)
		left = &w
	}
	if p.tok == token.EOF || (p.tok == token.LITWORD && p.val == "]]") {
		return left
	}
	switch p.tok {
	case token.LAND, token.LOR, token.LSS, token.GTR:
	case token.LITWORD:
		if p.tok = testBinaryOp(p.val); p.tok == token.ILLEGAL {
			p.curErr("not a valid test operator: %s", p.val)
		}
	case token.RPAREN:
		return left
	default:
		p.curErr("not a valid test operator: %v", p.tok)
	}
	b := &ast.BinaryExpr{
		OpPos: p.pos,
		Op:    p.tok,
		X:     left,
	}
	if p.tok == token.TREMATCH {
		old := p.preNested(testRegexp)
		p.next()
		p.postNested(old)
	} else {
		p.next()
	}
	if b.Y = p.testExpr(b.Op, b.OpPos); b.Y == nil {
		p.followErr(b.OpPos, b.Op.String(), "an expression")
	}
	return b
}

func testUnaryOp(val string) token.Token {
	switch val {
	case "!":
		return token.NOT
	case "-e", "-a":
		return token.TEXISTS
	case "-f":
		return token.TREGFILE
	case "-d":
		return token.TDIRECT
	case "-c":
		return token.TCHARSP
	case "-b":
		return token.TBLCKSP
	case "-p":
		return token.TNMPIPE
	case "-S":
		return token.TSOCKET
	case "-L", "-h":
		return token.TSMBLINK
	case "-g":
		return token.TSGIDSET
	case "-u":
		return token.TSUIDSET
	case "-r":
		return token.TREAD
	case "-w":
		return token.TWRITE
	case "-x":
		return token.TEXEC
	case "-s":
		return token.TNOEMPTY
	case "-t":
		return token.TFDTERM
	case "-z":
		return token.TEMPSTR
	case "-n":
		return token.TNEMPSTR
	case "-o":
		return token.TOPTSET
	case "-v":
		return token.TVARSET
	case "-R":
		return token.TNRFVAR
	default:
		return token.ILLEGAL
	}
}

func testBinaryOp(val string) token.Token {
	switch val {
	case "=":
		return token.ASSIGN
	case "==":
		return token.EQL
	case "=~":
		return token.TREMATCH
	case "!=":
		return token.NEQ
	case "-nt":
		return token.TNEWER
	case "-ot":
		return token.TOLDER
	case "-ef":
		return token.TDEVIND
	case "-eq":
		return token.TEQL
	case "-ne":
		return token.TNEQ
	case "-le":
		return token.TLEQ
	case "-ge":
		return token.TGEQ
	case "-lt":
		return token.TLSS
	case "-gt":
		return token.TGTR
	default:
		return token.ILLEGAL
	}
}

func (p *parser) declClause() *ast.DeclClause {
	name := p.val
	ds := &ast.DeclClause{Position: p.pos}
	switch name {
	case "declare", "typeset": // typeset is an obsolete synonym
	default:
		ds.Variant = name
	}
	p.next()
	for p.tok == token.LITWORD && p.val[0] == '-' {
		ds.Opts = append(ds.Opts, p.word())
	}
	for !p.newLine && !stopToken(p.tok) && !p.peekRedir() {
		if (p.tok == token.LIT || p.tok == token.LITWORD) && p.validIdent() {
			ds.Assigns = append(ds.Assigns, p.getAssign())
		} else if w := p.word(); w.Parts == nil {
			p.followErr(p.pos, name, "words")
		} else {
			ds.Assigns = append(ds.Assigns, &ast.Assign{Value: w})
		}
	}
	return ds
}

func (p *parser) evalClause() *ast.EvalClause {
	ec := &ast.EvalClause{Eval: p.pos}
	p.next()
	ec.Stmt, _ = p.getStmt(false)
	return ec
}

func isBashCompoundCommand(tok token.Token, val string) bool {
	switch tok {
	case token.LPAREN, token.DLPAREN:
		return true
	case token.LITWORD:
		switch val {
		case "{", "if", "while", "until", "for", "case", "[[", "eval",
			"coproc", "let", "function":
			return true
		}
		if bashDeclareWord(val) {
			return true
		}
	}
	return false
}

func (p *parser) coprocClause() *ast.CoprocClause {
	cc := &ast.CoprocClause{Coproc: p.pos}
	p.next()
	if isBashCompoundCommand(p.tok, p.val) {
		// has no name
		cc.Stmt, _ = p.getStmt(false)
		return cc
	}
	var l ast.Lit
	if p.gotLit(&l) {
		cc.Name = &l
	}
	cc.Stmt, _ = p.getStmt(false)
	if cc.Stmt == nil {
		// name was in fact the stmt
		cc.Stmt = &ast.Stmt{
			Position: cc.Name.ValuePos,
			Cmd: &ast.CallExpr{Args: []ast.Word{
				{Parts: p.singleWps(cc.Name)},
			}},
		}
		cc.Name = nil
	} else if call, ok := cc.Stmt.Cmd.(*ast.CallExpr); ok {
		// name was in fact the start of a call
		call.Args = append([]ast.Word{{Parts: p.singleWps(cc.Name)}},
			call.Args...)
		cc.Name = nil
	}
	return cc
}

func (p *parser) letClause() *ast.LetClause {
	lc := &ast.LetClause{Let: p.pos}
	old := p.preNested(arithmExprLet)
	p.next()
	for !p.newLine && !stopToken(p.tok) && !p.peekRedir() {
		x := p.arithmExpr(token.LET, lc.Let, 0, true)
		if x == nil {
			break
		}
		lc.Exprs = append(lc.Exprs, x)
	}
	if len(lc.Exprs) == 0 {
		p.posErr(lc.Let, "let clause requires at least one expression")
	}
	p.postNested(old)
	if p.tok == token.ILLEGAL {
		p.next()
	}
	return lc
}

func (p *parser) bashFuncDecl() *ast.FuncDecl {
	fpos := p.pos
	p.next()
	if p.tok != token.LITWORD {
		if w := p.followWord("function", fpos); p.err == nil {
			rawName := string(p.src[w.Pos()-1 : w.End()-1])
			p.posErr(w.Pos(), "invalid func name: %q", rawName)
		}
	}
	name := ast.Lit{ValuePos: p.pos, Value: p.val}
	p.next()
	if p.gotSameLine(token.LPAREN) {
		p.follow(name.ValuePos, "foo(", token.RPAREN)
	}
	return p.funcDecl(name, fpos)
}

func (p *parser) callExpr(s *ast.Stmt, w ast.Word) *ast.CallExpr {
	alloc := &struct {
		ce ast.CallExpr
		ws [4]ast.Word
	}{}
	ce := &alloc.ce
	ce.Args = alloc.ws[:1]
	ce.Args[0] = w
	for !p.newLine {
		switch p.tok {
		case token.EOF, token.SEMICOLON, token.AND, token.OR,
			token.LAND, token.LOR, token.PIPEALL,
			token.DSEMICOLON, token.SEMIFALL, token.DSEMIFALL:
			return ce
		case token.LITWORD:
			if litRedir(p.src, p.npos) {
				p.doRedirect(s)
				continue
			}
			ce.Args = append(ce.Args, ast.Word{
				Parts: p.singleWps(p.lit(p.pos, p.val)),
			})
			p.next()
		case token.BQUOTE:
			if p.quote == subCmdBckquo {
				return ce
			}
			fallthrough
		case token.LIT, token.DOLLBR, token.DOLLDP, token.DOLLPR,
			token.DOLLAR, token.CMDIN, token.CMDOUT, token.SQUOTE,
			token.DOLLSQ, token.DQUOTE, token.DOLLDQ, token.DOLLBK,
			token.GQUEST, token.GMUL, token.GADD, token.GAT, token.GNOT:
			ce.Args = append(ce.Args, ast.Word{Parts: p.wordParts()})
		case token.GTR, token.SHR, token.LSS, token.DPLIN, token.DPLOUT,
			token.CLBOUT, token.RDRINOUT, token.SHL, token.DHEREDOC,
			token.WHEREDOC, token.RDRALL, token.APPALL:
			p.doRedirect(s)
		case token.RPAREN:
			if p.quote == subCmd {
				return ce
			}
			fallthrough
		default:
			p.curErr("a command can only contain words and redirects")
		}
	}
	return ce
}

func (p *parser) funcDecl(name ast.Lit, pos token.Pos) *ast.FuncDecl {
	fd := &ast.FuncDecl{
		Position:  pos,
		BashStyle: pos != name.ValuePos,
		Name:      name,
	}
	if fd.Body, _ = p.getStmt(false); fd.Body == nil {
		p.followErr(fd.Pos(), "foo()", "a statement")
	}
	return fd
}
