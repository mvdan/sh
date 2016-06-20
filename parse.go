// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// Mode controls the parser behaviour via a set of flags.
type Mode uint

const (
	ParseComments Mode = 1 << iota // add comments to the AST
)

// Parse reads and parses a shell program with an optional name. It
// returns the parsed program if no issues were encountered. Otherwise,
// an error is returned.
func Parse(r io.Reader, name string, mode Mode) (*File, error) {
	p := &parser{
		br:   bufio.NewReader(r),
		file: &File{Name: name},
		mode: mode,
		npos: Pos{Line: 1, Column: 1},
	}
	p.next()
	p.file.Stmts = p.stmts()
	return p.file, p.err
}

type parser struct {
	br *bufio.Reader

	file *File
	err  error
	mode Mode

	spaced, newLine bool
	willBreakWord   bool

	nextErr   error
	remaining []byte

	ltok, tok Token
	lval, val string

	lpos, pos, npos Pos

	// stack of stop tokens
	stops []Token

	// stack of stmts (to save redirects)
	stmtStack []*Stmt
	// list of pending heredoc bodies
	heredocs []Redirect

	stopNewline  bool
	forbidNested bool
}

func (p *parser) pushStops(stops ...Token) {
	p.stops = append(p.stops, stops...)
	p.next()
}

func (p *parser) quote() Token {
	if len(p.stops) == 0 {
		return ILLEGAL
	}
	return p.stops[len(p.stops)-1]
}

func (p *parser) popStops(n int) { p.stops = p.stops[:len(p.stops)-n] }
func (p *parser) popStop()       { p.popStops(1) }

func (p *parser) reachingEOF() bool {
	return p.nextErr != nil && len(p.remaining) == 0
}

func (p *parser) readByte() byte {
	if p.reachingEOF() {
		p.errPass(p.nextErr)
		return 0
	}
	b, _ := p.br.ReadByte()
	if len(p.remaining) > 0 {
		p.remaining = p.remaining[:len(p.remaining)-1]
	}
	p.npos = moveWith(p.npos, b)
	return b
}

func moveWith(pos Pos, b byte) Pos {
	if pos.Line == 0 {
		return pos
	}
	if b == '\n' {
		pos.Line++
		pos.Column = 1
	} else {
		pos.Column++
	}
	return pos
}

func moveWithBytes(pos Pos, bs []byte) Pos {
	for _, b := range bs {
		if b == '\n' {
			pos.Line++
			pos.Column = 1
		} else {
			pos.Column++
		}
	}
	return pos
}

func (p *parser) peekByte() byte {
	if p.reachingEOF() {
		p.errPass(p.nextErr)
		return 0
	}
	bs, err := p.br.Peek(1)
	if err != nil {
		p.nextErr = err // TODO: remove
		p.errPass(err)
		return 0
	}
	return bs[0]
}

func (p *parser) willRead(b byte) bool {
	if p.reachingEOF() {
		return false
	}
	bs, err := p.br.Peek(1)
	if err != nil {
		p.nextErr = err
		return false
	}
	return bs[0] == b
}

func (p *parser) willReadStr(s string) bool {
	if p.nextErr != nil && len(p.remaining) < len(s) {
		return false
	}
	bs, err := p.br.Peek(len(s))
	if err != nil {
		p.nextErr = err
		p.remaining = bs
		return false
	}
	return string(bs) == s
}

func (p *parser) readOnlyStr(s string) bool {
	if p.willReadStr(s) {
		for i := 0; i < len(s); i++ {
			p.readByte()
		}
		return true
	}
	return false
}
func (p *parser) readOnlyTok(tok Token) bool {
	return p.readOnlyStr(tok.String())
}

func (p *parser) readOnly(b byte) bool {
	if p.willRead(b) {
		p.readByte()
		return true
	}
	return false
}

// bytes that form or start a token
func regOps(b byte) bool {
	return b == ';' || b == '"' || b == '\'' || b == '(' ||
		b == ')' || b == '$' || b == '|' || b == '&' ||
		b == '>' || b == '<' || b == '`'
}

// tokenize these inside parameter expansions
func paramOps(b byte) bool {
	return b == '}' || b == '#' || b == ':' || b == '-' ||
		b == '+' || b == '=' || b == '?' || b == '%' ||
		b == '[' || b == '/'
}

// tokenize these inside arithmetic expansions
func arithmOps(b byte) bool {
	return b == '+' || b == '-' || b == '!' || b == '*' ||
		b == '/' || b == '%' || b == '(' || b == ')' ||
		b == '^' || b == '<' || b == '>' || b == ':' ||
		b == '=' || b == ',' || b == '?' || b == '|' ||
		b == '&'
}

// space returns whether a byte acts as a space
func space(b byte) bool { return b == ' ' || b == '\t' || b == '\n' }

func (p *parser) next() {
	if p.tok == EOF {
		return
	}
	p.spaced, p.newLine = false, false
	var b byte
	q := p.quote()
	if q == DQUOTE || q == SQUOTE || q == RBRACE || q == QUO {
		if b = p.peekByte(); p.tok == EOF {
			p.lpos, p.pos = p.pos, p.npos
			return
		}
		p.advance(b, q)
		return
	}
	for {
		if b = p.peekByte(); p.tok == EOF {
			p.lpos, p.pos = p.pos, p.npos
			return
		}
		if b == '\\' && p.readOnlyStr("\\\n") {
			continue
		}
		if !space(b) {
			break
		}
		if p.stopNewline && b == '\n' {
			p.stopNewline = false
			p.advanceTok(STOPPED)
			return
		}
		p.readByte()
		p.spaced = true
		if b == '\n' {
			p.newLine = true
			p.doHeredocs()
		}
	}
	p.advance(b, q)
}

func (p *parser) advance(b byte, q Token) {
	p.lpos, p.pos = p.pos, p.npos
	switch {
	case (q == RBRACE || q == LBRACE || q == QUO) && p.readOnlyTok(RBRACE):
		p.advanceTok(RBRACE)
	case q == QUO && p.readOnlyTok(QUO):
		p.advanceTok(QUO)
	case q == RBRACK && p.readOnlyTok(RBRACK):
		p.advanceTok(RBRACK)
	case q == SQUOTE && p.readOnlyTok(SQUOTE):
		p.advanceTok(SQUOTE)
	case q == SQUOTE:
		p.advanceReadLit()
	case q == DQUOTE, q == RBRACE, q == QUO:
		if b == '`' || b == '"' || b == '$' {
			p.advanceTok(p.doRegToken())
		} else {
			p.advanceReadLit()
		}
	case b == '#' && q != LBRACE:
		line, _ := p.readUntil('\n')
		p.advanceBoth(COMMENT, line[1:])
	case q == LBRACE && paramOps(b):
		p.advanceTok(p.doParamToken())
	case (q == DLPAREN || q == DRPAREN || q == LPAREN) && arithmOps(b):
		p.advanceTok(p.doArithmToken())
	case regOps(b):
		p.advanceTok(p.doRegToken())
	default:
		p.advanceReadLit()
	}
}

func (p *parser) advanceReadLit() { p.advanceBoth(LIT, string(p.readLitBytes())) }
func (p *parser) readLitBytes() (bs []byte) {
	p.willBreakWord = false
	q := p.quote()
	for {
		b := p.peekByte()
		if p.tok == EOF {
			p.willBreakWord = true
			return
		}
		if b == '\\' { // escaped byte follows
			p.readByte()
			b := p.readByte()
			if p.tok == EOF {
				bs = append(bs, '\\')
				return
			}
			if q == DQUOTE || b != '\n' {
				bs = append(bs, '\\', b)
			}
			continue
		}
		switch {
		case q == SQUOTE:
			if b == '\'' {
				return
			}
		case b == '`':
			p.willBreakWord = true
			return
		case q == DQUOTE:
			if b == '"' || (b == '$' && !p.willReadStr(`$"`)) {
				return
			}
		case b == '$':
			return
		case q == RBRACE:
			if b == '}' || b == '"' {
				return
			}
		case q == LBRACE && paramOps(b), q == RBRACK && b == ']':
			return
		case q == QUO:
			if b == '/' || b == '}' {
				return
			}
		case space(b), wordBreak(b):
			p.willBreakWord = true
			return
		case regOps(b):
			return
		case (q == DLPAREN || q == DRPAREN || q == LPAREN) && arithmOps(b):
			return
		}
		p.readByte()
		bs = append(bs, b)
	}
}

func (p *parser) advanceTok(tok Token) { p.advanceBoth(tok, "") }
func (p *parser) advanceBoth(tok Token, val string) {
	if p.tok != EOF {
		p.ltok, p.lval = p.tok, p.val
	}
	p.tok, p.val = tok, val
}

func (p *parser) readUntil(b byte) (string, bool) {
	bs, err := p.br.ReadBytes(b)
	if err != nil {
		p.nextErr = err
		p.npos = moveWithBytes(p.npos, bs)
		return string(bs), false
	}
	bs = bs[:len(bs)-1]
	p.npos = moveWithBytes(p.npos, bs)
	p.br.UnreadByte()
	return string(bs), true
}

func (p *parser) readIncluding(b byte) (string, bool) {
	bs, err := p.br.ReadBytes(b)
	if err != nil {
		p.nextErr = err
		p.npos = moveWithBytes(p.npos, bs)
		return string(bs), false
	}
	p.npos = moveWithBytes(p.npos, bs)
	bs = bs[:len(bs)-1]
	return string(bs), true
}

func (p *parser) doHeredocs() {
	for i, r := range p.heredocs {
		end := unquotedWordStr(&r.Word)
		if i > 0 {
			p.readOnly('\n')
		}
		r.Hdoc.ValuePos = p.npos
		r.Hdoc.Value, _ = p.readHdocBody(end, r.Op == DHEREDOC)
	}
	p.heredocs = nil
}

func (p *parser) readHdocBody(end string, noTabs bool) (string, bool) {
	var buf bytes.Buffer
	for p.tok != EOF && !p.reachingEOF() {
		line, _ := p.readIncluding('\n')
		if line == end || (noTabs && strings.TrimLeft(line, "\t") == end) {
			// add trailing tabs
			fmt.Fprint(&buf, line[:len(line)-len(end)])
			return buf.String(), true
		}
		fmt.Fprintln(&buf, line)
	}
	return buf.String(), false
}

func (p *parser) saveComments() {
	for p.tok == COMMENT {
		if p.mode&ParseComments > 0 {
			p.file.Comments = append(p.file.Comments, Comment{
				Hash: p.pos,
				Text: p.val,
			})
		}
		p.next()
	}
}

func (p *parser) eof() bool {
	p.saveComments()
	return p.tok == EOF
}

func (p *parser) peek(tok Token) bool { return p.tok == tok }
func (p *parser) peekRsrv(tok Token) bool {
	return p.tok == LIT && p.val == tok.String() && p.willBreakWord
}
func (p *parser) peekEither(tok Token) bool {
	return p.peek(tok) || p.peekRsrv(tok)
}

func wordBreak(b byte) bool {
	return b == '&' || b == '>' || b == '<' || b == '|' ||
		b == ';' || b == '(' || b == ')' || b == '`'
}

func (p *parser) willSpaced() bool {
	if p.reachingEOF() {
		return true
	}
	bs, err := p.br.Peek(1)
	return err != nil || space(bs[0]) || wordBreak(bs[0])
}

func (p *parser) peekEitherAny(toks ...Token) bool {
	for _, tok := range toks {
		if p.peekEither(tok) {
			return true
		}
	}
	return false
}

func (p *parser) got(tok Token) bool {
	p.saveComments()
	if p.peek(tok) {
		p.next()
		return true
	}
	return false
}
func (p *parser) gotRsrv(tok Token) bool {
	p.saveComments()
	if p.peekRsrv(tok) {
		p.next()
		return true
	}
	return false
}
func (p *parser) gotSameLine(tok Token) bool {
	return !p.newLine && p.got(tok)
}

func readableStr(v interface{}) string {
	var s string
	switch x := v.(type) {
	case string:
		s = x
	case Token:
		s = x.String()
	}
	// don't quote tokens like & or }
	if s[0] >= 'a' && s[0] <= 'z' {
		return strconv.Quote(s)
	}
	return s
}

func (p *parser) followErr(pos Pos, left interface{}, right string) {
	leftStr := readableStr(left)
	p.posErr(pos, "%s must be followed by %s", leftStr, right)
}

func (p *parser) followFull(lpos Pos, left string, tok Token) Pos {
	if !p.got(tok) {
		p.followErr(lpos, left, fmt.Sprintf(`%q`, tok))
	}
	return p.lpos
}
func (p *parser) followRsrv(lpos Pos, left string, tok Token) Pos {
	if !p.gotRsrv(tok) {
		p.followErr(lpos, left, fmt.Sprintf(`%q`, tok))
	}
	return p.lpos
}

func (p *parser) followStmts(left Token, stops ...Token) []Stmt {
	if p.gotSameLine(SEMICOLON) {
		return nil
	}
	sts := p.stmts(stops...)
	if len(sts) < 1 && !p.newLine {
		p.followErr(p.lpos, left, "a statement list")
	}
	return sts
}

func (p *parser) followWord(left Token) Word {
	w, ok := p.gotWord()
	if !ok {
		p.followErr(p.lpos, left, "a word")
	}
	return w
}

func (p *parser) stmtEnd(n Node, startTok, tok Token) Pos {
	if !p.gotRsrv(tok) {
		p.posErr(n.Pos(), `%s statement must end with %q`, startTok, tok)
	}
	return p.lpos
}

func (p *parser) closingQuote(n Node, tok Token) {
	if !p.got(tok) {
		p.quoteErr(n.Pos(), tok)
	}
}

func (p *parser) quoteErr(lpos Pos, quote Token) {
	p.posErr(lpos, `reached %s without closing quote %s`, p.tok, quote)
}

func (p *parser) matchingErr(lpos Pos, left, right Token) {
	p.posErr(lpos, `reached %s without matching token %s with %s`,
		p.tok, left, right)
}

func (p *parser) matchedFull(lpos Pos, left, right Token) Pos {
	if !p.got(right) {
		p.matchingErr(lpos, left, right)
	}
	return p.lpos
}

func (p *parser) errPass(err error) {
	if p.err == nil {
		if err != io.EOF {
			p.err = err
		}
		p.advanceTok(EOF)
	}
}

type ParseError struct {
	Pos
	Filename string
	Text     string
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
		Pos:      pos,
		Filename: p.file.Name,
		Text:     fmt.Sprintf(format, a...),
	})
}

func (p *parser) curErr(format string, a ...interface{}) {
	p.posErr(p.pos, format, a...)
}

func (p *parser) stmts(stops ...Token) (sts []Stmt) {
	for !p.eof() {
		p.got(STOPPED)
		if p.peekEitherAny(stops...) {
			break
		}
		gotEnd := p.newLine || p.ltok == AND || p.ltok == SEMICOLON
		if len(sts) > 0 && !gotEnd {
			p.curErr("statements must be separated by &, ; or a newline")
		}
		if p.eof() {
			break
		}
		if s, ok := p.getStmt(stops...); !ok {
			p.invalidStmtStart()
		} else {
			sts = append(sts, s)
		}
	}
	return
}

func (p *parser) invalidStmtStart() {
	switch {
	case p.peekEitherAny(SEMICOLON, AND, OR, LAND, LOR):
		p.curErr("%s can only immediately follow a statement", p.tok)
	case p.peekRsrv(RBRACE):
		p.curErr("%s can only be used to close a block", p.val)
	case p.peek(RPAREN):
		p.curErr("%s can only be used to close a subshell", p.tok)
	default:
		p.curErr("%s is not a valid start for a statement", p.tok)
	}
}

func (p *parser) stmtsNested(stops ...Token) []Stmt {
	if p.forbidNested {
		p.curErr("nested statements not allowed in this word")
	}
	p.pushStops(stops...)
	sts := p.stmts(stops...)
	p.popStops(len(stops))
	return sts
}

func (p *parser) getWord() (w Word) {
	p.readParts(&w.Parts)
	return
}

func (p *parser) gotWord() (Word, bool) {
	var w Word
	p.readParts(&w.Parts)
	return w, len(w.Parts) > 0
}

func (p *parser) gotLit(l *Lit) bool {
	l.ValuePos = p.pos
	if p.got(LIT) {
		l.Value = p.lval
		return true
	}
	return false
}

func (p *parser) readParts(ns *[]WordPart) {
	for {
		n := p.wordPart()
		if n == nil {
			break
		}
		*ns = append(*ns, n)
		if p.spaced {
			break
		}
	}
}

func (p *parser) wordPart() WordPart {
	q := p.quote()
	switch {
	case p.got(LIT):
		return &Lit{ValuePos: p.lpos, Value: p.lval}
	case p.peek(DOLLBR):
		return p.paramExp()
	case p.peek(DOLLDP):
		p.pushStops(DRPAREN)
		ar := &ArithmExp{Dollar: p.lpos}
		ar.X = p.arithmExpr(DLPAREN)
		ar.Rparen = p.arithmEnd(ar.Dollar)
		return ar
	case p.peek(DOLLPR):
		cs := &CmdSubst{Left: p.pos}
		cs.Stmts = p.stmtsNested(RPAREN)
		cs.Right = p.matchedFull(cs.Left, LPAREN, RPAREN)
		return cs
	case p.peek(DOLLAR):
		if p.willSpaced() {
			p.tok, p.val = LIT, DOLLAR.String()
			return p.wordPart()
		}
		switch {
		case p.readOnlyTok(HASH):
			p.advanceBoth(LIT, HASH.String())
		case p.readOnlyTok(DOLLAR):
			p.advanceBoth(LIT, DOLLAR.String())
		default:
			p.next()
		}
		pe := &ParamExp{
			Dollar: p.lpos,
			Short:  true,
		}
		p.gotLit(&pe.Param)
		return pe
	case p.peek(CMDIN), p.peek(CMDOUT):
		ps := &ProcSubst{Op: p.tok, OpPos: p.pos}
		ps.Stmts = p.stmtsNested(RPAREN)
		ps.Rparen = p.matchedFull(ps.OpPos, ps.Op, RPAREN)
		return ps
	case q != SQUOTE && p.peek(SQUOTE):
		sq := &SglQuoted{Quote: p.pos}
		s, found := p.readIncluding('\'')
		if !found {
			p.posErr(sq.Pos(), `reached EOF without closing quote %s`, SQUOTE)
		}
		sq.Value = s
		p.next()
		return sq
	case q != SQUOTE && p.peek(DOLLSQ):
		fallthrough
	case q != DQUOTE && (p.peek(DQUOTE) || p.peek(DOLLDQ)):
		q := &Quoted{Quote: p.tok, QuotePos: p.pos}
		stop := quotedStop(q.Quote)
		p.pushStops(stop)
		p.readParts(&q.Parts)
		p.popStop()
		p.closingQuote(q, stop)
		return q
	case q != BQUOTE && p.peek(BQUOTE):
		cs := &CmdSubst{Backquotes: true, Left: p.pos}
		cs.Stmts = p.stmtsNested(BQUOTE)
		p.closingQuote(cs, BQUOTE)
		cs.Right = p.lpos
		return cs
	}
	return nil
}

func quotedStop(start Token) Token {
	switch start {
	case DOLLSQ:
		return SQUOTE
	case DOLLDQ:
		return DQUOTE
	}
	return start
}

func (p *parser) arithmExpr(following Token) ArithmExpr {
	if p.eof() || p.peekArithmEnd() || p.tok == STOPPED {
		return nil
	}
	left := p.arithmExprBase(following)
	q := p.quote()
	if q != DRPAREN && q != LPAREN && p.spaced {
		return left
	}
	if p.eof() || p.peek(RPAREN) || p.peek(SEMICOLON) || p.tok == STOPPED {
		return left
	}
	if p.peek(LIT) {
		p.curErr("not a valid arithmetic operator: %s", p.val)
	}
	p.next()
	b := &BinaryExpr{
		OpPos: p.lpos,
		Op:    p.ltok,
		X:     left,
	}
	if q != DRPAREN && q != LPAREN && p.spaced {
		p.followErr(b.OpPos, b.Op, "an expression")
	}
	if b.Y = p.arithmExpr(b.Op); b.Y == nil {
		p.followErr(b.OpPos, b.Op, "an expression")
	}
	return b
}

func (p *parser) arithmExprBase(following Token) ArithmExpr {
	if p.got(INC) || p.got(DEC) || p.got(NOT) {
		pre := &UnaryExpr{OpPos: p.lpos, Op: p.ltok}
		pre.X = p.arithmExprBase(pre.Op)
		return pre
	}
	var x ArithmExpr
	q := p.quote()
	switch {
	case p.peek(LPAREN):
		p.pushStops(LPAREN)
		pe := &ParenExpr{Lparen: p.lpos}
		pe.X = p.arithmExpr(LPAREN)
		if pe.X == nil {
			p.posErr(pe.Lparen, "parentheses must enclose an expression")
		}
		p.popStop()
		pe.Rparen = p.matchedFull(pe.Lparen, LPAREN, RPAREN)
		x = pe
	case p.got(ADD), p.got(SUB):
		ue := &UnaryExpr{OpPos: p.lpos, Op: p.ltok}
		if q != DRPAREN && q != LPAREN && p.spaced {
			p.followErr(ue.OpPos, ue.Op, "an expression")
		}
		ue.X = p.arithmExpr(ue.Op)
		if ue.X == nil {
			p.followErr(ue.OpPos, ue.Op, "an expression")
		}
		x = ue
	default:
		w := p.followWord(following)
		x = &w
	}
	if q != DRPAREN && q != LPAREN && p.spaced {
		return x
	}
	if p.got(INC) || p.got(DEC) {
		return &UnaryExpr{
			Post:  true,
			OpPos: p.lpos,
			Op:    p.ltok,
			X:     x,
		}
	}
	return x
}

func (p *parser) gotParamLit(l *Lit) bool {
	if p.gotLit(l) {
		return true
	}
	if p.got(DOLLAR) || p.got(QUEST) {
		l.Value = p.ltok.String()
		return true
	}
	return false
}

func (p *parser) paramExp() *ParamExp {
	pe := &ParamExp{Dollar: p.pos}
	p.pushStops(LBRACE)
	pe.Length = p.got(HASH)
	if !p.gotParamLit(&pe.Param) && !pe.Length {
		p.posErr(pe.Dollar, "parameter expansion requires a literal")
	}
	if p.peek(RBRACE) {
		p.popStop()
		p.next()
		return pe
	}
	if p.peek(LBRACK) {
		p.pushStops(RBRACK)
		lpos := p.lpos
		pe.Ind = &Index{Word: p.getWord()}
		p.popStop()
		p.matchedFull(lpos, LBRACK, RBRACK)
	}
	if p.peek(RBRACE) {
		p.popStop()
		p.next()
		return pe
	}
	if pe.Length {
		p.curErr(`can only get length of a simple parameter`)
	}
	if p.peek(QUO) || p.peek(DQUO) {
		pe.Repl = &Replace{All: p.tok == DQUO}
		p.pushStops(QUO)
		pe.Repl.Orig = p.getWord()
		if p.peek(QUO) {
			p.popStop()
			p.pushStops(RBRACE)
			pe.Repl.With = p.getWord()
		}
		p.popStop()
	} else {
		pe.Exp = &Expansion{Op: p.tok}
		p.popStop()
		p.pushStops(RBRACE)
		pe.Exp.Word = p.getWord()
	}
	p.popStop()
	p.matchedFull(pe.Dollar, DOLLBR, RBRACE)
	return pe
}

func (p *parser) peekArithmEnd() bool {
	return p.peek(RPAREN) && p.willRead(')')
}

func (p *parser) arithmEnd(left Pos) Pos {
	if !p.peekArithmEnd() {
		p.matchingErr(left, DLPAREN, DRPAREN)
	}
	p.readOnlyTok(RPAREN)
	p.popStop()
	p.next()
	return p.lpos
}

func (p *parser) peekEnd() bool {
	return p.eof() || p.newLine || p.peek(SEMICOLON)
}

func (p *parser) peekStop() bool {
	if p.peekEnd() || p.peek(AND) || p.peek(OR) ||
		p.peek(LAND) || p.peek(LOR) || p.peek(PIPEALL) {
		return true
	}
	for i := len(p.stops) - 1; i >= 0; i-- {
		stop := p.stops[i]
		if p.peek(stop) {
			return true
		}
		if stop == BQUOTE || stop == RPAREN {
			break
		}
	}
	return false
}

var identRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func (p *parser) assignSplit() int {
	i := strings.Index(p.val, "=")
	if i > 0 && p.val[i-1] == '+' {
		i--
	}
	if i >= 0 && identRe.MatchString(p.val[:i]) {
		return i
	}
	return -1
}

func (p *parser) getAssign() (Assign, bool) {
	var as Assign
	i := p.assignSplit()
	if i < 0 {
		return as, false
	}
	as.Name = &Lit{ValuePos: p.pos, Value: p.val[:i]}
	if p.val[i] == '+' {
		as.Append = true
		i++
	}
	start := &Lit{ValuePos: p.pos, Value: p.val[i+1:]}
	if start.Value != "" {
		start.ValuePos.Column += i
		as.Value.Parts = append(as.Value.Parts, start)
	}
	p.next()
	if p.spaced {
		return as, true
	}
	if start.Value == "" && p.got(LPAREN) {
		ae := &ArrayExpr{Lparen: p.lpos}
		for !p.eof() && !p.peek(RPAREN) {
			if w, ok := p.gotWord(); !ok {
				p.curErr("array elements must be words")
			} else {
				ae.List = append(ae.List, w)
			}
		}
		ae.Rparen = p.matchedFull(ae.Lparen, LPAREN, RPAREN)
		as.Value.Parts = append(as.Value.Parts, ae)
	} else if !p.peekStop() {
		w := p.getWord()
		if start.Value == "" {
			as.Value = w
		} else {
			as.Value.Parts = append(as.Value.Parts, w.Parts...)
		}
	}
	return as, true
}

func (p *parser) peekRedir() bool {
	if p.peek(LIT) && (p.willRead('>') || p.willRead('<')) {
		return true
	}
	return p.peek(GTR) || p.peek(SHR) || p.peek(LSS) ||
		p.peek(DPLIN) || p.peek(DPLOUT) || p.peek(RDRINOUT) ||
		p.peek(SHL) || p.peek(DHEREDOC) || p.peek(WHEREDOC) ||
		p.peek(RDRALL) || p.peek(APPALL)
}

func (p *parser) gotRedirect() bool {
	if !p.peekRedir() {
		return false
	}
	s := p.stmtStack[len(p.stmtStack)-1]
	var r Redirect
	var l Lit
	if p.gotLit(&l) {
		r.N = &l
	}
	r.Op, r.OpPos = p.tok, p.pos
	p.next()
	switch r.Op {
	case SHL, DHEREDOC:
		p.stopNewline = true
		p.forbidNested = true
		r.Word = p.followWord(r.Op)
		p.forbidNested = false
		r.Hdoc = &Lit{}
		p.heredocs = append(p.heredocs, r)
		p.got(STOPPED)
	default:
		r.Word = p.followWord(r.Op)
	}
	s.Redirs = append(s.Redirs, r)
	return true
}

func (p *parser) getStmt(stops ...Token) (Stmt, bool) {
	var s Stmt
	p.stmtStack = append(p.stmtStack, &s)
	ok := p.gotStmtAndOr(&s, stops...)
	p.stmtStack = p.stmtStack[:len(p.stmtStack)-1]
	return s, ok
}

func (p *parser) gotStmtAndOr(s *Stmt, stops ...Token) bool {
	if p.peekRsrv(RBRACE) {
		// don't let it be a LIT
		return false
	}
	s.Position = p.pos
	if p.gotRsrv(NOT) {
		s.Negated = true
	}
	for {
		if as, ok := p.getAssign(); ok {
			s.Assigns = append(s.Assigns, as)
		} else if !p.gotRedirect() {
			break
		}
		if p.peekEnd() {
			p.gotSameLine(SEMICOLON)
			return true
		}
	}
	if !p.gotStmtPipe(s) && !s.Negated && len(s.Assigns) == 0 {
		return false
	}
	switch {
	case p.got(LAND), p.got(LOR):
		*s = p.binaryStmt(*s)
		return true
	case p.got(AND):
		s.Background = true
	}
	p.gotSameLine(SEMICOLON)
	return true
}

func (p *parser) gotStmtPipe(s *Stmt) bool {
	switch {
	case p.peek(LPAREN):
		s.Cmd = p.subshell()
	case p.gotRsrv(LBRACE):
		s.Cmd = p.block()
	case p.gotRsrv(IF):
		s.Cmd = p.ifClause()
	case p.gotRsrv(WHILE):
		s.Cmd = p.whileClause()
	case p.gotRsrv(UNTIL):
		s.Cmd = p.untilClause()
	case p.gotRsrv(FOR):
		s.Cmd = p.forClause()
	case p.gotRsrv(CASE):
		s.Cmd = p.caseClause()
	case p.gotRsrv(DECLARE), p.gotRsrv(LOCAL):
		s.Cmd = p.declClause()
	case p.gotRsrv(EVAL):
		s.Cmd = p.evalClause()
	case p.peekRsrv(LET):
		s.Cmd = p.letClause()
	default:
		s.Cmd = p.callOrFunc()
	}
	for !p.newLine && p.gotRedirect() {
	}
	if s.Cmd == nil && len(s.Redirs) == 0 {
		return false
	}
	if p.got(OR) || p.got(PIPEALL) {
		*s = p.binaryStmt(*s)
	}
	return true
}

func (p *parser) binaryStmt(left Stmt) Stmt {
	b := &BinaryCmd{
		OpPos: p.lpos,
		Op:    p.ltok,
		X:     left,
	}
	p.got(STOPPED)
	if b.Op == LAND || b.Op == LOR {
		var ok bool
		if b.Y, ok = p.getStmt(); !ok {
			p.followErr(b.OpPos, b.Op.String(), "a statement")
		}
	} else {
		s := Stmt{Position: p.pos}
		p.stmtStack = append(p.stmtStack, &s)
		if !p.gotStmtPipe(&s) {
			p.followErr(b.OpPos, b.Op, "a statement")
		}
		p.stmtStack = p.stmtStack[:len(p.stmtStack)-1]
		b.Y = s
	}
	return Stmt{Position: left.Position, Cmd: b}
}

func (p *parser) subshell() *Subshell {
	s := &Subshell{Lparen: p.pos}
	s.Stmts = p.stmtsNested(RPAREN)
	s.Rparen = p.matchedFull(s.Lparen, LPAREN, RPAREN)
	if len(s.Stmts) == 0 {
		p.posErr(s.Lparen, "a subshell must contain at least one statement")
	}
	return s
}

func (p *parser) block() *Block {
	b := &Block{Lbrace: p.lpos}
	b.Stmts = p.stmts(RBRACE)
	if !p.gotRsrv(RBRACE) {
		p.matchingErr(b.Lbrace, LBRACE, RBRACE)
	}
	b.Rbrace = p.lpos
	return b
}

func (p *parser) ifClause() *IfClause {
	ic := &IfClause{If: p.lpos}
	ic.Cond = p.cond(IF, THEN)
	ic.Then = p.followRsrv(ic.If, "if [stmts]", THEN)
	ic.ThenStmts = p.followStmts(THEN, FI, ELIF, ELSE)
	for p.gotRsrv(ELIF) {
		elf := Elif{Elif: p.lpos}
		elf.Cond = p.cond(ELIF, THEN)
		elf.Then = p.followRsrv(elf.Elif, "elif [stmts]", THEN)
		elf.ThenStmts = p.followStmts(THEN, FI, ELIF, ELSE)
		ic.Elifs = append(ic.Elifs, elf)
	}
	if p.gotRsrv(ELSE) {
		ic.Else = p.lpos
		ic.ElseStmts = p.followStmts(ELSE, FI)
	}
	ic.Fi = p.stmtEnd(ic, IF, FI)
	return ic
}

func (p *parser) cond(left Token, stop Token) Cond {
	if p.peek(LPAREN) && p.readOnlyTok(LPAREN) {
		p.pushStops(DRPAREN)
		c := &CStyleCond{Lparen: p.lpos}
		c.X = p.arithmExpr(DLPAREN)
		c.Rparen = p.arithmEnd(c.Lparen)
		p.gotSameLine(SEMICOLON)
		return c
	}
	stmts := p.followStmts(left, stop)
	if len(stmts) == 0 {
		return nil
	}
	return &StmtCond{Stmts: stmts}
}

func (p *parser) whileClause() *WhileClause {
	wc := &WhileClause{While: p.lpos}
	wc.Cond = p.cond(WHILE, DO)
	wc.Do = p.followRsrv(wc.While, "while [stmts]", DO)
	wc.DoStmts = p.followStmts(DO, DONE)
	wc.Done = p.stmtEnd(wc, WHILE, DONE)
	return wc
}

func (p *parser) untilClause() *UntilClause {
	uc := &UntilClause{Until: p.lpos}
	uc.Cond = p.cond(UNTIL, DO)
	uc.Do = p.followRsrv(uc.Until, "until [stmts]", DO)
	uc.DoStmts = p.followStmts(DO, DONE)
	uc.Done = p.stmtEnd(uc, UNTIL, DONE)
	return uc
}

func (p *parser) forClause() *ForClause {
	fc := &ForClause{For: p.lpos}
	fc.Loop = p.loop(fc.For)
	fc.Do = p.followRsrv(fc.For, "for foo [in words]", DO)
	fc.DoStmts = p.followStmts(DO, DONE)
	fc.Done = p.stmtEnd(fc, FOR, DONE)
	return fc
}

func (p *parser) loop(forPos Pos) Loop {
	if p.peek(LPAREN) && p.readOnlyTok(LPAREN) {
		p.pushStops(DRPAREN)
		cl := &CStyleLoop{Lparen: p.lpos}
		cl.Init = p.arithmExpr(DLPAREN)
		p.followFull(p.pos, "expression", SEMICOLON)
		cl.Cond = p.arithmExpr(SEMICOLON)
		p.followFull(p.pos, "expression", SEMICOLON)
		cl.Post = p.arithmExpr(SEMICOLON)
		cl.Rparen = p.arithmEnd(cl.Lparen)
		p.gotSameLine(SEMICOLON)
		return cl
	}
	wi := &WordIter{}
	if !p.gotLit(&wi.Name) {
		p.followErr(forPos, FOR, "a literal")
	}
	if p.gotRsrv(IN) {
		for !p.peekEnd() {
			if w, ok := p.gotWord(); !ok {
				p.curErr("word list can only contain words")
			} else {
				wi.List = append(wi.List, w)
			}
		}
		p.gotSameLine(SEMICOLON)
	} else if !p.gotSameLine(SEMICOLON) && !p.newLine {
		p.followErr(forPos, "for foo", `"in", ; or a newline`)
	}
	return wi
}

func (p *parser) caseClause() *CaseClause {
	cc := &CaseClause{Case: p.lpos}
	cc.Word = p.followWord(CASE)
	p.followRsrv(cc.Case, "case x", IN)
	cc.List = p.patLists()
	cc.Esac = p.stmtEnd(cc, CASE, ESAC)
	return cc
}

func (p *parser) patLists() (pls []PatternList) {
	if p.gotSameLine(SEMICOLON) {
		return
	}
	for !p.eof() && !p.peekRsrv(ESAC) {
		var pl PatternList
		p.got(LPAREN)
		for !p.eof() {
			if w, ok := p.gotWord(); !ok {
				p.curErr("case patterns must consist of words")
			} else {
				pl.Patterns = append(pl.Patterns, w)
			}
			if p.peek(RPAREN) {
				break
			}
			if !p.got(OR) {
				p.curErr("case patterns must be separated with |")
			}
		}
		pl.Stmts = p.stmtsNested(DSEMICOLON, ESAC, SEMIFALL, DSEMIFALL)
		if !p.got(DSEMICOLON) && !p.got(SEMIFALL) && !p.got(DSEMIFALL) {
			pl.Op, pl.OpPos = DSEMICOLON, p.lpos
			pls = append(pls, pl)
			break
		}
		pl.Op, pl.OpPos = p.ltok, p.lpos
		pls = append(pls, pl)
	}
	return
}

func (p *parser) declClause() *DeclClause {
	ds := &DeclClause{Declare: p.lpos}
	ds.Local = p.lval == LOCAL.String()
	for p.peek(LIT) && p.willSpaced() && p.val[0] == '-' {
		ds.Opts = append(ds.Opts, p.getWord())
	}
	for !p.peekStop() {
		if as, ok := p.getAssign(); ok {
			ds.Assigns = append(ds.Assigns, as)
			continue
		}
		if w, ok := p.gotWord(); !ok {
			p.followErr(p.pos, DECLARE, "words")
		} else {
			ds.Assigns = append(ds.Assigns, Assign{Value: w})
		}
	}
	return ds
}

func (p *parser) evalClause() *EvalClause {
	ec := &EvalClause{Eval: p.lpos}
	ec.Stmt, _ = p.getStmt()
	return ec
}

func (p *parser) letClause() *LetClause {
	lc := &LetClause{Let: p.pos}
	p.pushStops(DLPAREN)
	p.stopNewline = true
	for !p.peekStop() && p.tok != STOPPED {
		x := p.arithmExpr(LET)
		if x == nil {
			p.followErr(p.pos, LET, "arithmetic expressions")
		}
		lc.Exprs = append(lc.Exprs, x)
	}
	p.stopNewline = false
	p.popStop()
	p.got(STOPPED)
	return lc
}

func (p *parser) callOrFunc() Command {
	if p.gotRsrv(FUNCTION) {
		fpos := p.lpos
		w := p.followWord(FUNCTION)
		if p.gotSameLine(LPAREN) {
			p.followFull(w.Pos(), "foo(", RPAREN)
		}
		return p.funcDecl(w, fpos)
	}
	w, ok := p.gotWord()
	if !ok {
		return nil
	}
	if p.gotSameLine(LPAREN) {
		p.followFull(w.Pos(), "foo(", RPAREN)
		return p.funcDecl(w, w.Pos())
	}
	ce := &CallExpr{Args: []Word{w}}
	for !p.peekStop() {
		if p.got(STOPPED) || p.gotRedirect() {
		} else if w, ok := p.gotWord(); ok {
			ce.Args = append(ce.Args, w)
		} else {
			p.curErr("a command can only contain words and redirects")
		}
	}
	return ce
}

func (p *parser) funcDecl(w Word, pos Pos) *FuncDecl {
	if len(w.Parts) == 0 {
		return nil
	}
	fd := &FuncDecl{
		Position:  pos,
		BashStyle: pos != w.Pos(),
	}
	if lit, ok := w.Parts[0].(*Lit); !ok || len(w.Parts) > 1 {
		p.posErr(fd.Pos(), "invalid func name: %s", wordStr(w))
	} else {
		fd.Name = *lit
	}
	var ok bool
	if fd.Body, ok = p.getStmt(); !ok {
		p.followErr(fd.Pos(), "foo()", "a statement")
	}
	return fd
}
