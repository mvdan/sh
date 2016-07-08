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
	"sync"
)

// Mode controls the parser behaviour via a set of flags.
type Mode uint

const (
	ParseComments Mode = 1 << iota // add comments to the AST
)

var readerFree = sync.Pool{
	New: func() interface{} { return bufio.NewReader(nil) },
}

// Parse reads and parses a shell program with an optional name. It
// returns the parsed program if no issues were encountered. Otherwise,
// an error is returned.
func Parse(r io.Reader, name string, mode Mode) (*File, error) {
	p := parser{
		r:    readerFree.Get().(*bufio.Reader),
		f:    &File{Name: name},
		mode: mode,
	}
	p.f.lines = make([]int, 1, 16)
	p.r.Reset(r)
	p.next()
	p.f.Stmts = p.stmts()
	readerFree.Put(p.r)
	return p.f, p.err
}

type parser struct {
	r *bufio.Reader

	f    *File
	mode Mode

	spaced, newLine           bool
	stopNewline, forbidNested bool

	nextByte     byte
	err, nextErr error

	tok Token
	val string

	buf [8]byte

	pos, npos Pos

	// stack of stop tokens
	stops []Token
	quote Token

	// list of pending heredoc bodies
	heredocs []*Redirect
}

func (p *parser) pushStop(stop Token) {
	p.stops = append(p.stops, stop)
	p.quote = stop
	p.next()
}

func (p *parser) popStop() {
	p.stops = p.stops[:len(p.stops)-1]
	if len(p.stops) == 0 {
		p.quote = ILLEGAL
	} else {
		p.quote = p.stops[len(p.stops)-1]
	}
}

func (p *parser) willRead(b byte) bool {
	if p.nextErr != nil {
		return false
	}
	bs, err := p.r.Peek(1)
	if err != nil {
		p.nextErr = err
		return false
	}
	return bs[0] == b
}

func (p *parser) readOnly(b byte) bool {
	if p.willRead(b) {
		p.r.ReadByte()
		p.npos++
		if b == '\n' {
			p.f.lines = append(p.f.lines, int(p.npos))
		}
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

func (p *parser) next() {
	if p.tok == EOF {
		return
	}
	var err error
	b := p.nextByte
	p.spaced, p.newLine = false, false
	switch b {
	case 0:
		if p.nextErr != nil {
			p.errPass(err)
			return
		}
		if b, err = p.r.ReadByte(); err != nil {
			p.errPass(err)
			return
		}
		p.npos++
		if b == '\n' {
			p.f.lines = append(p.f.lines, int(p.npos))
		}
	case '\n':
		p.nextByte = 0
		p.doHeredocs()
	default:
		p.nextByte = 0
	}
	q := p.quote
	switch q {
	case QUO:
		p.pos = p.npos
		switch b {
		case '}':
			p.advanceTok(RBRACE)
		case '/':
			p.advanceTok(QUO)
		case '`', '"', '$':
			p.advanceTok(p.doRegToken(b))
		default:
			p.advanceReadLit(b, q)
		}
		return
	case DQUOTE:
		switch b {
		case '`', '"', '$':
			p.pos = p.npos
			p.advanceTok(p.doDqToken(b))
		case '\n':
			p.pos++
			p.advanceReadLit(b, q)
		default:
			p.pos = p.npos
			p.advanceReadLit(b, q)
		}
		return
	case RBRACE:
		p.pos = p.npos
		switch b {
		case '}':
			p.advanceTok(RBRACE)
		case '`', '"', '$':
			p.advanceTok(p.doRegToken(b))
		default:
			p.advanceReadLit(b, q)
		}
		return
	case SQUOTE:
		switch b {
		case '\'':
			p.pos = p.npos
			p.advanceTok(SQUOTE)
		case '\n':
			p.pos++
			p.advanceReadLit(b, q)
		default:
			p.pos = p.npos
			p.advanceReadLit(b, q)
		}
		return
	}
	if p.nextErr != nil {
		p.errPass(p.nextErr)
		return
	}
skipSpace:
	for {
		switch b {
		case ' ', '\t', '\r':
			p.spaced = true
		case '\n':
			if p.spaced {
				p.f.lines = append(p.f.lines, int(p.npos))
			} else {
				p.spaced = true
			}
			if p.stopNewline {
				p.nextByte = '\n'
				p.stopNewline = false
				p.advanceTok(STOPPED)
				return
			}
			p.newLine = true
		case '\\':
			if !p.readOnly('\n') {
				break skipSpace
			}
		default:
			break skipSpace
		}
		var err error
		if b, err = p.r.ReadByte(); err != nil {
			p.errPass(err)
			return
		}
		p.npos++
	}
	p.pos = p.npos
	switch {
	case q == LBRACE && paramOps(b):
		p.advanceTok(p.doParamToken(b))
	case q == RBRACK && b == ']':
		p.advanceTok(RBRACK)
	case b == '#' && q != LBRACE:
		bs, _ := p.readIncluding('\n')
		p.npos += Pos(len(bs))
		p.f.lines = append(p.f.lines, int(p.npos))
		p.nextByte = '\n'
		if p.mode&ParseComments > 0 {
			p.f.Comments = append(p.f.Comments, &Comment{
				Hash: p.pos,
				Text: string(bs),
			})
		}
		p.next()
	case (q == DLPAREN || q == DRPAREN || q == LPAREN) && arithmOps(b):
		p.advanceTok(p.doArithmToken(b))
	case regOps(b):
		p.advanceTok(p.doRegToken(b))
	case q == ILLEGAL, q == RPAREN, q == BQUOTE, q == DSEMICOLON:
		p.advanceReadLitNone(b)
	default:
		p.advanceReadLit(b, q)
	}
}

func (p *parser) advanceReadLitNone(b byte) {
	if b == '\\' && p.nextErr != nil {
		p.advanceBoth(LITWORD, string([]byte{b}))
		return
	}
	bs, b2, willBreak, err := p.noneLoopByte(b)
	p.nextByte = b2
	switch {
	case err != nil:
		p.nextErr = err
		fallthrough
	case willBreak:
		p.advanceBoth(LITWORD, string(bs))
	default:
		p.advanceBoth(LIT, string(bs))
	}
}

func (p *parser) advanceReadLit(b byte, q Token) {
	if b == '\\' && p.nextErr != nil {
		p.advanceBoth(LITWORD, string([]byte{b}))
		return
	}
	var bs []byte
	var err error
	if q == DQUOTE {
		bs, b, err = p.dqLoopByte(b)
	} else {
		bs, b, err = p.regLoopByte(b, q)
	}
	p.nextByte = b
	if err != nil {
		p.nextErr = err
		p.advanceBoth(LITWORD, string(bs))
	} else {
		p.advanceBoth(LIT, string(bs))
	}
}

func (p *parser) regLoopByte(b0 byte, q Token) (bs []byte, b byte, err error) {
	b = b0
byteLoop:
	for {
		switch {
		case b == '\\': // escaped byte follows
			if b, err = p.r.ReadByte(); err != nil {
				bs = append(bs, '\\')
				return
			}
			p.npos++
			if b == '\n' {
				p.f.lines = append(p.f.lines, int(p.npos))
			} else {
				bs = append(bs, '\\', b)
			}
			if b, err = p.r.ReadByte(); err != nil {
				return
			}
			p.npos++
			continue byteLoop
		case q == SQUOTE:
			if b == '\'' {
				return
			}
		case b == '`', b == '$':
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
		case b == '\n':
			if bs == nil {
				p.f.lines = append(p.f.lines, int(p.npos))
			}
			return
		case wordBreak(b), regOps(b):
			return
		case (q == DLPAREN || q == DRPAREN || q == LPAREN) && arithmOps(b):
			return
		}
		bs = append(bs, b)
		if b, err = p.r.ReadByte(); err != nil {
			return
		}
		p.npos++
	}
}

func (p *parser) noneLoopByte(b0 byte) (bs []byte, b byte, willBreak bool, err error) {
	bs = p.buf[:0]
	b = b0
	for {
		switch b {
		case '\\': // escaped byte follows
			if b, err = p.r.ReadByte(); err != nil {
				bs = append(bs, '\\')
				return
			}
			p.npos++
			if b == '\n' {
				p.f.lines = append(p.f.lines, int(p.npos))
			} else {
				bs = append(bs, '\\', b)
			}
			if b, err = p.r.ReadByte(); err != nil {
				return
			}
			p.npos++
		case '\n':
			if len(bs) > 0 {
				p.f.lines = append(p.f.lines, int(p.npos))
			}
			fallthrough
		case ' ', '\t', '\r', '&', '>', '<', '|', ';', '(', ')', '`':
			willBreak = true
			return
		case '"', '\'', '$':
			return
		default:
			bs = append(bs, b)
			if b, err = p.r.ReadByte(); err != nil {
				return
			}
			p.npos++
		}
	}
}

func (p *parser) dqLoopByte(b0 byte) (bs []byte, b byte, err error) {
	b = b0
	for {
		switch b {
		case '\\': // escaped byte follows
			if b, err = p.r.ReadByte(); err != nil {
				bs = append(bs, '\\')
				return
			}
			p.npos++
			if b == '\n' {
				p.f.lines = append(p.f.lines, int(p.npos))
			}
			bs = append(bs, '\\', b)
			if b, err = p.r.ReadByte(); err != nil {
				return
			}
			p.npos++
		case '`', '"', '$':
			return
		case '\n':
			if bs == nil {
				p.f.lines = append(p.f.lines, int(p.npos))
			}
			fallthrough
		default:
			bs = append(bs, b)
			if b, err = p.r.ReadByte(); err != nil {
				return
			}
			p.npos++
		}
	}
}

func (p *parser) advanceTok(tok Token)              { p.advanceBoth(tok, "") }
func (p *parser) advanceBoth(tok Token, val string) { p.tok, p.val = tok, val }

func (p *parser) readIncluding(b byte) ([]byte, bool) {
	bs, err := p.r.ReadBytes(b)
	if err != nil {
		p.nextErr = err
		return bs, false
	}
	return bs[:len(bs)-1], true
}

func (p *parser) doHeredocs() {
	for _, r := range p.heredocs {
		end := unquotedWordStr(p.f, &r.Word)
		r.Hdoc.ValuePos = p.npos
		r.Hdoc.Value, _ = p.readHdocBody(end, r.Op == DHEREDOC)
	}
	p.heredocs = nil
}

func (p *parser) readHdocBody(end string, noTabs bool) (string, bool) {
	var buf bytes.Buffer
	for p.nextErr == nil {
		bs, _ := p.readIncluding('\n')
		p.npos += Pos(len(bs))
		p.f.lines = append(p.f.lines, int(p.npos))
		line := string(bs)
		if line == end || (noTabs && strings.TrimLeft(line, "\t") == end) {
			// add trailing tabs
			buf.Write(bs[:len(bs)-len(end)])
			return buf.String(), true
		}
		buf.Write(bs)
		buf.WriteByte('\n')
	}
	return buf.String(), false
}

func wordBreak(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n' ||
		b == '&' || b == '>' || b == '<' || b == '|' ||
		b == ';' || b == '(' || b == ')' || b == '`'
}

func (p *parser) got(tok Token) bool {
	if p.tok == tok {
		p.next()
		return true
	}
	return false
}
func (p *parser) gotRsrv(val string) bool {
	if p.tok == LITWORD && p.val == val {
		p.next()
		return true
	}
	return false
}
func (p *parser) gotSameLine(tok Token) bool {
	return !p.newLine && p.got(tok)
}

func readableStr(s string) string {
	// don't quote tokens like & or }
	if s[0] >= 'a' && s[0] <= 'z' {
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
		p.followErr(lpos, left, fmt.Sprintf(`%q`, tok))
	}
	return pos
}
func (p *parser) followRsrv(lpos Pos, left, val string) Pos {
	pos := p.pos
	if !p.gotRsrv(val) {
		p.followErr(lpos, left, fmt.Sprintf(`%q`, val))
	}
	return pos
}

func (p *parser) followStmts(left string, lpos Pos, stops ...string) []*Stmt {
	if p.gotSameLine(SEMICOLON) {
		return nil
	}
	sts := p.stmts(stops...)
	if len(sts) < 1 && !p.newLine {
		p.followErr(lpos, left, "a statement list")
	}
	return sts
}

func (p *parser) followWordTok(tok Token, pos Pos) Word {
	w, ok := p.gotWord()
	if !ok {
		p.followErr(pos, tok.String(), "a word")
	}
	return w
}

func (p *parser) followWord(s string, pos Pos) Word {
	w, ok := p.gotWord()
	if !ok {
		p.followErr(pos, s, "a word")
	}
	return w
}

func (p *parser) stmtEnd(n Node, start, end string) Pos {
	pos := p.pos
	if !p.gotRsrv(end) {
		p.posErr(n.Pos(), `%s statement must end with %q`, start, end)
	}
	return pos
}

func (p *parser) quoteErr(lpos Pos, quote Token) {
	p.posErr(lpos, `reached %s without closing quote %s`, p.tok, quote)
}

func (p *parser) matchingErr(lpos Pos, left, right Token) {
	p.posErr(lpos, `reached %s without matching token %s with %s`,
		p.tok, left, right)
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
		if err != io.EOF {
			p.err = err
		}
		p.advanceTok(EOF)
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

func dsemicolon(t Token) bool {
	return t == DSEMICOLON || t == SEMIFALL || t == DSEMIFALL
}

func (p *parser) stmts(stops ...string) (sts []*Stmt) {
	if p.forbidNested {
		p.curErr("nested statements not allowed in this word")
	}
	q := p.quote
	gotEnd := true
	for p.tok != EOF {
		switch p.tok {
		case LITWORD:
			for _, stop := range stops {
				if p.val == stop {
					return
				}
			}
		case q:
			return
		case SEMIFALL, DSEMIFALL:
			if q == DSEMICOLON {
				return
			}
		}
		if !p.newLine && !gotEnd {
			p.curErr("statements must be separated by &, ; or a newline")
		}
		if p.tok == EOF {
			break
		}
		if s, end := p.getStmt(true); s == nil {
			p.invalidStmtStart()
		} else {
			sts = append(sts, s)
			gotEnd = end
		}
		p.got(STOPPED)
	}
	return
}

func (p *parser) invalidStmtStart() {
	switch p.tok {
	case SEMICOLON, AND, OR, LAND, LOR:
		p.curErr("%s can only immediately follow a statement", p.tok)
	case RPAREN:
		p.curErr("%s can only be used to close a subshell", p.tok)
	default:
		p.curErr("%s is not a valid start for a statement", p.tok)
	}
}

func (p *parser) getWord() Word { return Word{Parts: p.wordParts()} }
func (p *parser) gotWord() (Word, bool) {
	w := p.getWord()
	return w, len(w.Parts) > 0
}

func (p *parser) gotLit(l *Lit) bool {
	l.ValuePos = p.pos
	if p.tok == LIT || p.tok == LITWORD {
		l.Value = p.val
		p.next()
		return true
	}
	return false
}

func (p *parser) wordParts() (wps []WordPart) {
	if p.tok == LITWORD {
		wps = append(wps, &Lit{ValuePos: p.pos, Value: p.val})
		p.next()
		return
	}
	for {
		n := p.wordPart()
		if n == nil {
			return
		}
		wps = append(wps, n)
		if p.spaced {
			return
		}
	}
}

func (p *parser) wordPart() WordPart {
	switch p.tok {
	case LIT, LITWORD:
		l := &Lit{ValuePos: p.pos, Value: p.val}
		p.next()
		return l
	case p.quote:
		return nil
	case DOLLBR:
		return p.paramExp()
	case DOLLDP:
		ar := &ArithmExp{Dollar: p.pos}
		p.pushStop(DRPAREN)
		ar.X = p.arithmExpr(DOLLDP, ar.Dollar)
		ar.Rparen = p.arithmEnd(ar.Dollar)
		return ar
	case DOLLPR:
		cs := &CmdSubst{Left: p.pos}
		p.pushStop(RPAREN)
		cs.Stmts = p.stmts()
		p.popStop()
		cs.Right = p.matched(cs.Left, LPAREN, RPAREN)
		return cs
	case DOLLAR:
		var b byte
		if p.nextErr != nil {
			p.errPass(p.nextErr)
		} else {
			var err error
			if b, err = p.r.ReadByte(); err != nil {
				p.errPass(err)
			}
		}
		if p.tok == EOF || wordBreak(b) || b == '"' {
			l := &Lit{ValuePos: p.pos, Value: "$"}
			p.nextByte = b
			p.next()
			return l
		}
		pe := &ParamExp{Dollar: p.pos, Short: true}
		if b == '#' || b == '$' || b == '?' {
			p.advanceBoth(LIT, string(b))
		} else {
			p.nextByte = b
			p.next()
		}
		p.gotLit(&pe.Param)
		return pe
	case CMDIN, CMDOUT:
		ps := &ProcSubst{Op: p.tok, OpPos: p.pos}
		p.pushStop(RPAREN)
		ps.Stmts = p.stmts()
		p.popStop()
		ps.Rparen = p.matched(ps.OpPos, ps.Op, RPAREN)
		return ps
	case SQUOTE:
		sq := &SglQuoted{Quote: p.pos}
		bs, found := p.readIncluding('\'')
		rem := bs
		for {
			i := bytes.IndexByte(rem, '\n')
			if i < 0 {
				p.npos += Pos(len(rem))
				break
			}
			p.npos += Pos(i + 1)
			p.f.lines = append(p.f.lines, int(p.npos))
			rem = rem[i+1:]
		}
		p.npos++
		if !found {
			p.posErr(sq.Pos(), `reached EOF without closing quote %s`, SQUOTE)
		}
		sq.Value = string(bs)
		p.next()
		return sq
	case DOLLSQ:
		fallthrough
	case DQUOTE, DOLLDQ:
		q := &Quoted{Quote: p.tok, QuotePos: p.pos}
		stop := quotedStop(q.Quote)
		p.pushStop(stop)
		q.Parts = p.wordParts()
		p.popStop()
		if !p.got(stop) {
			p.quoteErr(q.Pos(), stop)
		}
		return q
	case BQUOTE:
		cs := &CmdSubst{Backquotes: true, Left: p.pos}
		p.pushStop(BQUOTE)
		cs.Stmts = p.stmts()
		p.popStop()
		cs.Right = p.pos
		if !p.got(BQUOTE) {
			p.quoteErr(cs.Pos(), BQUOTE)
		}
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

func (p *parser) arithmExpr(ftok Token, fpos Pos) ArithmExpr {
	if p.tok == EOF || p.peekArithmEnd() {
		return nil
	}
	left := p.arithmExprBase(ftok, fpos)
	q := p.quote
	if q != DRPAREN && q != LPAREN && p.spaced {
		return left
	}
	switch p.tok {
	case EOF, RPAREN, SEMICOLON, DSEMICOLON, SEMIFALL, DSEMIFALL:
		return left
	case LIT, LITWORD:
		p.curErr("not a valid arithmetic operator: %s", p.val)
	}
	b := &BinaryExpr{
		OpPos: p.pos,
		Op:    p.tok,
		X:     left,
	}
	p.next()
	if q != DRPAREN && q != LPAREN && p.spaced {
		p.followErr(b.OpPos, b.Op.String(), "an expression")
	}
	if b.Y = p.arithmExpr(b.Op, b.OpPos); b.Y == nil {
		p.followErr(b.OpPos, b.Op.String(), "an expression")
	}
	return b
}

func (p *parser) arithmExprBase(ftok Token, fpos Pos) ArithmExpr {
	if p.tok == INC || p.tok == DEC || p.tok == NOT {
		pre := &UnaryExpr{OpPos: p.pos, Op: p.tok}
		p.next()
		pre.X = p.arithmExprBase(pre.Op, pre.OpPos)
		return pre
	}
	var x ArithmExpr
	q := p.quote
	switch p.tok {
	case LPAREN:
		pe := &ParenExpr{Lparen: p.pos}
		p.pushStop(LPAREN)
		pe.X = p.arithmExpr(LPAREN, pe.Lparen)
		if pe.X == nil {
			p.posErr(pe.Lparen, "parentheses must enclose an expression")
		}
		p.popStop()
		pe.Rparen = p.matched(pe.Lparen, LPAREN, RPAREN)
		x = pe
	case ADD, SUB:
		ue := &UnaryExpr{OpPos: p.pos, Op: p.tok}
		p.next()
		if q != DRPAREN && q != LPAREN && p.spaced {
			p.followErr(ue.OpPos, ue.Op.String(), "an expression")
		}
		ue.X = p.arithmExpr(ue.Op, ue.OpPos)
		if ue.X == nil {
			p.followErr(ue.OpPos, ue.Op.String(), "an expression")
		}
		x = ue
	default:
		w := p.followWordTok(ftok, fpos)
		x = &w
	}
	if q != DRPAREN && q != LPAREN && p.spaced {
		return x
	}
	if p.tok == INC || p.tok == DEC {
		u := &UnaryExpr{
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
	switch p.tok {
	case LIT, LITWORD:
		l.ValuePos, l.Value = p.pos, p.val
	case DOLLAR:
		l.ValuePos, l.Value = p.pos, "$"
	case QUEST:
		l.ValuePos, l.Value = p.pos, "?"
	default:
		l.ValuePos = p.pos
		return false
	}
	p.next()
	return true
}

func (p *parser) paramExp() *ParamExp {
	pe := &ParamExp{Dollar: p.pos}
	p.pushStop(LBRACE)
	pe.Length = p.got(HASH)
	if !p.gotParamLit(&pe.Param) && !pe.Length {
		p.posErr(pe.Dollar, "parameter expansion requires a literal")
	}
	if p.tok == RBRACE {
		p.popStop()
		p.next()
		return pe
	}
	if p.tok == LBRACK {
		lpos := p.pos
		p.pushStop(RBRACK)
		pe.Ind = &Index{Word: p.getWord()}
		p.popStop()
		p.matched(lpos, LBRACK, RBRACK)
	}
	if p.tok == RBRACE {
		p.popStop()
		p.next()
		return pe
	}
	if pe.Length {
		p.curErr(`can only get length of a simple parameter`)
	}
	if p.tok == QUO || p.tok == DQUO {
		pe.Repl = &Replace{All: p.tok == DQUO}
		p.pushStop(QUO)
		pe.Repl.Orig = p.getWord()
		if p.tok == QUO {
			p.popStop()
			p.pushStop(RBRACE)
			pe.Repl.With = p.getWord()
		}
		p.popStop()
	} else {
		pe.Exp = &Expansion{Op: p.tok}
		p.popStop()
		p.pushStop(RBRACE)
		pe.Exp.Word = p.getWord()
	}
	p.popStop()
	p.matched(pe.Dollar, DOLLBR, RBRACE)
	return pe
}

func (p *parser) peekArithmEnd() bool {
	return p.tok == RPAREN && p.willRead(')')
}

func (p *parser) arithmEnd(left Pos) Pos {
	if !p.peekArithmEnd() {
		p.matchingErr(left, DLPAREN, DRPAREN)
	}
	p.r.ReadByte()
	p.npos++
	p.popStop()
	pos := p.pos
	p.next()
	return pos
}

func (p *parser) peekEnd() bool {
	return p.tok == EOF || p.newLine || p.tok == SEMICOLON
}

func (p *parser) peekStop() bool {
	return p.peekEnd() || p.tok == AND || p.tok == OR ||
		p.tok == LAND || p.tok == LOR || p.tok == PIPEALL ||
		p.tok == p.quote || (p.quote == DSEMICOLON && dsemicolon(p.tok))
}

var identRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func assignSplit(s string) int {
	i := strings.Index(s, "=")
	if i <= 0 {
		return -1
	}
	if s[i-1] == '+' {
		i--
	}
	if !identRe.MatchString(s[:i]) {
		return -1
	}
	return i
}

func (p *parser) getAssign() (*Assign, bool) {
	i := assignSplit(p.val)
	if i < 0 {
		return nil, false
	}
	as := &Assign{}
	as.Name = &Lit{ValuePos: p.pos, Value: p.val[:i]}
	if p.val[i] == '+' {
		as.Append = true
		i++
	}
	start := &Lit{ValuePos: p.pos, Value: p.val[i+1:]}
	if start.Value != "" {
		start.ValuePos += Pos(i)
		as.Value.Parts = append(as.Value.Parts, start)
	}
	p.next()
	if p.spaced {
		return as, true
	}
	if start.Value == "" && p.tok == LPAREN {
		ae := &ArrayExpr{Lparen: p.pos}
		p.next()
		for p.tok != EOF && p.tok != RPAREN {
			if w, ok := p.gotWord(); !ok {
				p.curErr("array elements must be words")
			} else {
				ae.List = append(ae.List, w)
			}
		}
		ae.Rparen = p.matched(ae.Lparen, LPAREN, RPAREN)
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
	switch p.tok {
	case LITWORD:
		return p.nextByte == '>' || p.nextByte == '<'
	case GTR, SHR, LSS, DPLIN, DPLOUT, RDRINOUT,
		SHL, DHEREDOC, WHEREDOC, RDRALL, APPALL:
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
	r.Op, r.OpPos = p.tok, p.pos
	p.next()
	switch r.Op {
	case SHL, DHEREDOC:
		p.stopNewline = true
		p.forbidNested = true
		r.Word = p.followWordTok(r.Op, r.OpPos)
		p.forbidNested = false
		r.Hdoc = &Lit{}
		p.heredocs = append(p.heredocs, r)
		p.got(STOPPED)
	default:
		r.Word = p.followWordTok(r.Op, r.OpPos)
	}
	s.Redirs = append(s.Redirs, r)
}

func (p *parser) getStmt(readEnd bool) (s *Stmt, gotEnd bool) {
	s = &Stmt{Position: p.pos}
	if p.gotRsrv("!") {
		s.Negated = true
	}
preLoop:
	for {
		switch p.tok {
		case LIT, LITWORD:
			if as, ok := p.getAssign(); ok {
				s.Assigns = append(s.Assigns, as)
			} else if p.nextByte == '>' || p.nextByte == '<' {
				p.doRedirect(s)
			} else {
				break preLoop
			}
		case GTR, SHR, LSS, DPLIN, DPLOUT, RDRINOUT,
			SHL, DHEREDOC, WHEREDOC, RDRALL, APPALL:
			p.doRedirect(s)
		default:
			break preLoop
		}
		if p.peekEnd() {
			gotEnd = p.gotSameLine(SEMICOLON)
			return
		}
	}
	if s = p.gotStmtPipe(s); s == nil {
		return
	}
	switch p.tok {
	case LAND, LOR:
		s = p.binaryCmdAndOr(s)
	case AND:
		p.next()
		s.Background = true
		gotEnd = true
	}
	if readEnd && p.gotSameLine(SEMICOLON) {
		gotEnd = true
	}
	return
}

func (p *parser) gotStmtPipe(s *Stmt) *Stmt {
	switch p.tok {
	case LPAREN:
		s.Cmd = p.subshell()
	case LITWORD:
		switch p.val {
		case "}":
			p.curErr("%s can only be used to close a block", p.val)
		case "{":
			s.Cmd = p.block()
		case "if":
			s.Cmd = p.ifClause()
		case "while":
			s.Cmd = p.whileClause()
		case "until":
			s.Cmd = p.untilClause()
		case "for":
			s.Cmd = p.forClause()
		case "case":
			s.Cmd = p.caseClause()
		case "declare":
			s.Cmd = p.declClause(false)
		case "local":
			s.Cmd = p.declClause(true)
		case "eval":
			s.Cmd = p.evalClause()
		case "let":
			s.Cmd = p.letClause()
		case "function":
			s.Cmd = p.bashFuncDecl()
		default:
			name := Lit{ValuePos: p.pos, Value: p.val}
			w := p.getWord()
			if p.gotSameLine(LPAREN) {
				p.follow(name.ValuePos, "foo(", RPAREN)
				s.Cmd = p.funcDecl(name, name.ValuePos)
			} else {
				s.Cmd = p.callExpr(s, w)
			}
		}
	case LIT, DOLLBR, DOLLDP, DOLLPR, DOLLAR, CMDIN, CMDOUT,
		SQUOTE, DOLLSQ, DQUOTE, DOLLDQ, BQUOTE:
		w := p.getWord()
		if p.gotSameLine(LPAREN) && p.err == nil {
			p.posErr(w.Pos(), "invalid func name: %s", wordStr(p.f, w))
		}
		s.Cmd = p.callExpr(s, w)
	}
	for !p.newLine && p.peekRedir() {
		p.doRedirect(s)
	}
	if s.Cmd == nil && len(s.Redirs) == 0 && !s.Negated && len(s.Assigns) == 0 {
		return nil
	}
	if p.tok == OR || p.tok == PIPEALL {
		s = p.binaryCmdPipe(s)
	}
	return s
}

func (p *parser) binaryCmdAndOr(left *Stmt) *Stmt {
	b := &BinaryCmd{OpPos: p.pos, Op: p.tok, X: left}
	p.next()
	p.got(STOPPED)
	if b.Y, _ = p.getStmt(false); b.Y == nil {
		p.followErr(b.OpPos, b.Op.String(), "a statement")
	}
	return &Stmt{Position: left.Position, Cmd: b}
}

func (p *parser) binaryCmdPipe(left *Stmt) *Stmt {
	b := &BinaryCmd{OpPos: p.pos, Op: p.tok, X: left}
	p.next()
	p.got(STOPPED)
	if b.Y = p.gotStmtPipe(&Stmt{Position: p.pos}); b.Y == nil {
		p.followErr(b.OpPos, b.Op.String(), "a statement")
	}
	return &Stmt{Position: left.Position, Cmd: b}
}

func (p *parser) subshell() *Subshell {
	s := &Subshell{Lparen: p.pos}
	p.pushStop(RPAREN)
	s.Stmts = p.stmts()
	p.popStop()
	s.Rparen = p.matched(s.Lparen, LPAREN, RPAREN)
	if len(s.Stmts) == 0 {
		p.posErr(s.Lparen, "a subshell must contain at least one statement")
	}
	return s
}

func (p *parser) block() *Block {
	b := &Block{Lbrace: p.pos}
	p.next()
	b.Stmts = p.stmts("}")
	b.Rbrace = p.pos
	if !p.gotRsrv("}") {
		p.posErr(b.Lbrace, `reached %s without matching word { with }`, p.tok)
	}
	return b
}

func (p *parser) ifClause() *IfClause {
	ic := &IfClause{If: p.pos}
	p.next()
	ic.Cond = p.cond("if", ic.If, "then")
	ic.Then = p.followRsrv(ic.If, "if [stmts]", "then")
	ic.ThenStmts = p.followStmts("then", ic.Then, "fi", "elif", "else")
	elifPos := p.pos
	for p.gotRsrv("elif") {
		elf := &Elif{Elif: elifPos}
		elf.Cond = p.cond("elif", elf.Elif, "then")
		elf.Then = p.followRsrv(elf.Elif, "elif [stmts]", "then")
		elf.ThenStmts = p.followStmts("then", elf.Then, "fi", "elif", "else")
		ic.Elifs = append(ic.Elifs, elf)
		elifPos = p.pos
	}
	elsePos := p.pos
	if p.gotRsrv("else") {
		ic.Else = elsePos
		ic.ElseStmts = p.followStmts("else", ic.Else, "fi")
	}
	ic.Fi = p.stmtEnd(ic, "if", "fi")
	return ic
}

func (p *parser) cond(left string, lpos Pos, stop string) Cond {
	if p.tok == LPAREN && p.readOnly('(') {
		c := &CStyleCond{Lparen: p.pos}
		p.pushStop(DRPAREN)
		c.X = p.arithmExpr(DLPAREN, c.Lparen)
		c.Rparen = p.arithmEnd(c.Lparen)
		p.gotSameLine(SEMICOLON)
		return c
	}
	stmts := p.followStmts(left, lpos, stop)
	if len(stmts) == 0 {
		return nil
	}
	return &StmtCond{Stmts: stmts}
}

func (p *parser) whileClause() *WhileClause {
	wc := &WhileClause{While: p.pos}
	p.next()
	wc.Cond = p.cond("while", wc.While, "do")
	wc.Do = p.followRsrv(wc.While, "while [stmts]", "do")
	wc.DoStmts = p.followStmts("do", wc.Do, "done")
	wc.Done = p.stmtEnd(wc, "while", "done")
	return wc
}

func (p *parser) untilClause() *UntilClause {
	uc := &UntilClause{Until: p.pos}
	p.next()
	uc.Cond = p.cond("until", uc.Until, "do")
	uc.Do = p.followRsrv(uc.Until, "until [stmts]", "do")
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
	if p.tok == LPAREN && p.readOnly('(') {
		cl := &CStyleLoop{Lparen: p.pos}
		p.pushStop(DRPAREN)
		cl.Init = p.arithmExpr(DLPAREN, cl.Lparen)
		scPos := p.pos
		p.follow(p.pos, "expression", SEMICOLON)
		cl.Cond = p.arithmExpr(SEMICOLON, scPos)
		scPos = p.pos
		p.follow(p.pos, "expression", SEMICOLON)
		cl.Post = p.arithmExpr(SEMICOLON, scPos)
		cl.Rparen = p.arithmEnd(cl.Lparen)
		p.gotSameLine(SEMICOLON)
		return cl
	}
	wi := &WordIter{}
	if !p.gotLit(&wi.Name) {
		p.followErr(forPos, "for", "a literal")
	}
	if p.gotRsrv("in") {
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
	cc := &CaseClause{Case: p.pos}
	p.next()
	cc.Word = p.followWord("case", cc.Case)
	p.followRsrv(cc.Case, "case x", "in")
	cc.List = p.patLists()
	cc.Esac = p.stmtEnd(cc, "case", "esac")
	return cc
}

func (p *parser) patLists() (pls []*PatternList) {
	if p.gotSameLine(SEMICOLON) {
		return
	}
	for p.tok != EOF && !(p.tok == LITWORD && p.val == "esac") {
		pl := &PatternList{}
		p.got(LPAREN)
		for p.tok != EOF {
			if w, ok := p.gotWord(); !ok {
				p.curErr("case patterns must consist of words")
			} else {
				pl.Patterns = append(pl.Patterns, w)
			}
			if p.tok == RPAREN {
				break
			}
			if !p.got(OR) {
				p.curErr("case patterns must be separated with |")
			}
		}
		p.pushStop(DSEMICOLON)
		pl.Stmts = p.stmts("esac")
		p.popStop()
		if !dsemicolon(p.tok) {
			pl.Op, pl.OpPos = DSEMICOLON, p.pos
			pls = append(pls, pl)
			break
		}
		pl.Op, pl.OpPos = p.tok, p.pos
		p.next()
		pls = append(pls, pl)
	}
	return
}

func (p *parser) declClause(local bool) *DeclClause {
	ds := &DeclClause{Declare: p.pos, Local: local}
	p.next()
	for p.tok == LITWORD && p.val[0] == '-' {
		ds.Opts = append(ds.Opts, p.getWord())
	}
	for !p.peekStop() {
		if as, ok := p.getAssign(); ok {
			ds.Assigns = append(ds.Assigns, as)
			continue
		}
		if w, ok := p.gotWord(); !ok {
			p.followErr(p.pos, "declare", "words")
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

func (p *parser) letClause() *LetClause {
	lc := &LetClause{Let: p.pos}
	p.pushStop(DLPAREN)
	p.stopNewline = true
	for !p.peekStop() && p.tok != STOPPED && !dsemicolon(p.tok) {
		x := p.arithmExpr(LET, lc.Let)
		if x == nil {
			p.followErr(p.pos, "let", "arithmetic expressions")
		}
		lc.Exprs = append(lc.Exprs, x)
	}
	if len(lc.Exprs) == 0 {
		p.posErr(lc.Let, "let clause requires at least one expression")
	}
	p.stopNewline = false
	p.popStop()
	p.got(STOPPED)
	return lc
}

func (p *parser) bashFuncDecl() *FuncDecl {
	fpos := p.pos
	p.next()
	if p.tok != LITWORD {
		w := p.followWord("function", fpos)
		if p.err == nil {
			p.posErr(w.Pos(), "invalid func name: %s", wordStr(p.f, w))
		}
	}
	name := Lit{ValuePos: p.pos, Value: p.val}
	p.next()
	if p.gotSameLine(LPAREN) {
		p.follow(name.ValuePos, "foo(", RPAREN)
	}
	return p.funcDecl(name, fpos)
}

func (p *parser) callExpr(s *Stmt, w Word) *CallExpr {
	ce := &CallExpr{Args: []Word{w}}
argLoop:
	for !p.peekStop() {
		switch p.tok {
		case STOPPED:
			p.next()
		case LITWORD:
			if p.nextByte == '>' || p.nextByte == '<' {
				p.doRedirect(s)
				continue argLoop
			}
			fallthrough
		case LIT, DOLLBR, DOLLDP, DOLLPR, DOLLAR, CMDIN, CMDOUT,
			SQUOTE, DOLLSQ, DQUOTE, DOLLDQ, BQUOTE:
			ce.Args = append(ce.Args, p.getWord())
		case GTR, SHR, LSS, DPLIN, DPLOUT, RDRINOUT,
			SHL, DHEREDOC, WHEREDOC, RDRALL, APPALL:
			p.doRedirect(s)
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
