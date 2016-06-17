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

	stopNewline bool
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
	for {
		if b = p.peekByte(); p.tok == EOF {
			p.lpos, p.pos = p.pos, p.npos
			return
		}
		if q != DQUOTE && b == '\\' && p.readOnlyStr("\\\n") {
			continue
		}
		if q == DQUOTE || q == SQUOTE || q == RBRACE || q == QUO || !space(b) {
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
	q := p.quote()
	for {
		b := p.peekByte()
		if p.tok == EOF {
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
		case q == DQUOTE:
			if b == '"' || (b == '$' && !p.willReadStr(`$"`)) {
				return
			}
		case b == '$', b == '`':
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
		case regOps(b), space(b):
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
	} else {
		bs = bs[:len(bs)-1]
		p.br.UnreadByte()
	}
	p.npos = moveWithBytes(p.npos, bs)
	return string(bs), err == nil
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
		line, _ := p.readUntil('\n')
		if line == end || (noTabs && strings.TrimLeft(line, "\t") == end) {
			// add trailing tabs
			fmt.Fprint(&buf, line[:len(line)-len(end)])
			return buf.String(), true
		}
		fmt.Fprintln(&buf, line)
		p.readOnly('\n')
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

func (p *parser) peekFull(tok Token) bool { return p.tok == tok }
func (p *parser) peekReserved(tok Token) bool {
	return p.tok == LIT && p.val == tok.String() && p.willSpaced()
}
func (p *parser) peek(tok Token) bool {
	return p.peekFull(tok) || p.peekReserved(tok)
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

func (p *parser) peekAny(toks ...Token) bool {
	for _, tok := range toks {
		if p.peek(tok) {
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
func (p *parser) gotReserved(tok Token) bool {
	p.saveComments()
	if p.peekReserved(tok) {
		p.next()
		return true
	}
	return false
}
func (p *parser) gotSameLine(tok Token) bool { return !p.newLine && p.got(tok) }

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

func (p *parser) followTok(lpos Pos, left string, tok Token) Pos {
	if !p.got(tok) {
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
	if !p.got(tok) {
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

func (p *parser) matchedTok(lpos Pos, left, right Token) Pos {
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
		if p.peekAny(stops...) {
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
	case p.peekAny(SEMICOLON, AND, OR, LAND, LOR):
		p.curErr("%s can only immediately follow a statement", p.tok)
	case p.peekReserved(RBRACE):
		p.curErr("%s can only be used to close a block", p.val)
	case p.peekFull(RPAREN):
		p.curErr("%s can only be used to close a subshell", p.tok)
	default:
		p.curErr("%s is not a valid start for a statement", p.tok)
	}
}

func (p *parser) stmtsNested(stops ...Token) []Stmt {
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
	case p.peekFull(DOLLBR):
		return p.paramExp()
	case p.peekFull(DOLLDP):
		p.pushStops(DRPAREN)
		ar := &ArithmExp{Dollar: p.lpos}
		ar.X = p.arithmExpr(DLPAREN)
		ar.Rparen = p.arithmEnd(ar.Dollar)
		return ar
	case p.peekFull(DOLLPR):
		cs := &CmdSubst{Left: p.pos}
		cs.Stmts = p.stmtsNested(RPAREN)
		cs.Right = p.matchedTok(cs.Left, LPAREN, RPAREN)
		return cs
	case p.peekFull(DOLLAR):
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
	case p.peekFull(CMDIN), p.peekFull(CMDOUT):
		ps := &ProcSubst{Op: p.tok, OpPos: p.pos}
		ps.Stmts = p.stmtsNested(RPAREN)
		ps.Rparen = p.matchedTok(ps.OpPos, ps.Op, RPAREN)
		return ps
	case q != SQUOTE && p.peekFull(SQUOTE):
		sq := &SglQuoted{Quote: p.pos}
		s, found := p.readUntil('\'')
		if !found {
			p.posErr(sq.Pos(), `reached EOF without closing quote %s`, SQUOTE)
		}
		sq.Value = s
		p.readOnlyTok(SQUOTE)
		p.next()
		return sq
	case q != SQUOTE && p.peekFull(DOLLSQ):
		fallthrough
	case q != DQUOTE && (p.peekFull(DQUOTE) || p.peekFull(DOLLDQ)):
		q := &Quoted{Quote: p.tok, QuotePos: p.pos}
		stop := quotedStop(q.Quote)
		p.pushStops(stop)
		p.readParts(&q.Parts)
		p.popStop()
		p.closingQuote(q, stop)
		return q
	case q != BQUOTE && p.peekFull(BQUOTE):
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
	if p.eof() || p.peekFull(RPAREN) || p.peekFull(SEMICOLON) || p.tok == STOPPED {
		return left
	}
	if p.peekFull(LIT) {
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
	case p.peekFull(LPAREN):
		p.pushStops(LPAREN)
		pe := &ParenExpr{Lparen: p.lpos}
		pe.X = p.arithmExpr(LPAREN)
		if pe.X == nil {
			p.posErr(pe.Lparen, "parentheses must enclose an expression")
		}
		p.popStop()
		pe.Rparen = p.matchedTok(pe.Lparen, LPAREN, RPAREN)
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
	if p.peekFull(RBRACE) {
		p.popStop()
		p.next()
		return pe
	}
	if p.peekFull(LBRACK) {
		p.pushStops(RBRACK)
		lpos := p.lpos
		pe.Ind = &Index{Word: p.getWord()}
		p.popStop()
		p.matchedTok(lpos, LBRACK, RBRACK)
	}
	if p.peekFull(RBRACE) {
		p.popStop()
		p.next()
		return pe
	}
	if pe.Length {
		p.curErr(`can only get length of a simple parameter`)
	}
	if p.peekFull(QUO) || p.peekFull(DQUO) {
		pe.Repl = &Replace{All: p.tok == DQUO}
		p.pushStops(QUO)
		pe.Repl.Orig = p.getWord()
		if p.peekFull(QUO) {
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
	p.matchedTok(pe.Dollar, DOLLBR, RBRACE)
	return pe
}

func (p *parser) peekArithmEnd() bool {
	return p.peekFull(RPAREN) && p.willRead(')')
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
	return p.eof() || p.newLine || p.peekFull(SEMICOLON)
}

func (p *parser) peekStop() bool {
	if p.peekEnd() || p.peekFull(AND) || p.peekFull(OR) ||
		p.peekFull(LAND) || p.peekFull(LOR) || p.peekFull(PIPEALL) {
		return true
	}
	for i := len(p.stops) - 1; i >= 0; i-- {
		stop := p.stops[i]
		if p.peekFull(stop) {
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
		ae.Rparen = p.matchedTok(ae.Lparen, LPAREN, RPAREN)
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
	if p.peekFull(LIT) && (p.willRead('>') || p.willRead('<')) {
		return true
	}
	return p.peekFull(GTR) || p.peekFull(SHR) || p.peekFull(LSS) ||
		p.peekFull(DPLIN) || p.peekFull(DPLOUT) || p.peekFull(RDRINOUT) ||
		p.peekFull(SHL) || p.peekFull(DHEREDOC) || p.peekFull(WHEREDOC) ||
		p.peekFull(RDRALL) || p.peekFull(APPALL)
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
		r.Word = p.followWord(r.Op)
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
	if p.peekReserved(RBRACE) {
		// don't let it be a LIT
		return false
	}
	s.Position = p.pos
	if p.gotReserved(NOT) {
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
	case p.peekFull(LPAREN):
		s.Cmd = p.subshell()
	case p.gotReserved(LBRACE):
		s.Cmd = p.block()
	case p.gotReserved(IF):
		s.Cmd = p.ifClause()
	case p.gotReserved(WHILE):
		s.Cmd = p.whileClause()
	case p.gotReserved(UNTIL):
		s.Cmd = p.untilClause()
	case p.gotReserved(FOR):
		s.Cmd = p.forClause()
	case p.gotReserved(CASE):
		s.Cmd = p.caseClause()
	case p.gotReserved(DECLARE), p.gotReserved(LOCAL):
		s.Cmd = p.declClause()
	case p.gotReserved(EVAL):
		s.Cmd = p.evalClause()
	case p.peekReserved(LET):
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
	s.Rparen = p.matchedTok(s.Lparen, LPAREN, RPAREN)
	if len(s.Stmts) == 0 {
		p.posErr(s.Lparen, "a subshell must contain at least one statement")
	}
	return s
}

func (p *parser) block() *Block {
	b := &Block{Lbrace: p.lpos}
	b.Stmts = p.stmts(RBRACE)
	b.Rbrace = p.matchedTok(b.Lbrace, LBRACE, RBRACE)
	return b
}

func (p *parser) ifClause() *IfClause {
	ic := &IfClause{If: p.lpos}
	ic.Cond = p.cond(IF, THEN)
	ic.Then = p.followTok(ic.If, "if [stmts]", THEN)
	ic.ThenStmts = p.followStmts(THEN, FI, ELIF, ELSE)
	for p.gotReserved(ELIF) {
		elf := Elif{Elif: p.lpos}
		elf.Cond = p.cond(ELIF, THEN)
		elf.Then = p.followTok(elf.Elif, "elif [stmts]", THEN)
		elf.ThenStmts = p.followStmts(THEN, FI, ELIF, ELSE)
		ic.Elifs = append(ic.Elifs, elf)
	}
	if p.gotReserved(ELSE) {
		ic.Else = p.lpos
		ic.ElseStmts = p.followStmts(ELSE, FI)
	}
	ic.Fi = p.stmtEnd(ic, IF, FI)
	return ic
}

func (p *parser) cond(left Token, stop Token) Cond {
	if p.peekFull(LPAREN) && p.readOnlyTok(LPAREN) {
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
	wc.Do = p.followTok(wc.While, "while [stmts]", DO)
	wc.DoStmts = p.followStmts(DO, DONE)
	wc.Done = p.stmtEnd(wc, WHILE, DONE)
	return wc
}

func (p *parser) untilClause() *UntilClause {
	uc := &UntilClause{Until: p.lpos}
	uc.Cond = p.cond(UNTIL, DO)
	uc.Do = p.followTok(uc.Until, "until [stmts]", DO)
	uc.DoStmts = p.followStmts(DO, DONE)
	uc.Done = p.stmtEnd(uc, UNTIL, DONE)
	return uc
}

func (p *parser) forClause() *ForClause {
	fc := &ForClause{For: p.lpos}
	fc.Loop = p.loop(fc.For)
	fc.Do = p.followTok(fc.For, "for foo [in words]", DO)
	fc.DoStmts = p.followStmts(DO, DONE)
	fc.Done = p.stmtEnd(fc, FOR, DONE)
	return fc
}

func (p *parser) loop(forPos Pos) Loop {
	if p.peekFull(LPAREN) && p.readOnlyTok(LPAREN) {
		p.pushStops(DRPAREN)
		cl := &CStyleLoop{Lparen: p.lpos}
		cl.Init = p.arithmExpr(DLPAREN)
		p.followTok(p.pos, "expression", SEMICOLON)
		cl.Cond = p.arithmExpr(SEMICOLON)
		p.followTok(p.pos, "expression", SEMICOLON)
		cl.Post = p.arithmExpr(SEMICOLON)
		cl.Rparen = p.arithmEnd(cl.Lparen)
		p.gotSameLine(SEMICOLON)
		return cl
	}
	wi := &WordIter{}
	if !p.gotLit(&wi.Name) {
		p.followErr(forPos, FOR, "a literal")
	}
	if p.gotReserved(IN) {
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
	p.followTok(cc.Case, "case x", IN)
	cc.List = p.patLists()
	cc.Esac = p.stmtEnd(cc, CASE, ESAC)
	return cc
}

func (p *parser) patLists() (pls []PatternList) {
	if p.gotSameLine(SEMICOLON) {
		return
	}
	for !p.eof() && !p.peekReserved(ESAC) {
		var pl PatternList
		p.got(LPAREN)
		for !p.eof() {
			if w, ok := p.gotWord(); !ok {
				p.curErr("case patterns must consist of words")
			} else {
				pl.Patterns = append(pl.Patterns, w)
			}
			if p.peekFull(RPAREN) {
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
	for p.peekFull(LIT) && p.willSpaced() && p.val[0] == '-' {
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
	if p.gotReserved(FUNCTION) {
		fpos := p.lpos
		w := p.followWord(FUNCTION)
		if p.gotSameLine(LPAREN) {
			p.followTok(w.Pos(), "foo(", RPAREN)
		}
		return p.funcDecl(w, fpos)
	}
	w, ok := p.gotWord()
	if !ok {
		return nil
	}
	if p.gotSameLine(LPAREN) {
		p.followTok(w.Pos(), "foo(", RPAREN)
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
