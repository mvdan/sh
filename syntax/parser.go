// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"bytes"
	"fmt"
	"strconv"
	"sync"
)

// ParseMode controls the parser behaviour via a set of flags.
type ParseMode uint

const (
	ParseComments   ParseMode = 1 << iota // add comments to the AST
	PosixConformant                       // match the POSIX standard where it differs from bash
)

var parserFree = sync.Pool{
	New: func() interface{} {
		return &parser{helperBuf: new(bytes.Buffer)}
	},
}

// Parse reads and parses a shell program with an optional name. It
// returns the parsed program if no issues were encountered. Otherwise,
// an error is returned.
func Parse(src []byte, name string, mode ParseMode) (*File, error) {
	p := parserFree.Get().(*parser)
	p.reset()
	alloc := &struct {
		f File
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

	f    *File
	mode ParseMode

	spaced, newLine bool

	err error

	tok Token
	val string

	pos  Pos
	npos int

	quote quoteState
	asPos int

	// list of pending heredoc bodies
	buriedHdocs int
	heredocs    []*Redirect
	hdocStop    []byte

	helperBuf *bytes.Buffer

	litBatch    []Lit
	wpsBatch    []WordPart
	stmtBatch   []Stmt
	stListBatch []*Stmt

	litBuf [32]byte
}

func (p *parser) lit(pos Pos, val string) *Lit {
	if len(p.litBatch) == 0 {
		p.litBatch = make([]Lit, 32)
	}
	l := &p.litBatch[0]
	l.ValuePos = pos
	l.Value = val
	p.litBatch = p.litBatch[1:]
	return l
}

func (p *parser) singleWps(wp WordPart) []WordPart {
	if len(p.wpsBatch) == 0 {
		p.wpsBatch = make([]WordPart, 64)
	}
	wps := p.wpsBatch[:1:1]
	p.wpsBatch = p.wpsBatch[1:]
	wps[0] = wp
	return wps
}

func (p *parser) wps() []WordPart {
	if len(p.wpsBatch) < 4 {
		p.wpsBatch = make([]WordPart, 64)
	}
	wps := p.wpsBatch[:0:4]
	p.wpsBatch = p.wpsBatch[4:]
	return wps
}

func (p *parser) stmt(pos Pos) *Stmt {
	if len(p.stmtBatch) == 0 {
		p.stmtBatch = make([]Stmt, 16)
	}
	s := &p.stmtBatch[0]
	s.Position = pos
	p.stmtBatch = p.stmtBatch[1:]
	return s
}

func (p *parser) stList() []*Stmt {
	if len(p.stListBatch) == 0 {
		p.stListBatch = make([]*Stmt, 128)
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
	p.tok, p.quote = illegalTok, noState
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

func (p *parser) unquotedWordBytes(w Word) ([]byte, bool) {
	p.helperBuf.Reset()
	didUnquote := false
	for _, wp := range w.Parts {
		if p.unquotedWordPart(p.helperBuf, wp) {
			didUnquote = true
		}
	}
	return p.helperBuf.Bytes(), didUnquote
}

func (p *parser) unquotedWordPart(b *bytes.Buffer, wp WordPart) bool {
	switch x := wp.(type) {
	case *Lit:
		if x.Value[0] == '\\' {
			b.WriteString(x.Value[1:])
			return true
		}
		b.WriteString(x.Value)
		return false
	case *SglQuoted:
		b.WriteString(x.Value)
		return true
	case *DblQuoted:
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
	p.tok = illegalTok
	old := p.quote
	hdocs := p.heredocs[p.buriedHdocs:]
	p.heredocs = p.heredocs[:p.buriedHdocs]
	for i, r := range hdocs {
		if r.Op == DashHdoc {
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

func (p *parser) got(tok Token) bool {
	if p.tok == tok {
		p.next()
		return true
	}
	return false
}

func (p *parser) gotRsrv(val string) bool {
	if p.tok == _LitWord && p.val == val {
		p.next()
		return true
	}
	return false
}

func (p *parser) gotSameLine(tok Token) bool {
	if !p.newLine && p.tok == tok {
		p.next()
		return true
	}
	return false
}

func readableStr(s string) string {
	// don't quote tokens like & or }
	if s != "" && s[0] >= 'a' && s[0] <= 'z' {
		return strconv.Quote(s)
	}
	return s
}

func (p *parser) followErr(pos Pos, left, right string) {
	leftStr := readableStr(left)
	p.posErr(pos, "%s must be followed by %s", leftStr, right)
}

func (p *parser) follow(lpos Pos, left string, tok Token) Pos {
	pos := p.pos
	if !p.got(tok) {
		p.followErr(lpos, left, tok.String())
	}
	return pos
}

func (p *parser) followRsrv(lpos Pos, left, val string) Pos {
	pos := p.pos
	if !p.gotRsrv(val) {
		p.followErr(lpos, left, fmt.Sprintf("%q", val))
	}
	return pos
}

func (p *parser) followStmts(left string, lpos Pos, stops ...string) []*Stmt {
	if p.gotSameLine(semicolon) {
		return nil
	}
	sts := p.stmts(stops...)
	if len(sts) < 1 && !p.newLine {
		p.followErr(lpos, left, "a statement list")
	}
	return sts
}

func (p *parser) followWordTok(tok Token, pos Pos) Word {
	w := p.word()
	if w.Parts == nil {
		p.followErr(pos, tok.String(), "a word")
	}
	return w
}

func (p *parser) followWord(s string, pos Pos) Word {
	w := p.word()
	if w.Parts == nil {
		p.followErr(pos, s, "a word")
	}
	return w
}

func (p *parser) stmtEnd(n Node, start, end string) Pos {
	pos := p.pos
	if !p.gotRsrv(end) {
		p.posErr(n.Pos(), "%s statement must end with %q", start, end)
	}
	return pos
}

func (p *parser) quoteErr(lpos Pos, quote Token) {
	p.posErr(lpos, "reached %s without closing quote %s", p.tok, quote)
}

func (p *parser) matchingErr(lpos Pos, left, right interface{}) {
	p.posErr(lpos, "reached %s without matching %s with %s", p.tok, left, right)
}

func (p *parser) matched(lpos Pos, left, right Token) Pos {
	pos := p.pos
	if !p.got(right) {
		p.matchingErr(lpos, left, right)
	}
	return pos
}

func (p *parser) errPass(err error) {
	if p.err == nil {
		p.err = err
		p.tok = _EOF
	}
}

// ParseError represents an error found when parsing a source file.
type ParseError struct {
	Position
	Filename, Text string
}

func (e *ParseError) Error() string {
	prefix := ""
	if e.Filename != "" {
		prefix = e.Filename + ":"
	}
	return fmt.Sprintf("%s%d:%d: %s", prefix, e.Line, e.Column, e.Text)
}

func (p *parser) posErr(pos Pos, format string, a ...interface{}) {
	p.errPass(&ParseError{
		Position: p.f.Position(pos),
		Filename: p.f.Name,
		Text:     fmt.Sprintf(format, a...),
	})
}

func (p *parser) curErr(format string, a ...interface{}) {
	p.posErr(p.pos, format, a...)
}

func (p *parser) stmts(stops ...string) (sts []*Stmt) {
	q := p.quote
	gotEnd := true
	for p.tok != _EOF {
		switch p.tok {
		case _LitWord:
			for _, stop := range stops {
				if p.val == stop {
					return
				}
			}
		case rightParen:
			if q == subCmd {
				return
			}
		case bckQuote:
			if q == subCmdBckquo {
				return
			}
		case dblSemicolon, semiFall, dblSemiFall:
			if q == switchCase {
				return
			}
			p.curErr("%s can only be used in a case clause", p.tok)
		}
		if !p.newLine && !gotEnd {
			p.curErr("statements must be separated by &, ; or a newline")
		}
		if p.tok == _EOF {
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
	case semicolon, And, Or, AndExpr, OrExpr:
		p.curErr("%s can only immediately follow a statement", p.tok)
	case rightParen:
		p.curErr("%s can only be used to close a subshell", p.tok)
	default:
		p.curErr("%s is not a valid start for a statement", p.tok)
	}
}

func (p *parser) word() Word {
	if p.tok == _LitWord {
		w := Word{Parts: p.singleWps(p.lit(p.pos, p.val))}
		p.next()
		return w
	}
	return Word{Parts: p.wordParts()}
}

func (p *parser) gotLit(l *Lit) bool {
	l.ValuePos = p.pos
	if p.tok == _Lit || p.tok == _LitWord {
		l.Value = p.val
		p.next()
		return true
	}
	return false
}

func (p *parser) wordParts() (wps []WordPart) {
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

func (p *parser) wordPart() WordPart {
	switch p.tok {
	case _Lit, _LitWord:
		l := p.lit(p.pos, p.val)
		p.next()
		return l
	case dollBrace:
		return p.paramExp()
	case dollDblParen, dollBrack:
		left := p.tok
		ar := &ArithmExp{Left: p.pos, Bracket: left == dollBrack}
		old := p.preNested(arithmExpr)
		if ar.Bracket {
			p.quote = arithmExprBrack
		} else if !p.couldBeArithm() {
			p.postNested(old)
			p.npos = int(ar.Left) + 1
			p.tok = dollParen
			p.pos = ar.Left
			wp := p.wordPart()
			if p.err != nil {
				p.err = nil
				p.matchingErr(ar.Left, dollDblParen, dblRightParen)
			}
			return wp
		}
		p.next()
		ar.X = p.arithmExpr(left, ar.Left, 0, false)
		if ar.Bracket {
			if p.tok != rightBrack {
				p.matchingErr(ar.Left, dollBrack, rightBrack)
			}
			p.postNested(old)
			ar.Right = p.pos
			p.next()
		} else {
			ar.Right = p.arithmEnd(dollDblParen, ar.Left, old)
		}
		return ar
	case dollParen:
		if p.quote == hdocWord {
			p.curErr("nested statements not allowed in heredoc words")
		}
		cs := &CmdSubst{Left: p.pos}
		old := p.preNested(subCmd)
		p.next()
		cs.Stmts = p.stmts()
		p.postNested(old)
		cs.Right = p.matched(cs.Left, leftParen, rightParen)
		return cs
	case dollar:
		var b byte
		if p.npos >= len(p.src) {
			p.tok = _EOF
		} else {
			b = p.src[p.npos]
		}
		if p.tok == _EOF || wordBreak(b) || b == '"' || b == '\'' || b == '`' || b == '[' {
			l := p.lit(p.pos, "$")
			p.next()
			return l
		}
		pe := &ParamExp{Dollar: p.pos, Short: true}
		p.pos++
		switch b {
		case '@', '*', '#', '$', '?', '!', '0', '-':
			p.npos++
			p.tok, p.val = _Lit, string(b)
		default:
			p.advanceLitOther(p.quote)
		}
		p.gotLit(&pe.Param)
		return pe
	case cmdIn, cmdOut:
		ps := &ProcSubst{Op: ProcOperator(p.tok), OpPos: p.pos}
		old := p.preNested(subCmd)
		p.next()
		ps.Stmts = p.stmts()
		p.postNested(old)
		ps.Rparen = p.matched(ps.OpPos, Token(ps.Op), rightParen)
		return ps
	case sglQuote:
		sq := &SglQuoted{Position: p.pos}
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
			p.posErr(sq.Pos(), "reached EOF without closing quote %s", sglQuote)
		}
		sq.Value = string(bs)
		p.next()
		return sq
	case dollSglQuote:
		sq := &SglQuoted{Position: p.pos, Dollar: true}
		old := p.quote
		p.quote = sglQuotes
		p.next()
		if p.tok == sglQuote {
			p.quote = old
		} else {
			sq.Value = p.val
			p.quote = old
			p.next()
		}
		if !p.got(sglQuote) {
			p.quoteErr(sq.Pos(), sglQuote)
		}
		return sq
	case dblQuote:
		if p.quote == dblQuotes {
			return nil
		}
		fallthrough
	case dollDblQuote:
		q := &DblQuoted{Position: p.pos, Dollar: p.tok == dollDblQuote}
		old := p.quote
		p.quote = dblQuotes
		p.next()
		if p.tok == _LitWord {
			q.Parts = p.singleWps(p.lit(p.pos, p.val))
			p.next()
		} else {
			q.Parts = p.wordParts()
		}
		p.quote = old
		if !p.got(dblQuote) {
			p.quoteErr(q.Pos(), dblQuote)
		}
		return q
	case bckQuote:
		switch p.quote {
		case hdocWord:
			p.curErr("nested statements not allowed in heredoc words")
		case subCmdBckquo:
			return nil
		}
		cs := &CmdSubst{Left: p.pos}
		old := p.preNested(subCmdBckquo)
		p.next()
		cs.Stmts = p.stmts()
		p.postNested(old)
		cs.Right = p.pos
		if !p.got(bckQuote) {
			p.quoteErr(cs.Pos(), bckQuote)
		}
		return cs
	case globQuest, globMul, globAdd, globAt, globNot:
		eg := &ExtGlob{Op: GlobOperator(p.tok)}
		eg.Pattern.ValuePos = Pos(p.npos + 1)
		start := p.npos
		lparens := 0
		for _, b := range p.src[start:] {
			p.npos++
			if b == '(' {
				lparens++
			} else if b == ')' {
				if lparens--; lparens < 0 {
					eg.Pattern.Value = string(p.src[start : p.npos-1])
					break
				}
			}
		}
		p.next()
		if lparens != -1 {
			p.matchingErr(p.pos, eg.Op, rightParen)
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
tokLoop:
	for p.tok != _EOF {
		switch p.tok {
		case leftParen, dollParen:
			lparens++
		case dollDblParen, dblLeftParen:
			lparens += 2
		case rightParen:
			if lparens == 0 {
				could = p.peekArithmEnd()
				break tokLoop
			}
			lparens--
		}
		p.next()
	}
	// recover state
	p.tok = oldTok
	p.npos = oldNpos
	p.f.Lines = p.f.Lines[:oldLines]
	return
}

func arithmOpLevel(tok Token) int {
	switch tok {
	case Comma:
		return 0
	case AddAssgn, SubAssgn, MulAssgn, QuoAssgn, RemAssgn, AndAssgn,
		OrAssgn, XorAssgn, ShlAssgn, ShrAssgn:
		return 1
	case Assgn:
		return 2
	case Quest, Colon:
		return 3
	case AndExpr, OrExpr:
		return 4
	case And, Or, Xor:
		return 5
	case Eql, Neq:
		return 6
	case Lss, Gtr, Leq, Geq:
		return 7
	case Shl, Shr:
		return 8
	case Add, Sub:
		return 9
	case Mul, Quo, Rem:
		return 10
	case Pow:
		return 11
	}
	return -1
}

func (p *parser) arithmExpr(ftok Token, fpos Pos, level int, compact bool) ArithmExpr {
	if p.tok == _EOF || p.peekArithmEnd() {
		return nil
	}
	var left ArithmExpr
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
		case _Lit, _LitWord:
			p.curErr("not a valid arithmetic operator: %s", p.val)
			return nil
		case rightParen, _EOF:
		default:
			if p.quote == arithmExpr {
				p.curErr("not a valid arithmetic operator: %v", p.tok)
				return nil
			}
		}
	}
	if newLevel < 0 || newLevel < level {
		return left
	}
	b := &BinaryArithm{
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

func (p *parser) arithmExprBase(ftok Token, fpos Pos, compact bool) ArithmExpr {
	var x ArithmExpr
	switch p.tok {
	case Inc, Dec, Not:
		pre := &UnaryArithm{OpPos: p.pos, Op: p.tok}
		p.next()
		pre.X = p.arithmExprBase(pre.Op, pre.OpPos, compact)
		return pre
	case leftParen:
		pe := &ParenArithm{Lparen: p.pos}
		p.next()
		if pe.X = p.arithmExpr(leftParen, pe.Lparen, 0, false); pe.X == nil {
			p.posErr(pe.Lparen, "parentheses must enclose an expression")
		}
		pe.Rparen = p.matched(pe.Lparen, leftParen, rightParen)
		x = pe
	case Add, Sub:
		ue := &UnaryArithm{OpPos: p.pos, Op: p.tok}
		if p.next(); compact && p.spaced {
			p.followErr(ue.OpPos, ue.Op.String(), "an expression")
		}
		if ue.X = p.arithmExpr(ue.Op, ue.OpPos, 0, compact); ue.X == nil {
			p.followErr(ue.OpPos, ue.Op.String(), "an expression")
		}
		x = ue
	case bckQuote:
		if p.quote == arithmExprLet {
			return nil
		}
		fallthrough
	default:
		w := p.word()
		if w.Parts == nil {
			p.followErr(fpos, ftok.String(), "an expression")
		}
		x = &w
	}
	if compact && p.spaced {
		return x
	}
	if p.tok == Inc || p.tok == Dec {
		u := &UnaryArithm{
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

func (p *parser) gotParamLit(l *Lit) bool {
	l.ValuePos = p.pos
	switch p.tok {
	case _Lit, _LitWord:
		l.Value = p.val
	case dollar:
		l.Value = "$"
	case Quest:
		l.Value = "?"
	case Hash:
		l.Value = "#"
	case Sub:
		l.Value = "-"
	default:
		return false
	}
	p.next()
	return true
}

func (p *parser) paramExp() *ParamExp {
	pe := &ParamExp{Dollar: p.pos}
	old := p.preNested(paramExpName)
	p.next()
	switch p.tok {
	case dblHash:
		p.tok = Hash
		p.npos--
		fallthrough
	case Hash:
		if p.npos < len(p.src) && p.src[p.npos] != '}' {
			pe.Length = true
			p.next()
		}
	}
	if !p.gotParamLit(&pe.Param) && !pe.Length {
		p.posErr(pe.Dollar, "parameter expansion requires a literal")
	}
	if p.tok == rightBrace {
		pe.Rbrace = p.pos
		p.postNested(old)
		p.next()
		return pe
	}
	if p.tok == leftBrack {
		if !p.bash() {
			p.curErr("arrays are a bash feature")
		}
		lpos := p.pos
		p.quote = paramExpInd
		p.next()
		pe.Ind = &Index{Word: p.word()}
		p.quote = paramExpName
		p.matched(lpos, leftBrack, rightBrack)
	}
	switch p.tok {
	case rightBrace:
		pe.Rbrace = p.pos
		p.postNested(old)
		p.next()
		return pe
	case Quo, dblQuo:
		pe.Repl = &Replace{All: p.tok == dblQuo}
		p.quote = paramExpRepl
		p.next()
		pe.Repl.Orig = p.word()
		if p.tok == Quo {
			p.quote = paramExpExp
			p.next()
			pe.Repl.With = p.word()
		}
	case Colon:
		if !p.bash() {
			p.curErr("slicing is a bash feature")
		}
		pe.Slice = &Slice{}
		colonPos := p.pos
		p.next()
		if p.tok != Colon {
			pe.Slice.Offset = p.followWordTok(Colon, colonPos)
		}
		colonPos = p.pos
		if p.got(Colon) {
			pe.Slice.Length = p.followWordTok(Colon, colonPos)
		}
	case Xor, dblXor, Comma, dblComma:
		if !p.bash() {
			p.curErr("case expansions are a bash feature")
		}
		fallthrough
	default:
		pe.Exp = &Expansion{Op: ParExpOperator(p.tok)}
		p.quote = paramExpExp
		p.next()
		pe.Exp.Word = p.word()
	}
	p.postNested(old)
	pe.Rbrace = p.pos
	p.matched(pe.Dollar, dollBrace, rightBrace)
	return pe
}

func (p *parser) peekArithmEnd() bool {
	return p.tok == rightParen && p.npos < len(p.src) && p.src[p.npos] == ')'
}

func (p *parser) arithmEnd(ltok Token, lpos Pos, old saveState) Pos {
	if p.peekArithmEnd() {
		p.npos++
	} else {
		p.matchingErr(lpos, ltok, dblRightParen)
	}
	p.postNested(old)
	pos := p.pos
	p.next()
	return pos
}

func stopToken(tok Token) bool {
	switch tok {
	case _EOF, semicolon, And, Or, AndExpr, OrExpr, pipeAll, dblSemicolon,
		semiFall, dblSemiFall, rightParen:
		return true
	}
	return false
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

func (p *parser) getAssign() *Assign {
	asPos := p.asPos
	as := &Assign{Name: p.lit(p.pos, p.val[:asPos])}
	if p.val[asPos] == '+' {
		as.Append = true
		asPos++
	}
	start := p.lit(p.pos+1, p.val[asPos+1:])
	if start.Value != "" {
		start.ValuePos += Pos(asPos)
		as.Value.Parts = p.singleWps(start)
	}
	p.next()
	if p.spaced {
		return as
	}
	if start.Value == "" && p.tok == leftParen {
		if !p.bash() {
			p.curErr("arrays are a bash feature")
		}
		ae := &ArrayExpr{Lparen: p.pos}
		p.next()
		for p.tok != _EOF && p.tok != rightParen {
			if w := p.word(); w.Parts == nil {
				p.curErr("array elements must be words")
			} else {
				ae.List = append(ae.List, w)
			}
		}
		ae.Rparen = p.matched(ae.Lparen, leftParen, rightParen)
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
	case _LitWord:
		return litRedir(p.src, p.npos)
	case Gtr, Shr, Lss, dplIn, dplOut, clbOut, rdrInOut, Shl, dashHdoc,
		wordHdoc, rdrAll, appAll:
		return true
	}
	return false
}

func (p *parser) doRedirect(s *Stmt) {
	r := &Redirect{}
	var l Lit
	if p.gotLit(&l) {
		r.N = &l
	}
	r.Op, r.OpPos = RedirOperator(p.tok), p.pos
	p.next()
	switch r.Op {
	case Hdoc, DashHdoc:
		old := p.quote
		p.quote = hdocWord
		if p.newLine {
			p.curErr("heredoc stop word must be on the same line")
		}
		p.heredocs = append(p.heredocs, r)
		r.Word = p.followWordTok(Token(r.Op), r.OpPos)
		p.quote = old
		p.next()
	default:
		if p.newLine {
			p.curErr("redirect word must be on the same line")
		}
		r.Word = p.followWordTok(Token(r.Op), r.OpPos)
	}
	s.Redirs = append(s.Redirs, r)
}

func (p *parser) getStmt(readEnd bool) (s *Stmt, gotEnd bool) {
	s = p.stmt(p.pos)
	if p.gotRsrv("!") {
		s.Negated = true
	}
preLoop:
	for {
		switch p.tok {
		case _Lit, _LitWord:
			if p.validIdent() {
				s.Assigns = append(s.Assigns, p.getAssign())
			} else if litRedir(p.src, p.npos) {
				p.doRedirect(s)
			} else {
				break preLoop
			}
		case Gtr, Shr, Lss, dplIn, dplOut, clbOut, rdrInOut, Shl, dashHdoc,
			wordHdoc, rdrAll, appAll:
			p.doRedirect(s)
		default:
			break preLoop
		}
		switch {
		case p.newLine, p.tok == _EOF:
			return
		case p.tok == semicolon:
			if readEnd {
				s.SemiPos = p.pos
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
	case AndExpr, OrExpr:
		b := &BinaryCmd{OpPos: p.pos, Op: BinCmdOperator(p.tok), X: s}
		p.next()
		if b.Y, _ = p.getStmt(false); b.Y == nil {
			p.followErr(b.OpPos, b.Op.String(), "a statement")
		}
		s = p.stmt(s.Position)
		s.Cmd = b
		if readEnd && p.gotSameLine(semicolon) {
			gotEnd = true
		}
	case And:
		p.next()
		s.Background = true
		gotEnd = true
	case semicolon:
		if !p.newLine && readEnd {
			s.SemiPos = p.pos
			p.next()
			gotEnd = true
		}
	}
	return
}

func bashDeclareWord(s string) bool {
	switch s {
	case "declare", "local", "export", "readonly", "typeset", "nameref":
		return true
	}
	return false
}

func (p *parser) gotStmtPipe(s *Stmt) *Stmt {
	switch p.tok {
	case leftParen:
		s.Cmd = p.subshell()
	case dblLeftParen:
		s.Cmd = p.arithmExpCmd()
	case _LitWord:
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
			name := Lit{ValuePos: p.pos, Value: p.val}
			p.next()
			if p.gotSameLine(leftParen) {
				p.follow(name.ValuePos, "foo(", rightParen)
				s.Cmd = p.funcDecl(name, name.ValuePos)
			} else {
				s.Cmd = p.callExpr(s, Word{
					Parts: p.singleWps(&name),
				})
			}
		}
	case _Lit, dollBrace, dollDblParen, dollParen, dollar, cmdIn, cmdOut, sglQuote,
		dollSglQuote, dblQuote, dollDblQuote, bckQuote, dollBrack, globQuest, globMul, globAdd,
		globAt, globNot:
		w := Word{Parts: p.wordParts()}
		if p.gotSameLine(leftParen) && p.err == nil {
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
	if p.tok == Or || p.tok == pipeAll {
		b := &BinaryCmd{OpPos: p.pos, Op: BinCmdOperator(p.tok), X: s}
		p.next()
		if b.Y = p.gotStmtPipe(p.stmt(p.pos)); b.Y == nil {
			p.followErr(b.OpPos, b.Op.String(), "a statement")
		}
		s = p.stmt(s.Position)
		s.Cmd = b
	}
	return s
}

func (p *parser) subshell() *Subshell {
	s := &Subshell{Lparen: p.pos}
	old := p.preNested(subCmd)
	p.next()
	s.Stmts = p.stmts()
	p.postNested(old)
	s.Rparen = p.matched(s.Lparen, leftParen, rightParen)
	return s
}

func (p *parser) arithmExpCmd() Command {
	ar := &ArithmCmd{Left: p.pos}
	old := p.preNested(arithmExprCmd)
	if !p.couldBeArithm() {
		p.postNested(old)
		p.npos = int(ar.Left)
		p.tok = leftParen
		p.pos = ar.Left
		s := p.subshell()
		if p.err != nil {
			p.err = nil
			p.matchingErr(ar.Left, dblLeftParen, dblRightParen)
		}
		return s
	}
	p.next()
	ar.X = p.arithmExpr(dblLeftParen, ar.Left, 0, false)
	ar.Right = p.arithmEnd(dblLeftParen, ar.Left, old)
	return ar
}

func (p *parser) block() *Block {
	b := &Block{Lbrace: p.pos}
	p.next()
	b.Stmts = p.stmts("}")
	b.Rbrace = p.pos
	if !p.gotRsrv("}") {
		p.matchingErr(b.Lbrace, "{", "}")
	}
	return b
}

func (p *parser) ifClause() *IfClause {
	ic := &IfClause{If: p.pos}
	p.next()
	ic.CondStmts = p.followStmts("if", ic.If, "then")
	ic.Then = p.followRsrv(ic.If, "if <cond>", "then")
	ic.ThenStmts = p.followStmts("then", ic.Then, "fi", "elif", "else")
	elifPos := p.pos
	for p.gotRsrv("elif") {
		elf := &Elif{Elif: elifPos}
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

func (p *parser) whileClause() *WhileClause {
	wc := &WhileClause{While: p.pos}
	p.next()
	wc.CondStmts = p.followStmts("while", wc.While, "do")
	wc.Do = p.followRsrv(wc.While, "while <cond>", "do")
	wc.DoStmts = p.followStmts("do", wc.Do, "done")
	wc.Done = p.stmtEnd(wc, "while", "done")
	return wc
}

func (p *parser) untilClause() *UntilClause {
	uc := &UntilClause{Until: p.pos}
	p.next()
	uc.CondStmts = p.followStmts("until", uc.Until, "do")
	uc.Do = p.followRsrv(uc.Until, "until <cond>", "do")
	uc.DoStmts = p.followStmts("do", uc.Do, "done")
	uc.Done = p.stmtEnd(uc, "until", "done")
	return uc
}

func (p *parser) forClause() *ForClause {
	fc := &ForClause{For: p.pos}
	p.next()
	fc.Loop = p.loop(fc.For)
	fc.Do = p.followRsrv(fc.For, "for foo [in words]", "do")
	fc.DoStmts = p.followStmts("do", fc.Do, "done")
	fc.Done = p.stmtEnd(fc, "for", "done")
	return fc
}

func (p *parser) loop(forPos Pos) Loop {
	if p.tok == dblLeftParen {
		cl := &CStyleLoop{Lparen: p.pos}
		old := p.preNested(arithmExprCmd)
		p.next()
		if p.tok == dblSemicolon {
			p.npos--
			p.tok = semicolon
		}
		if p.tok != semicolon {
			cl.Init = p.arithmExpr(dblLeftParen, cl.Lparen, 0, false)
		}
		scPos := p.pos
		p.follow(p.pos, "expression", semicolon)
		if p.tok != semicolon {
			cl.Cond = p.arithmExpr(semicolon, scPos, 0, false)
		}
		scPos = p.pos
		p.follow(p.pos, "expression", semicolon)
		if p.tok != semicolon {
			cl.Post = p.arithmExpr(semicolon, scPos, 0, false)
		}
		cl.Rparen = p.arithmEnd(dblLeftParen, cl.Lparen, old)
		p.gotSameLine(semicolon)
		return cl
	}
	wi := &WordIter{}
	if !p.gotLit(&wi.Name) {
		p.followErr(forPos, "for", "a literal")
	}
	if p.gotRsrv("in") {
		for !p.newLine && p.tok != _EOF && p.tok != semicolon {
			if w := p.word(); w.Parts == nil {
				p.curErr("word list can only contain words")
			} else {
				wi.List = append(wi.List, w)
			}
		}
		p.gotSameLine(semicolon)
	} else if !p.newLine && !p.got(semicolon) {
		p.followErr(forPos, "for foo", `"in", ; or a newline`)
	}
	return wi
}

func (p *parser) caseClause() *CaseClause {
	cc := &CaseClause{Case: p.pos}
	p.next()
	cc.Word = p.followWord("case", cc.Case)
	p.followRsrv(cc.Case, "case x", "in")
	cc.List = p.patLists()
	cc.Esac = p.stmtEnd(cc, "case", "esac")
	return cc
}

func (p *parser) patLists() (pls []*PatternList) {
	for p.tok != _EOF && !(p.tok == _LitWord && p.val == "esac") {
		pl := &PatternList{}
		p.got(leftParen)
		for p.tok != _EOF {
			if w := p.word(); w.Parts == nil {
				p.curErr("case patterns must consist of words")
			} else {
				pl.Patterns = append(pl.Patterns, w)
			}
			if p.tok == rightParen {
				break
			}
			if !p.got(Or) {
				p.curErr("case patterns must be separated with |")
			}
		}
		old := p.preNested(switchCase)
		p.next()
		pl.Stmts = p.stmts("esac")
		p.postNested(old)
		pl.OpPos = p.pos
		if p.tok != dblSemicolon && p.tok != semiFall && p.tok != dblSemiFall {
			pl.Op = DblSemicolon
			pls = append(pls, pl)
			break
		}
		pl.Op = CaseOperator(p.tok)
		p.next()
		pls = append(pls, pl)
	}
	return
}

func (p *parser) testClause() *TestClause {
	tc := &TestClause{Left: p.pos}
	p.next()
	if p.tok == _EOF || p.gotRsrv("]]") {
		p.posErr(tc.Left, "test clause requires at least one expression")
	}
	tc.X = p.testExpr(illegalTok, tc.Left, 0)
	tc.Right = p.pos
	if !p.gotRsrv("]]") {
		p.matchingErr(tc.Left, "[[", "]]")
	}
	return tc
}

func (p *parser) testExpr(ftok Token, fpos Pos, level int) TestExpr {
	var left TestExpr
	if level > 1 {
		left = p.testExprBase(ftok, fpos)
	} else {
		left = p.testExpr(ftok, fpos, level+1)
	}
	if left == nil {
		return left
	}
	var newLevel int
	switch p.tok {
	case AndExpr, OrExpr:
	case _LitWord:
		if p.val == "]]" {
			return left
		}
		fallthrough
	case Lss, Gtr:
		newLevel = 1
	case _EOF, rightParen:
		return left
	default:
		p.curErr("not a valid test operator: %v", p.tok)
	}
	if newLevel < level {
		return left
	}
	if p.tok == _LitWord {
		if p.tok = testBinaryOp(p.val); p.tok == illegalTok {
			p.curErr("not a valid test operator: %s", p.val)
		}
	}
	b := &BinaryTest{
		OpPos: p.pos,
		Op:    BinTestOperator(p.tok),
		X:     left,
	}
	if b.Op == TsReMatch {
		old := p.preNested(testRegexp)
		p.next()
		p.postNested(old)
	} else {
		p.next()
	}
	if b.Y = p.testExpr(Token(b.Op), b.OpPos, newLevel); b.Y == nil {
		p.followErr(b.OpPos, b.Op.String(), "an expression")
	}
	return b
}

func (p *parser) testExprBase(ftok Token, fpos Pos) TestExpr {
	switch p.tok {
	case _EOF:
		return nil
	case _LitWord:
		if op := testUnaryOp(p.val); op != illegalTok {
			p.tok = op
		}
	}
	switch p.tok {
	case Not:
		u := &UnaryTest{OpPos: p.pos, Op: TsNot}
		p.next()
		u.X = p.testExpr(Token(u.Op), u.OpPos, 0)
		return u
	case tsExists, tsRegFile, tsDirect, tsCharSp, tsBlckSp, tsNmPipe, tsSocket, tsSmbLink,
		tsGIDSet, tsUIDSet, tsRead, tsWrite, tsExec, tsNoEmpty, tsFdTerm, tsEmpStr,
		tsNempStr, tsOptSet, tsVarSet, tsRefVar:
		u := &UnaryTest{OpPos: p.pos, Op: UnTestOperator(p.tok)}
		p.next()
		w := p.followWordTok(ftok, fpos)
		u.X = &w
		return u
	case leftParen:
		pe := &ParenTest{Lparen: p.pos}
		p.next()
		if pe.X = p.testExpr(leftParen, pe.Lparen, 0); pe.X == nil {
			p.posErr(pe.Lparen, "parentheses must enclose an expression")
		}
		pe.Rparen = p.matched(pe.Lparen, leftParen, rightParen)
		return pe
	case rightParen:
	default:
		w := p.followWordTok(ftok, fpos)
		return &w
	}
	return nil
}

func (p *parser) declClause() *DeclClause {
	name := p.val
	ds := &DeclClause{Position: p.pos}
	switch name {
	case "declare", "typeset": // typeset is an obsolete synonym
	default:
		ds.Variant = name
	}
	p.next()
	for p.tok == _LitWord && p.val[0] == '-' {
		ds.Opts = append(ds.Opts, p.word())
	}
	for !p.newLine && !stopToken(p.tok) && !p.peekRedir() {
		if (p.tok == _Lit || p.tok == _LitWord) && p.validIdent() {
			ds.Assigns = append(ds.Assigns, p.getAssign())
		} else if w := p.word(); w.Parts == nil {
			p.followErr(p.pos, name, "words")
		} else {
			ds.Assigns = append(ds.Assigns, &Assign{Value: w})
		}
	}
	return ds
}

func (p *parser) evalClause() *EvalClause {
	ec := &EvalClause{Eval: p.pos}
	p.next()
	ec.Stmt, _ = p.getStmt(false)
	return ec
}

func isBashCompoundCommand(tok Token, val string) bool {
	switch tok {
	case leftParen, dblLeftParen:
		return true
	case _LitWord:
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

func (p *parser) coprocClause() *CoprocClause {
	cc := &CoprocClause{Coproc: p.pos}
	p.next()
	if isBashCompoundCommand(p.tok, p.val) {
		// has no name
		cc.Stmt, _ = p.getStmt(false)
		return cc
	}
	if p.newLine {
		p.posErr(cc.Coproc, "coproc clause requires a command")
	}
	var l Lit
	if p.gotLit(&l) {
		cc.Name = &l
	}
	cc.Stmt, _ = p.getStmt(false)
	if cc.Stmt == nil {
		if cc.Name == nil {
			p.posErr(cc.Coproc, "coproc clause requires a command")
			return nil
		}
		// name was in fact the stmt
		cc.Stmt = &Stmt{
			Position: cc.Name.ValuePos,
			Cmd: &CallExpr{Args: []Word{
				{Parts: p.singleWps(cc.Name)},
			}},
		}
		cc.Name = nil
	} else if cc.Name != nil {
		if call, ok := cc.Stmt.Cmd.(*CallExpr); ok {
			// name was in fact the start of a call
			call.Args = append([]Word{{Parts: p.singleWps(cc.Name)}},
				call.Args...)
			cc.Name = nil
		}
	}
	return cc
}

func (p *parser) letClause() *LetClause {
	lc := &LetClause{Let: p.pos}
	old := p.preNested(arithmExprLet)
	p.next()
	for !p.newLine && !stopToken(p.tok) && !p.peekRedir() {
		x := p.arithmExpr(illegalTok, lc.Let, 0, true)
		if x == nil {
			break
		}
		lc.Exprs = append(lc.Exprs, x)
	}
	if len(lc.Exprs) == 0 {
		p.posErr(lc.Let, "let clause requires at least one expression")
	}
	p.postNested(old)
	if p.tok == illegalTok {
		p.next()
	}
	return lc
}

func (p *parser) bashFuncDecl() *FuncDecl {
	fpos := p.pos
	p.next()
	if p.tok != _LitWord {
		if w := p.followWord("function", fpos); p.err == nil {
			rawName := string(p.src[w.Pos()-1 : w.End()-1])
			p.posErr(w.Pos(), "invalid func name: %q", rawName)
		}
	}
	name := Lit{ValuePos: p.pos, Value: p.val}
	p.next()
	if p.gotSameLine(leftParen) {
		p.follow(name.ValuePos, "foo(", rightParen)
	}
	return p.funcDecl(name, fpos)
}

func (p *parser) callExpr(s *Stmt, w Word) *CallExpr {
	alloc := &struct {
		ce CallExpr
		ws [4]Word
	}{}
	ce := &alloc.ce
	ce.Args = alloc.ws[:1]
	ce.Args[0] = w
	for !p.newLine {
		switch p.tok {
		case _EOF, semicolon, And, Or, AndExpr, OrExpr, pipeAll,
			dblSemicolon, semiFall, dblSemiFall:
			return ce
		case _LitWord:
			if litRedir(p.src, p.npos) {
				p.doRedirect(s)
				continue
			}
			ce.Args = append(ce.Args, Word{
				Parts: p.singleWps(p.lit(p.pos, p.val)),
			})
			p.next()
		case bckQuote:
			if p.quote == subCmdBckquo {
				return ce
			}
			fallthrough
		case _Lit, dollBrace, dollDblParen, dollParen, dollar, cmdIn, cmdOut,
			sglQuote, dollSglQuote, dblQuote, dollDblQuote, dollBrack, globQuest,
			globMul, globAdd, globAt, globNot:
			ce.Args = append(ce.Args, Word{Parts: p.wordParts()})
		case Gtr, Shr, Lss, dplIn, dplOut, clbOut, rdrInOut, Shl,
			dashHdoc, wordHdoc, rdrAll, appAll:
			p.doRedirect(s)
		case rightParen:
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

func (p *parser) funcDecl(name Lit, pos Pos) *FuncDecl {
	fd := &FuncDecl{
		Position:  pos,
		BashStyle: pos != name.ValuePos,
		Name:      name,
	}
	if fd.Body, _ = p.getStmt(false); fd.Body == nil {
		p.followErr(fd.Pos(), "foo()", "a statement")
	}
	return fd
}
