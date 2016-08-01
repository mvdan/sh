// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
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
func Parse(src []byte, name string, mode Mode) (*File, error) {
	p := parser{
		f:    &File{Name: name},
		src:  src,
		mode: mode,
	}
	p.f.lines = make([]int, 1, 16)
	p.next()
	p.f.Stmts = p.stmts()
	return p.f, p.err
}

type parser struct {
	src []byte

	f    *File
	mode Mode

	spaced, newLine           bool
	stopNewline, forbidNested bool

	err error

	tok Token
	val string

	buf [8]byte

	pos  Pos
	npos int

	quote Token

	// list of pending heredoc bodies
	heredocs []*Redirect
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
	if p.npos >= len(p.src) {
		p.errPass(io.EOF)
		return
	}
	b := p.src[p.npos]
	if p.tok == STOPPED && b == '\n' {
		p.npos++
		p.f.lines = append(p.f.lines, p.npos)
		p.doHeredocs()
		if p.npos >= len(p.src) {
			p.errPass(io.EOF)
			return
		}
		b = p.src[p.npos]
		p.spaced, p.newLine = true, true
	} else {
		p.spaced, p.newLine = false, false
	}
	q := p.quote
	switch q {
	case QUO:
		p.pos = Pos(p.npos + 1)
		switch b {
		case '}':
			p.npos++
			p.tok = RBRACE
		case '/':
			p.npos++
			p.tok = QUO
		case '`', '"', '$':
			p.tok = p.dqToken(b)
		default:
			p.advanceLitOther(q)
		}
		return
	case DQUOTE:
		p.pos = Pos(p.npos + 1)
		if b == '`' || b == '"' || b == '$' {
			p.tok = p.dqToken(b)
		} else {
			p.advanceLitDquote()
		}
		return
	case RBRACE:
		p.pos = Pos(p.npos + 1)
		switch b {
		case '}':
			p.npos++
			p.tok = RBRACE
		case '`', '"', '$':
			p.tok = p.dqToken(b)
		default:
			p.advanceLitOther(q)
		}
		return
	case SQUOTE:
		p.pos = Pos(p.npos + 1)
		if b == '\'' {
			p.npos++
			p.tok = SQUOTE
		} else {
			p.advanceLitOther(q)
		}
		return
	}
skipSpace:
	for {
		switch b {
		case ' ', '\t', '\r':
			p.spaced = true
			p.npos++
		case '\n':
			if p.stopNewline {
				p.stopNewline = false
				p.tok = STOPPED
				return
			}
			p.spaced = true
			if p.npos < len(p.src) {
				p.npos++
			}
			p.f.lines = append(p.f.lines, p.npos)
			p.newLine = true
		case '\\':
			if p.npos < len(p.src)-1 && p.src[p.npos+1] == '\n' {
				p.npos += 2
				p.f.lines = append(p.f.lines, p.npos)
			} else {
				break skipSpace
			}
		default:
			break skipSpace
		}
		if p.npos >= len(p.src) {
			p.errPass(io.EOF)
			return
		}
		b = p.src[p.npos]
	}
	p.pos = Pos(p.npos + 1)
	switch {
	case q == ILLEGAL, q == RPAREN, q == BQUOTE, q == DSEMICOLON:
		switch b {
		case ';', '"', '\'', '(', ')', '$', '|', '&', '>', '<', '`':
			p.tok = p.regToken(b)
		case '#':
			p.npos++
			bs, _ := p.readUntil('\n')
			p.npos += len(bs)
			if p.mode&ParseComments > 0 {
				p.f.Comments = append(p.f.Comments, &Comment{
					Hash: p.pos,
					Text: string(bs),
				})
			}
			p.next()
		default:
			p.advanceLitNone()
		}
	case q == LBRACE && paramOps(b):
		p.tok = p.paramToken(b)
	case q == DRPAREN && arithmOps(b):
		p.tok = p.arithmToken(b)
	case q == RBRACK && b == ']':
		p.npos++
		p.tok = RBRACK
	case regOps(b):
		p.tok = p.regToken(b)
	default:
		p.advanceLitOther(q)
	}
}

func (p *parser) advanceLitOther(q Token) {
	bs := p.buf[:0]
	for {
		if p.npos >= len(p.src) {
			p.tok, p.val = LIT, string(bs)
			return
		}
		b := p.src[p.npos]
		switch {
		case b == '\\': // escaped byte follows
			if p.npos == len(p.src)-1 {
				p.npos++
				bs = append(bs, '\\')
				p.tok, p.val = LIT, string(bs)
				return
			}
			b = p.src[p.npos+1]
			p.npos += 2
			if b == '\n' {
				p.f.lines = append(p.f.lines, p.npos)
			} else {
				bs = append(bs, '\\', b)
			}
			continue
		case q == SQUOTE:
			switch b {
			case '\n':
				p.f.lines = append(p.f.lines, p.npos+1)
			case '\'':
				p.tok, p.val = LIT, string(bs)
				return
			}
		case b == '`', b == '$':
			p.tok, p.val = LIT, string(bs)
			return
		case q == RBRACE:
			if b == '}' || b == '"' {
				p.tok, p.val = LIT, string(bs)
				return
			}
		case q == LBRACE && paramOps(b), q == RBRACK && b == ']':
			p.tok, p.val = LIT, string(bs)
			return
		case q == QUO:
			if b == '/' || b == '}' {
				p.tok, p.val = LIT, string(bs)
				return
			}
		case wordBreak(b), regOps(b):
			p.tok, p.val = LIT, string(bs)
			return
		case q == DRPAREN && arithmOps(b):
			p.tok, p.val = LIT, string(bs)
			return
		}
		bs = append(bs, p.src[p.npos])
		p.npos++
	}
}

func (p *parser) advanceLitNone() {
	var i int
	tok := LIT
loop:
	for i = p.npos; i < len(p.src); i++ {
		switch p.src[i] {
		case '\\': // escaped byte follows
			if i == len(p.src)-1 {
				break
			}
			i++
			if p.src[i] == '\n' {
				p.f.lines = append(p.f.lines, i+1)
				bs := p.src[p.npos : i-1]
				p.npos = i + 1
				p.advanceLitNoneCont(bs)
				return
			}
		case ' ', '\t', '\n', '\r', '&', '>', '<', '|', ';', '(', ')', '`':
			tok = LITWORD
			break loop
		case '"', '\'', '$':
			break loop
		}
	}
	if i == len(p.src) {
		tok = LITWORD
	}
	p.tok, p.val = tok, string(p.src[p.npos:i])
	p.npos = i
}

func (p *parser) advanceLitNoneCont(bs []byte) {
	for {
		if p.npos >= len(p.src) {
			p.tok, p.val = LITWORD, string(bs)
			return
		}
		switch p.src[p.npos] {
		case '\\': // escaped byte follows
			if p.npos == len(p.src)-1 {
				p.npos++
				bs = append(bs, '\\')
				p.tok, p.val = LIT, string(bs)
				return
			}
			b := p.src[p.npos+1]
			p.npos += 2
			if b == '\n' {
				p.f.lines = append(p.f.lines, p.npos)
			} else {
				bs = append(bs, '\\', b)
			}
		case ' ', '\t', '\n', '\r', '&', '>', '<', '|', ';', '(', ')', '`':
			p.tok, p.val = LITWORD, string(bs)
			return
		case '"', '\'', '$':
			p.tok, p.val = LIT, string(bs)
			return
		default:
			bs = append(bs, p.src[p.npos])
			p.npos++
		}
	}
}

func (p *parser) advanceLitDquote() {
	var i int
loop:
	for i = p.npos; i < len(p.src); i++ {
		switch p.src[i] {
		case '\\': // escaped byte follows
			i++
			if len(p.src) > i && p.src[i] == '\n' {
				p.f.lines = append(p.f.lines, i+1)
			}
		case '`', '"', '$':
			break loop
		case '\n':
			p.f.lines = append(p.f.lines, i+1)
		}
	}
	p.tok, p.val = LIT, string(p.src[p.npos:i])
	p.npos = i
}

func (p *parser) readUntil(b byte) ([]byte, bool) {
	rem := p.src[p.npos:]
	i := bytes.IndexByte(rem, b)
	if i < 0 {
		bs := rem
		return bs, false
	}
	return rem[:i], true
}

func (p *parser) doHeredocs() {
	for _, r := range p.heredocs {
		end := unquotedWordStr(p.f, &r.Word)
		l := &Lit{ValuePos: Pos(p.npos + 1)}
		l.Value = p.readHdocBody(end, r.Op == DHEREDOC)
		r.Hdoc = Word{Parts: []WordPart{l}}
	}
	p.heredocs = nil
}

func (p *parser) readHdocBody(end string, noTabs bool) string {
	var buf bytes.Buffer
	for p.npos < len(p.src) {
		bs, found := p.readUntil('\n')
		p.npos += len(bs) + 1
		if found {
			p.f.lines = append(p.f.lines, p.npos)
		}
		line := string(bs)
		if line == end || (noTabs && strings.TrimLeft(line, "\t") == end) {
			// add trailing tabs
			buf.Write(bs[:len(bs)-len(end)])
			break
		}
		buf.Write(bs)
		if found {
			buf.WriteByte('\n')
		}
	}
	return buf.String()
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
		p.tok = EOF
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
		case DSEMICOLON, SEMIFALL, DSEMIFALL:
			if q == DSEMICOLON {
				return
			}
			p.curErr("%s can only be used in a case clause", p.tok)
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

func (p *parser) getWord() (w Word) {
	if p.tok == LITWORD {
		w.Parts = append(w.Parts, &Lit{ValuePos: p.pos, Value: p.val})
		p.next()
	} else {
		w.Parts = p.wordParts()
	}
	return
}

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
		old := p.quote
		p.quote = DRPAREN
		p.next()
		ar.X = p.arithmExpr(DOLLDP, ar.Dollar, 0, false)
		ar.Rparen = p.arithmEnd(ar.Dollar, old)
		return ar
	case DOLLPR:
		cs := &CmdSubst{Left: p.pos}
		old := p.quote
		p.quote = RPAREN
		p.next()
		cs.Stmts = p.stmts()
		p.quote = old
		cs.Right = p.matched(cs.Left, LPAREN, RPAREN)
		return cs
	case DOLLAR:
		var b byte
		if p.npos >= len(p.src) {
			p.errPass(io.EOF)
		} else {
			b = p.src[p.npos]
		}
		if p.tok == EOF || wordBreak(b) || b == '"' {
			l := &Lit{ValuePos: p.pos, Value: "$"}
			p.next()
			return l
		}
		pe := &ParamExp{Dollar: p.pos, Short: true}
		if b == '#' || b == '$' || b == '?' {
			p.npos++
			p.pos++
			p.tok, p.val = LIT, string(b)
		} else {
			p.next()
		}
		p.gotLit(&pe.Param)
		return pe
	case CMDIN, CMDOUT:
		ps := &ProcSubst{Op: p.tok, OpPos: p.pos}
		old := p.quote
		p.quote = RPAREN
		p.next()
		ps.Stmts = p.stmts()
		p.quote = old
		ps.Rparen = p.matched(ps.OpPos, ps.Op, RPAREN)
		return ps
	case SQUOTE:
		sq := &SglQuoted{Quote: p.pos}
		bs, found := p.readUntil('\'')
		rem := bs
		for {
			i := bytes.IndexByte(rem, '\n')
			if i < 0 {
				p.npos += len(rem)
				break
			}
			p.npos += i + 1
			p.f.lines = append(p.f.lines, p.npos)
			rem = rem[i+1:]
		}
		p.npos++
		if !found {
			p.posErr(sq.Pos(), `reached EOF without closing quote %s`, SQUOTE)
		}
		sq.Value = string(bs)
		p.next()
		return sq
	case DOLLSQ, DQUOTE, DOLLDQ:
		q := &Quoted{Quote: p.tok, QuotePos: p.pos}
		stop := quotedStop(q.Quote)
		old := p.quote
		p.quote = stop
		p.next()
		q.Parts = p.wordParts()
		p.quote = old
		if !p.got(stop) {
			p.quoteErr(q.Pos(), stop)
		}
		return q
	case BQUOTE:
		cs := &CmdSubst{Backquotes: true, Left: p.pos}
		old := p.quote
		p.quote = BQUOTE
		p.next()
		cs.Stmts = p.stmts()
		p.quote = old
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

func arithmOpLevel(tok Token) int {
	switch tok {
	case COMMA:
		return 0
	case ADDASSGN, SUBASSGN, MULASSGN, QUOASSGN, REMASSGN,
		ANDASSGN, ORASSGN, XORASSGN, SHLASSGN, SHRASSGN:
		return 1
	case ASSIGN:
		return 2
	case QUEST, COLON:
		return 3
	case LOR:
		return 4
	case LAND:
		return 5
	case AND, OR, XOR:
		return 5
	case EQL, NEQ:
		return 6
	case LSS, GTR, LEQ, GEQ:
		return 7
	case SHL, SHR:
		return 8
	case ADD, SUB:
		return 9
	case MUL, QUO, REM:
		return 10
	case POW:
		return 11
	}
	return -1
}

func (p *parser) arithmExpr(ftok Token, fpos Pos, level int, compact bool) ArithmExpr {
	if p.tok == EOF || p.peekArithmEnd() {
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
	if p.tok == LIT || p.tok == LITWORD {
		p.curErr("not a valid arithmetic operator: %s", p.val)
	}
	newLevel := arithmOpLevel(p.tok)
	if newLevel < 0 || newLevel < level {
		return left
	}
	b := &BinaryExpr{
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
	if p.tok == INC || p.tok == DEC || p.tok == NOT {
		pre := &UnaryExpr{OpPos: p.pos, Op: p.tok}
		p.next()
		pre.X = p.arithmExprBase(pre.Op, pre.OpPos, compact)
		return pre
	}
	var x ArithmExpr
	switch p.tok {
	case LPAREN:
		pe := &ParenExpr{Lparen: p.pos}
		p.next()
		if pe.X = p.arithmExpr(LPAREN, pe.Lparen, 0, false); pe.X == nil {
			p.posErr(pe.Lparen, "parentheses must enclose an expression")
		}
		pe.Rparen = p.matched(pe.Lparen, LPAREN, RPAREN)
		x = pe
	case ADD, SUB:
		ue := &UnaryExpr{OpPos: p.pos, Op: p.tok}
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
	l.ValuePos = p.pos
	switch p.tok {
	case LIT, LITWORD:
		l.Value = p.val
	case DOLLAR:
		l.Value = "$"
	case QUEST:
		l.Value = "?"
	default:
		return false
	}
	p.next()
	return true
}

func (p *parser) paramExp() *ParamExp {
	pe := &ParamExp{Dollar: p.pos}
	old := p.quote
	p.quote = LBRACE
	p.next()
	pe.Length = p.got(HASH)
	if !p.gotParamLit(&pe.Param) && !pe.Length {
		p.posErr(pe.Dollar, "parameter expansion requires a literal")
	}
	if p.tok == RBRACE {
		p.quote = old
		p.next()
		return pe
	}
	if p.tok == LBRACK {
		lpos := p.pos
		p.quote = RBRACK
		p.next()
		pe.Ind = &Index{Word: p.getWord()}
		p.quote = LBRACE
		p.matched(lpos, LBRACK, RBRACK)
	}
	if p.tok == RBRACE {
		p.quote = old
		p.next()
		return pe
	}
	if pe.Length {
		p.curErr(`can only get length of a simple parameter`)
	}
	if p.tok == QUO || p.tok == DQUO {
		pe.Repl = &Replace{All: p.tok == DQUO}
		p.quote = QUO
		p.next()
		pe.Repl.Orig = p.getWord()
		if p.tok == QUO {
			p.quote = RBRACE
			p.next()
			pe.Repl.With = p.getWord()
		}
	} else {
		pe.Exp = &Expansion{Op: p.tok}
		p.quote = RBRACE
		p.next()
		pe.Exp.Word = p.getWord()
	}
	p.quote = old
	p.matched(pe.Dollar, DOLLBR, RBRACE)
	return pe
}

func (p *parser) peekArithmEnd() bool {
	return p.tok == RPAREN && p.npos < len(p.src) && p.src[p.npos] == ')'
}

func (p *parser) arithmEnd(left Pos, old Token) Pos {
	if p.peekArithmEnd() {
		p.npos++
	} else {
		p.matchingErr(left, DLPAREN, DRPAREN)
	}
	p.quote = old
	pos := p.pos
	p.next()
	return pos
}

func stopToken(tok Token) bool {
	return tok == EOF || tok == SEMICOLON || tok == AND || tok == OR ||
		tok == LAND || tok == LOR || tok == PIPEALL ||
		tok == DSEMICOLON || tok == SEMIFALL || tok == DSEMIFALL
}

var identRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func (p *parser) getAssign() (*Assign, bool) {
	i := strings.Index(p.val, "=")
	if i <= 0 {
		return nil, false
	}
	if p.val[i-1] == '+' {
		i--
	}
	if !identRe.MatchString(p.val[:i]) {
		return nil, false
	}
	as := &Assign{}
	as.Name = &Lit{ValuePos: p.pos, Value: p.val[:i]}
	if p.val[i] == '+' {
		as.Append = true
		i++
	}
	start := &Lit{ValuePos: p.pos + 1, Value: p.val[i+1:]}
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
	} else if !p.newLine && !stopToken(p.tok) {
		if w := p.getWord(); start.Value == "" {
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
		return p.npos < len(p.src) && (p.src[p.npos] == '>' || p.src[p.npos] == '<')
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
			} else if p.npos < len(p.src) && (p.src[p.npos] == '>' || p.src[p.npos] == '<') {
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
		switch {
		case p.newLine, p.tok == EOF:
			return
		case p.tok == SEMICOLON:
			p.next()
			gotEnd = true
			return
		}
	}
	if s = p.gotStmtPipe(s); s == nil {
		return
	}
	switch p.tok {
	case LAND, LOR:
		b := &BinaryCmd{OpPos: p.pos, Op: p.tok, X: s}
		p.next()
		p.got(STOPPED)
		if b.Y, _ = p.getStmt(false); b.Y == nil {
			p.followErr(b.OpPos, b.Op.String(), "a statement")
		}
		s = &Stmt{Position: s.Position, Cmd: b}
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
		b := &BinaryCmd{OpPos: p.pos, Op: p.tok, X: s}
		p.next()
		p.got(STOPPED)
		if b.Y = p.gotStmtPipe(&Stmt{Position: p.pos}); b.Y == nil {
			p.followErr(b.OpPos, b.Op.String(), "a statement")
		}
		s = &Stmt{Position: s.Position, Cmd: b}
	}
	return s
}

func (p *parser) subshell() *Subshell {
	s := &Subshell{Lparen: p.pos}
	old := p.quote
	p.quote = RPAREN
	p.next()
	s.Stmts = p.stmts()
	p.quote = old
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
	if p.tok == LPAREN && p.npos < len(p.src) && p.src[p.npos] == '(' {
		p.npos++
		c := &CStyleCond{Lparen: p.pos}
		old := p.quote
		p.quote = DRPAREN
		p.next()
		c.X = p.arithmExpr(DLPAREN, c.Lparen, 0, false)
		c.Rparen = p.arithmEnd(c.Lparen, old)
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
	if p.tok == LPAREN && p.npos < len(p.src) && p.src[p.npos] == '(' {
		p.npos++
		cl := &CStyleLoop{Lparen: p.pos}
		old := p.quote
		p.quote = DRPAREN
		p.next()
		cl.Init = p.arithmExpr(DLPAREN, cl.Lparen, 0, false)
		scPos := p.pos
		p.follow(p.pos, "expression", SEMICOLON)
		cl.Cond = p.arithmExpr(SEMICOLON, scPos, 0, false)
		scPos = p.pos
		p.follow(p.pos, "expression", SEMICOLON)
		cl.Post = p.arithmExpr(SEMICOLON, scPos, 0, false)
		cl.Rparen = p.arithmEnd(cl.Lparen, old)
		p.gotSameLine(SEMICOLON)
		return cl
	}
	wi := &WordIter{}
	if !p.gotLit(&wi.Name) {
		p.followErr(forPos, "for", "a literal")
	}
	if p.gotRsrv("in") {
		for !p.newLine && p.tok != EOF && p.tok != SEMICOLON {
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
		old := p.quote
		p.quote = DSEMICOLON
		p.next()
		pl.Stmts = p.stmts("esac")
		p.quote = old
		pl.OpPos = p.pos
		if p.tok != DSEMICOLON && p.tok != SEMIFALL && p.tok != DSEMIFALL {
			pl.Op = DSEMICOLON
			pls = append(pls, pl)
			break
		}
		pl.Op = p.tok
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
	for !p.newLine && !stopToken(p.tok) {
		if as, ok := p.getAssign(); ok {
			ds.Assigns = append(ds.Assigns, as)
		} else if w, ok := p.gotWord(); !ok {
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
	old := p.quote
	p.quote = DRPAREN
	p.next()
	p.stopNewline = true
	for !p.newLine && !stopToken(p.tok) && p.tok != STOPPED {
		x := p.arithmExpr(LET, lc.Let, 0, true)
		if x == nil {
			p.followErr(p.pos, "let", "arithmetic expressions")
		}
		lc.Exprs = append(lc.Exprs, x)
	}
	if len(lc.Exprs) == 0 {
		p.posErr(lc.Let, "let clause requires at least one expression")
	}
	p.stopNewline = false
	p.quote = old
	p.got(STOPPED)
	return lc
}

func (p *parser) bashFuncDecl() *FuncDecl {
	fpos := p.pos
	p.next()
	if p.tok != LITWORD {
		if w := p.followWord("function", fpos); p.err == nil {
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
	for !p.newLine {
		switch p.tok {
		case EOF, SEMICOLON, AND, OR, LAND, LOR, PIPEALL, p.quote, DSEMICOLON, SEMIFALL, DSEMIFALL:
			return ce
		case STOPPED:
			p.next()
		case LITWORD:
			if p.npos < len(p.src) && (p.src[p.npos] == '>' || p.src[p.npos] == '<') {
				p.doRedirect(s)
				continue
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
