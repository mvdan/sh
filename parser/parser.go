// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package parser

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
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
	p.f = &ast.File{
		Name:  name,
		Lines: make([]int, 1, 16),
	}
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

	spaced, newLine           bool
	stopNewline, forbidNested bool

	err error

	tok token.Token
	val string

	pos  token.Pos
	npos int

	quote quoteState

	// list of pending heredoc bodies
	heredocs []*ast.Redirect
	hdocStop []byte

	helperBuf *bytes.Buffer

	litBatch  []ast.Lit
	stmtBatch []ast.Stmt
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

func (p *parser) stmt(pos token.Pos) *ast.Stmt {
	if len(p.stmtBatch) == 0 {
		p.stmtBatch = make([]ast.Stmt, 16)
	}
	s := &p.stmtBatch[0]
	s.Position = pos
	p.stmtBatch = p.stmtBatch[1:]
	return s
}

type quoteState int

const (
	noState quoteState = 1 << iota
	subCmd
	subCmdBckquo
	sglQuotes
	dblQuotes
	hdocBody
	hdocBodyTabs
	arithmExpr
	arithmExprCmd
	arithmExprBrack
	testRegexp
	switchCase
	paramExpName
	paramExpInd
	paramExpRepl
	paramExpExp

	allRegTokens  = noState | subCmd | subCmdBckquo | switchCase
	allArithmExpr = arithmExpr | arithmExprCmd | arithmExprBrack
	allRbrack     = arithmExprBrack | paramExpInd
	allHdoc       = hdocBody | hdocBodyTabs
)

func (p *parser) bash() bool { return p.mode&PosixConformant == 0 }

func (p *parser) reset() {
	p.spaced, p.newLine = false, false
	p.stopNewline, p.forbidNested = false, false
	p.err = nil
	p.npos = 0
	p.tok, p.quote = token.ILLEGAL, noState
	p.heredocs = p.heredocs[:]
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
	hdocs := p.heredocs
	p.heredocs = p.heredocs[:0]
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
			r.Hdoc = ast.Word{Parts: p.wordParts()}
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
		if p.quote == arithmExpr {
			if err == io.EOF {
				p.tok = token.EOF
			} else {
				p.err = err
			}
			return
		}
		if err != io.EOF {
			p.err = err
		}
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
	p.got(token.STOPPED)
	if p.forbidNested {
		p.curErr("nested statements not allowed in this word")
	}
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
			sts = append(sts, s)
			gotEnd = end
		}
		p.got(token.STOPPED)
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
		w := ast.Word{Parts: []ast.WordPart{p.lit(p.pos, p.val)}}
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
		lastLit := p.tok == token.LIT
		n := p.wordPart()
		if n == nil {
			return
		}
		wps = append(wps, n)
		if p.spaced {
			return
		}
		if p.quote&allHdoc != 0 && p.hdocStop == nil {
			// TODO: is this is a hack around a bug?
			if p.tok == token.LIT && !lastLit {
				wps = append(wps, p.lit(p.pos, p.val))
			}
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
		oldQuote := p.quote
		if ar.Token == token.DOLLBK {
			// treat deprecated $[ as $((
			ar.Token = token.DOLLDP
			p.quote = arithmExprBrack
		} else {
			p.quote = arithmExpr
		}
		if p.err != nil {
			return nil
		}
		p.next()
		ar.X = p.arithmExpr(ar.Token, ar.Left, 0, false)
		hasEnd := p.peekArithmEnd(p.tok)
		oldTok := p.tok
		oldErr := p.err
		if p.quote == arithmExpr && !hasEnd {
			// TODO: this will probably break if there is
			// extra lingering state, such as pending
			// heredocs
			p.quote = oldQuote
			p.err = nil
			p.tok, p.pos = token.DOLLPR, ar.Left
			p.npos = int(ar.Left) + 1
			wp := p.wordPart()
			if p.err != nil {
				if oldErr != nil {
					// if retrying fails, report the
					// arithmetic expr error as that's got
					// higher precedence
					p.err = oldErr
				} else if !hasEnd {
					// if retrying fails and the
					// arithmetic expression wasn't
					// closed, report that properly
					p.err = nil
					p.tok = oldTok
					goto arithmClose
				}
			}
			return wp
		}
		if oldErr != nil {
			// not retrying, so recover error
			p.err = oldErr
			p.tok = token.EOF
		}
	arithmClose:
		if left == token.DOLLBK {
			if p.tok != token.RBRACK {
				p.matchingErr(ar.Left, left, token.RBRACK)
			}
			p.quote = oldQuote
			ar.Right = p.pos
			p.next()
		} else {
			ar.Right = p.arithmEnd(left, ar.Left, oldQuote)
		}
		return ar
	case token.DOLLPR:
		cs := &ast.CmdSubst{Left: p.pos}
		old := p.quote
		p.quote = subCmd
		p.next()
		cs.Stmts = p.stmts()
		p.quote = old
		cs.Right = p.matched(cs.Left, token.LPAREN, token.RPAREN)
		return cs
	case token.DOLLAR:
		var b byte
		if p.npos >= len(p.src) {
			p.errPass(io.EOF)
		} else {
			b = p.src[p.npos]
		}
		if p.tok == token.EOF || wordBreak(b) || b == '"' || b == '\'' || b == '`' {
			l := p.lit(p.pos, "$")
			p.next()
			return l
		}
		pe := &ast.ParamExp{Dollar: p.pos, Short: true}
		if b == '#' || b == '$' || b == '?' {
			p.npos++
			p.pos++
			p.tok, p.val = token.LIT, string(b)
		} else {
			old := p.quote
			if p.quote&allHdoc != 0 {
				p.quote = noState
			}
			p.next()
			p.quote = old
		}
		p.gotLit(&pe.Param)
		return pe
	case token.CMDIN, token.CMDOUT:
		ps := &ast.ProcSubst{Op: p.tok, OpPos: p.pos}
		old := p.quote
		p.quote = subCmd
		p.next()
		ps.Stmts = p.stmts()
		p.quote = old
		ps.Rparen = p.matched(ps.OpPos, ps.Op, token.RPAREN)
		return ps
	case token.SQUOTE:
		if p.quote == sglQuotes {
			return nil
		}
		sq := &ast.SglQuoted{Quote: p.pos}
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
	case token.DQUOTE:
		if p.quote == dblQuotes {
			return nil
		}
		fallthrough
	case token.DOLLSQ, token.DOLLDQ:
		q := &ast.Quoted{Quote: p.tok, QuotePos: p.pos}
		stop := q.Quote
		old := p.quote
		switch q.Quote {
		case token.DOLLSQ:
			stop = token.SQUOTE
			p.quote = sglQuotes
		case token.DOLLDQ:
			stop = token.DQUOTE
			p.quote = dblQuotes
		case token.SQUOTE:
			p.quote = sglQuotes
		case token.DQUOTE:
			p.quote = dblQuotes
		}
		p.next()
		q.Parts = p.wordParts()
		p.quote = old
		if !p.got(stop) {
			p.quoteErr(q.Pos(), stop)
		}
		return q
	case token.BQUOTE:
		if p.quote == subCmdBckquo {
			return nil
		}
		cs := &ast.CmdSubst{Backquotes: true, Left: p.pos}
		old := p.quote
		p.quote = subCmdBckquo
		p.next()
		cs.Stmts = p.stmts()
		p.quote = old
		cs.Right = p.pos
		if !p.got(token.BQUOTE) {
			p.quoteErr(cs.Pos(), token.BQUOTE)
		}
		return cs
	}
	return nil
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
	if p.tok == token.EOF || p.peekArithmEnd(p.tok) {
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
	if p.quote == arithmExpr && p.tok == token.SEMICOLON {
		p.curErr("not a valid arithmetic operator: %v", p.tok)
		newLevel = 0
	} else if p.tok == token.LIT || p.tok == token.LITWORD {
		p.curErr("not a valid arithmetic operator: %s", p.val)
		newLevel = 0
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
	if p.tok == token.INC || p.tok == token.DEC || p.tok == token.NOT {
		pre := &ast.UnaryExpr{OpPos: p.pos, Op: p.tok}
		p.next()
		pre.X = p.arithmExprBase(pre.Op, pre.OpPos, compact)
		return pre
	}
	var x ast.ArithmExpr
	switch p.tok {
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
	default:
		return false
	}
	p.next()
	return true
}

func (p *parser) paramExp() *ast.ParamExp {
	pe := &ast.ParamExp{Dollar: p.pos}
	old := p.quote
	p.quote = paramExpName
	p.next()
	pe.Length = p.got(token.HASH)
	if !p.gotParamLit(&pe.Param) && !pe.Length {
		p.posErr(pe.Dollar, "parameter expansion requires a literal")
	}
	if p.tok == token.RBRACE {
		p.quote = old
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
	if p.tok == token.RBRACE {
		p.quote = old
		p.next()
		return pe
	}
	if pe.Length {
		p.curErr(`can only get length of a simple parameter`)
	}
	if p.tok == token.QUO || p.tok == token.DQUO {
		pe.Repl = &ast.Replace{All: p.tok == token.DQUO}
		p.quote = paramExpRepl
		p.next()
		pe.Repl.Orig = p.word()
		if p.tok == token.QUO {
			p.quote = paramExpExp
			p.next()
			pe.Repl.With = p.word()
		}
	} else {
		pe.Exp = &ast.Expansion{Op: p.tok}
		p.quote = paramExpExp
		p.next()
		pe.Exp.Word = p.word()
	}
	p.quote = old
	p.matched(pe.Dollar, token.DOLLBR, token.RBRACE)
	return pe
}

func (p *parser) peekArithmEnd(tok token.Token) bool {
	return tok == token.RPAREN && p.npos < len(p.src) && p.src[p.npos] == ')'
}

func (p *parser) arithmEnd(ltok token.Token, lpos token.Pos, old quoteState) token.Pos {
	if p.peekArithmEnd(p.tok) {
		p.npos++
	} else {
		p.matchingErr(lpos, ltok, token.DRPAREN)
	}
	p.quote = old
	pos := p.pos
	p.next()
	return pos
}

func stopToken(tok token.Token) bool {
	return tok == token.EOF || tok == token.SEMICOLON || tok == token.AND ||
		tok == token.OR || tok == token.LAND || tok == token.LOR ||
		tok == token.PIPEALL || tok == token.DSEMICOLON ||
		tok == token.SEMIFALL || tok == token.DSEMIFALL
}

func validIdent(s string) bool {
	for i, c := range s {
		switch {
		case 'a' <= c && c <= 'z':
		case 'A' <= c && c <= 'Z':
		case c == '_':
		case i > 0 && '0' <= c && c <= '9':
		default:
			return false
		}
	}
	return true
}

func (p *parser) getAssign() (*ast.Assign, bool) {
	i := strings.Index(p.val, "=")
	if i <= 0 {
		return nil, false
	}
	if p.val[i-1] == '+' {
		i--
	}
	if !validIdent(p.val[:i]) {
		return nil, false
	}
	as := &ast.Assign{}
	as.Name = p.lit(p.pos, p.val[:i])
	if p.val[i] == '+' {
		as.Append = true
		i++
	}
	start := p.lit(p.pos + 1, p.val[i+1:])
	if start.Value != "" {
		start.ValuePos += token.Pos(i)
		as.Value.Parts = append(as.Value.Parts, start)
	}
	p.next()
	if p.spaced {
		return as, true
	}
	if start.Value == "" && p.tok == token.LPAREN {
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
		as.Value.Parts = append(as.Value.Parts, ae)
	} else if !p.newLine && !stopToken(p.tok) {
		if w := p.word(); start.Value == "" {
			as.Value = w
		} else {
			as.Value.Parts = append(as.Value.Parts, w.Parts...)
		}
	}
	return as, true
}

func (p *parser) peekRedir() bool {
	switch p.tok {
	case token.LITWORD:
		return p.npos < len(p.src) && (p.src[p.npos] == '>' || p.src[p.npos] == '<')
	case token.GTR, token.SHR, token.LSS, token.DPLIN, token.DPLOUT,
		token.RDRINOUT, token.SHL, token.DHEREDOC, token.WHEREDOC,
		token.RDRALL, token.APPALL:
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
		p.stopNewline = true
		p.forbidNested = true
		if p.newLine {
			p.curErr("heredoc stop word must be on the same line")
		}
		r.Word = p.followWordTok(r.Op, r.OpPos)
		p.forbidNested = false
		p.heredocs = append(p.heredocs, r)
		p.got(token.STOPPED)
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
			if as, ok := p.getAssign(); ok {
				s.Assigns = append(s.Assigns, as)
			} else if p.npos < len(p.src) && (p.src[p.npos] == '>' || p.src[p.npos] == '<') {
				p.doRedirect(s)
			} else {
				break preLoop
			}
		case token.GTR, token.SHR, token.LSS, token.DPLIN, token.DPLOUT,
			token.RDRINOUT, token.SHL, token.DHEREDOC,
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
		p.got(token.STOPPED)
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
		case p.bash() && (p.val == "declare" || p.val == "local"):
			s.Cmd = p.declClause()
		case p.bash() && p.val == "eval":
			s.Cmd = p.evalClause()
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
					Parts: []ast.WordPart{&name},
				})
			}
		}
	case token.LIT, token.DOLLBR, token.DOLLDP, token.DOLLPR, token.DOLLAR,
		token.CMDIN, token.CMDOUT, token.SQUOTE, token.DOLLSQ,
		token.DQUOTE, token.DOLLDQ, token.BQUOTE, token.DOLLBK:
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
		p.got(token.STOPPED)
		if b.Y = p.gotStmtPipe(&ast.Stmt{Position: p.pos}); b.Y == nil {
			p.followErr(b.OpPos, b.Op.String(), "a statement")
		}
		s = &ast.Stmt{Position: s.Position, Cmd: b}
	}
	return s
}

func (p *parser) subshell() *ast.Subshell {
	s := &ast.Subshell{Lparen: p.pos}
	old := p.quote
	p.quote = subCmd
	p.next()
	s.Stmts = p.stmts()
	p.quote = old
	s.Rparen = p.matched(s.Lparen, token.LPAREN, token.RPAREN)
	return s
}

func (p *parser) arithmExpCmd() *ast.ArithmExp {
	ar := &ast.ArithmExp{Token: p.tok, Left: p.pos}
	old := p.quote
	p.quote = arithmExprCmd
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
		old := p.quote
		p.quote = arithmExprCmd
		p.next()
		cl.Init = p.arithmExpr(token.DLPAREN, cl.Lparen, 0, false)
		scPos := p.pos
		p.follow(p.pos, "expression", token.SEMICOLON)
		cl.Cond = p.arithmExpr(token.SEMICOLON, scPos, 0, false)
		scPos = p.pos
		p.follow(p.pos, "expression", token.SEMICOLON)
		cl.Post = p.arithmExpr(token.SEMICOLON, scPos, 0, false)
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
		old := p.quote
		p.quote = switchCase
		p.next()
		pl.Stmts = p.stmts("esac")
		p.quote = old
		pl.OpPos = p.pos
		if p.tok != token.DSEMICOLON && p.tok != token.SEMIFALL && p.tok != token.DSEMIFALL {
			pl.Op = token.DSEMICOLON
			pls = append(pls, pl)
			break
		}
		pl.Op = p.tok
		p.next()
		pls = append(pls, pl)
		p.got(token.STOPPED)
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
		old := p.quote
		p.quote = testRegexp
		p.next()
		p.quote = old
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
	ds := &ast.DeclClause{Declare: p.pos, Local: p.val == "local"}
	p.next()
	for p.tok == token.LITWORD && p.val[0] == '-' {
		ds.Opts = append(ds.Opts, p.word())
	}
	for !p.newLine && !stopToken(p.tok) && !p.peekRedir() {
		if as, ok := p.getAssign(); ok {
			ds.Assigns = append(ds.Assigns, as)
		} else if w := p.word(); w.Parts == nil {
			p.followErr(p.pos, "declare", "words")
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

func (p *parser) letClause() *ast.LetClause {
	lc := &ast.LetClause{Let: p.pos}
	old := p.quote
	p.quote = arithmExprCmd
	p.next()
	p.stopNewline = true
	for !p.newLine && !stopToken(p.tok) && p.tok != token.STOPPED && !p.peekRedir() {
		x := p.arithmExpr(token.LET, lc.Let, 0, true)
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
	p.got(token.STOPPED)
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
	ce := &ast.CallExpr{Args: []ast.Word{w}}
	for !p.newLine {
		switch p.tok {
		case token.EOF, token.SEMICOLON, token.AND, token.OR,
			token.LAND, token.LOR, token.PIPEALL,
			token.DSEMICOLON, token.SEMIFALL, token.DSEMIFALL:
			return ce
		case token.STOPPED:
			p.next()
		case token.LITWORD:
			if p.npos < len(p.src) && (p.src[p.npos] == '>' || p.src[p.npos] == '<') {
				p.doRedirect(s)
				continue
			}
			ce.Args = append(ce.Args, ast.Word{
				Parts: []ast.WordPart{p.lit(p.pos, p.val)},
			})
			p.next()
		case token.BQUOTE:
			if p.quote == subCmdBckquo {
				return ce
			}
			fallthrough
		case token.LIT, token.DOLLBR, token.DOLLDP, token.DOLLPR,
			token.DOLLAR, token.CMDIN, token.CMDOUT, token.SQUOTE,
			token.DOLLSQ, token.DQUOTE, token.DOLLDQ, token.DOLLBK:
			ce.Args = append(ce.Args, ast.Word{Parts: p.wordParts()})
		case token.GTR, token.SHR, token.LSS, token.DPLIN, token.DPLOUT,
			token.RDRINOUT, token.SHL, token.DHEREDOC,
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
