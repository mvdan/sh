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

	"github.com/mvdan/sh/ast"
	"github.com/mvdan/sh/token"
)

// Mode controls the parser behaviour via a set of flags.
type Mode uint

const (
	ParseComments   Mode = 1 << iota // add comments to the AST
	PosixComformant                  // match the POSIX standard where it differs from bash
)

var parserFree = sync.Pool{
	New: func() interface{} { return &parser{} },
}

// Parse reads and parses a shell program with an optional name. It
// returns the parsed program if no issues were encountered. Otherwise,
// an error is returned.
func Parse(src []byte, name string, mode Mode) (*ast.File, error) {
	p := parserFree.Get().(*parser)
	*p = parser{
		f:            &ast.File{Name: name},
		src:          src,
		mode:         mode,
		helperBuf:    p.helperBuf,
		helperWriter: p.helperWriter,
	}
	p.f.Lines = make([]int, 1, 16)
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

	quote token.Token

	hdocStop string
	hdocTabs bool

	// list of pending heredoc bodies
	heredocs []*ast.Redirect

	helperBuf    *bytes.Buffer
	helperWriter *bufio.Writer
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
	if p.tok == token.EOF {
		return
	}
	if p.npos >= len(p.src) {
		p.errPass(io.EOF)
		return
	}
	b := p.src[p.npos]
	if p.quote == token.SHL && p.hdocStop == "" {
		return
	}
	if p.tok == token.STOPPED && b == '\n' {
		p.npos++
		p.f.Lines = append(p.f.Lines, p.npos)
		p.doHeredocs()
		if p.tok == token.EOF || p.npos >= len(p.src) {
			p.errPass(io.EOF)
			return
		}
		b = p.src[p.npos]
		p.spaced, p.newLine = true, true
	} else {
		p.spaced, p.newLine = false, false
	}
	q := p.quote
	p.pos = token.Pos(p.npos + 1)
	switch q {
	case token.QUO:
		switch b {
		case '}':
			p.npos++
			p.tok = token.RBRACE
		case '/':
			p.npos++
			p.tok = token.QUO
		case '`', '"', '$':
			p.tok = p.dqToken(b)
		default:
			p.advanceLitOther(q)
		}
		return
	case token.DQUOTE:
		if b == '`' || b == '"' || b == '$' {
			p.tok = p.dqToken(b)
		} else {
			p.advanceLitDquote()
		}
		return
	case token.SHL:
		if b == '`' || b == '$' {
			p.tok = p.dqToken(b)
		} else {
			p.advanceLitHdoc()
		}
		return
	case token.RBRACE:
		switch b {
		case '}':
			p.npos++
			p.tok = token.RBRACE
		case '`', '"', '$':
			p.tok = p.dqToken(b)
		default:
			p.advanceLitOther(q)
		}
		return
	case token.SQUOTE:
		if b == '\'' {
			p.npos++
			p.tok = token.SQUOTE
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
				p.tok = token.STOPPED
				return
			}
			p.spaced = true
			if p.npos < len(p.src) {
				p.npos++
			}
			p.f.Lines = append(p.f.Lines, p.npos)
			p.newLine = true
		case '\\':
			if p.npos < len(p.src)-1 && p.src[p.npos+1] == '\n' {
				p.npos += 2
				p.f.Lines = append(p.f.Lines, p.npos)
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
	p.pos = token.Pos(p.npos + 1)
	switch {
	case q == token.ILLEGAL, q == token.RPAREN, q == token.BQUOTE, q == token.DSEMICOLON:
		switch b {
		case ';', '"', '\'', '(', ')', '$', '|', '&', '>', '<', '`':
			p.tok = p.regToken(b)
		case '#':
			p.npos++
			bs, _ := p.readUntil('\n')
			p.npos += len(bs)
			if p.mode&ParseComments > 0 {
				p.f.Comments = append(p.f.Comments, &ast.Comment{
					Hash: p.pos,
					Text: string(bs),
				})
			}
			p.next()
		default:
			p.advanceLitNone()
		}
	case q == token.LBRACE && paramOps(b):
		p.tok = p.paramToken(b)
	case q == token.DRPAREN && arithmOps(b):
		p.tok = p.arithmToken(b)
	case q == token.RBRACK && b == ']':
		p.npos++
		p.tok = token.RBRACK
	case regOps(b):
		p.tok = p.regToken(b)
	default:
		p.advanceLitOther(q)
	}
}

func (p *parser) advanceLitOther(q token.Token) {
	bs := make([]byte, 0, 8)
	for {
		if p.npos >= len(p.src) {
			p.tok, p.val = token.LIT, string(bs)
			return
		}
		b := p.src[p.npos]
		switch {
		case b == '\\': // escaped byte follows
			if p.npos == len(p.src)-1 {
				p.npos++
				bs = append(bs, '\\')
				p.tok, p.val = token.LIT, string(bs)
				return
			}
			b = p.src[p.npos+1]
			p.npos += 2
			if b == '\n' {
				p.f.Lines = append(p.f.Lines, p.npos)
			} else {
				bs = append(bs, '\\', b)
			}
			continue
		case q == token.SQUOTE:
			switch b {
			case '\n':
				p.f.Lines = append(p.f.Lines, p.npos+1)
			case '\'':
				p.tok, p.val = token.LIT, string(bs)
				return
			}
		case b == '`', b == '$':
			p.tok, p.val = token.LIT, string(bs)
			return
		case q == token.RBRACE:
			if b == '}' || b == '"' {
				p.tok, p.val = token.LIT, string(bs)
				return
			}
		case q == token.QUO:
			if b == '/' || b == '}' {
				p.tok, p.val = token.LIT, string(bs)
				return
			}
		case wordBreak(b), regOps(b), q == token.DRPAREN && arithmOps(b),
			q == token.LBRACE && paramOps(b), q == token.RBRACK && b == ']':
			p.tok, p.val = token.LIT, string(bs)
			return
		}
		bs = append(bs, p.src[p.npos])
		p.npos++
	}
}

func (p *parser) advanceLitNone() {
	i, tok := 0, token.LIT
loop:
	for i = p.npos; i < len(p.src); i++ {
		switch p.src[i] {
		case '\\': // escaped byte follows
			if i == len(p.src)-1 {
				break
			}
			i++
			if p.src[i] == '\n' {
				p.f.Lines = append(p.f.Lines, i+1)
				bs := p.src[p.npos : i-1]
				p.npos = i + 1
				p.advanceLitNoneCont(bs)
				return
			}
		case ' ', '\t', '\n', '\r', '&', '>', '<', '|', ';', '(', ')':
			tok = token.LITWORD
			break loop
		case '`':
			if p.quote == token.BQUOTE {
				tok = token.LITWORD
			}
			break loop
		case '"', '\'', '$':
			break loop
		}
	}
	if i == len(p.src) {
		tok = token.LITWORD
	}
	p.tok, p.val = tok, string(p.src[p.npos:i])
	p.npos = i
}

func (p *parser) advanceLitNoneCont(bs []byte) {
	for {
		if p.npos >= len(p.src) {
			p.tok, p.val = token.LITWORD, string(bs)
			return
		}
		switch p.src[p.npos] {
		case '\\': // escaped byte follows
			if p.npos == len(p.src)-1 {
				p.npos++
				bs = append(bs, '\\')
				p.tok, p.val = token.LIT, string(bs)
				return
			}
			b := p.src[p.npos+1]
			p.npos += 2
			if b == '\n' {
				p.f.Lines = append(p.f.Lines, p.npos)
			} else {
				bs = append(bs, '\\', b)
			}
		case ' ', '\t', '\n', '\r', '&', '>', '<', '|', ';', '(', ')':
			p.tok, p.val = token.LITWORD, string(bs)
			return
		case '`':
			if p.quote == token.BQUOTE {
				p.tok, p.val = token.LITWORD, string(bs)
			} else {
				p.tok, p.val = token.LIT, string(bs)
			}
			return
		case '"', '\'', '$':
			p.tok, p.val = token.LIT, string(bs)
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
				p.f.Lines = append(p.f.Lines, i+1)
			}
		case '`', '"', '$':
			break loop
		case '\n':
			p.f.Lines = append(p.f.Lines, i+1)
		}
	}
	p.tok, p.val = token.LIT, string(p.src[p.npos:i])
	p.npos = i
}

func (p *parser) advanceLitHdoc() {
	end := []byte(p.hdocStop)
	isEnd := func(i int) bool {
		if len(p.src) < i+len(end) {
			return false
		}
		if !bytes.Equal(end, p.src[i:i+len(end)]) {
			return false
		}
		return len(p.src) == i+len(end) || p.src[i+len(end)] == '\n'
	}
	n := p.npos
	for p.hdocTabs && n < len(p.src) && p.src[n] == '\t' {
		n++
	}
	if isEnd(n) {
		if n == p.npos {
			p.tok = token.ILLEGAL
		} else {
			p.tok, p.val = token.LIT, string(p.src[p.npos:n])
		}
		p.npos = n + len(end)
		p.hdocStop = ""
		return
	}
	var i int
loop:
	for i = p.npos; i < len(p.src); i++ {
		switch p.src[i] {
		case '\\': // escaped byte follows
			i++
			if i == len(p.src) {
				break loop
			}
			if p.src[i] == '\n' {
				p.f.Lines = append(p.f.Lines, i+1)
			}
		case '`', '$':
			break loop
		case '\n':
			n := i + 1
			p.f.Lines = append(p.f.Lines, n)
			for p.hdocTabs && n < len(p.src) && p.src[n] == '\t' {
				n++
			}
			if isEnd(n) {
				p.tok, p.val = token.LIT, string(p.src[p.npos:n])
				p.npos = n + len(end)
				p.hdocStop = ""
				return
			}
		}
	}
	p.tok, p.val = token.LIT, string(p.src[p.npos:i])
	p.npos = i
}

func (p *parser) readUntil(b byte) ([]byte, bool) {
	rem := p.src[p.npos:]
	if i := bytes.IndexByte(rem, b); i >= 0 {
		return rem[:i], true
	}
	return rem, false
}

func (p *parser) wordStr(w ast.Word) string {
	if p.helperWriter == nil {
		p.helperBuf = new(bytes.Buffer)
		p.helperWriter = bufio.NewWriter(p.helperBuf)
	} else {
		p.helperWriter.Reset(p.helperBuf)
		p.helperBuf.Reset()
	}
	pr := printer{Writer: p.helperWriter, f: p.f}
	pr.word(w)
	p.helperWriter.Flush()
	return p.helperBuf.String()
}

func (p *parser) unquotedWordStr(w *ast.Word) string {
	if p.helperWriter == nil {
		p.helperBuf = new(bytes.Buffer)
		p.helperWriter = bufio.NewWriter(p.helperBuf)
	} else {
		p.helperWriter.Reset(p.helperBuf)
		p.helperBuf.Reset()
	}
	pr := printer{Writer: p.helperWriter, f: p.f}
	pr.unquotedWord(w)
	p.helperWriter.Flush()
	return p.helperBuf.String()
}

func (p *parser) doHeredocs() {
	old := p.quote
	p.quote = token.SHL
	hdocs := p.heredocs
	p.heredocs = p.heredocs[:0]
	for _, r := range hdocs {
		p.hdocTabs = r.Op == token.DHEREDOC
		p.hdocStop = p.unquotedWordStr(&r.Word)
		if p.npos < len(p.src) && p.src[p.npos] == '\n' {
			p.npos++
			p.f.Lines = append(p.f.Lines, p.npos)
		}
		p.next()
		r.Hdoc = p.getWord()
	}
	p.quote = old
}

func wordBreak(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n' ||
		b == '&' || b == '>' || b == '<' || b == '|' ||
		b == ';' || b == '(' || b == ')'
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
		p.followErr(lpos, left, fmt.Sprintf(`%q`, tok))
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
	w, ok := p.gotWord()
	if !ok {
		p.followErr(pos, tok.String(), "a word")
	}
	return w
}

func (p *parser) followWord(s string, pos token.Pos) ast.Word {
	w, ok := p.gotWord()
	if !ok {
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
	p.posErr(lpos, `reached %s without matching token %s with %s`,
		p.tok, left, right)
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
		case q:
			return
		case token.DSEMICOLON, token.SEMIFALL, token.DSEMIFALL:
			if q == token.DSEMICOLON {
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

func (p *parser) getWord() ast.Word {
	if p.tok == token.LITWORD {
		w := ast.Word{Parts: []ast.WordPart{
			&ast.Lit{ValuePos: p.pos, Value: p.val},
		}}
		p.next()
		return w
	}
	return ast.Word{Parts: p.wordParts()}
}

func (p *parser) gotWord() (ast.Word, bool) {
	w := p.getWord()
	return w, len(w.Parts) > 0
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
		if p.quote == token.SHL && p.hdocStop == "" {
			// TODO: is this is a hack around a bug?
			if p.tok == token.LIT && !lastLit {
				wps = append(wps, &ast.Lit{
					ValuePos: p.pos,
					Value:    p.val,
				})
			}
			return
		}
	}
}

func (p *parser) wordPart() ast.WordPart {
	switch p.tok {
	case token.LIT, token.LITWORD:
		l := &ast.Lit{ValuePos: p.pos, Value: p.val}
		p.next()
		return l
	case p.quote:
		return nil
	case token.DOLLBR:
		return p.paramExp()
	case token.DOLLDP, token.DLPAREN:
		ar := &ast.ArithmExp{Token: p.tok, Left: p.pos}
		old := p.quote
		p.quote = token.DRPAREN
		p.next()
		ar.X = p.arithmExpr(token.DOLLDP, ar.Left, 0, false)
		ar.Right = p.arithmEnd(ar.Left, old)
		return ar
	case token.DOLLPR:
		cs := &ast.CmdSubst{Left: p.pos}
		old := p.quote
		p.quote = token.RPAREN
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
		if p.tok == token.EOF || wordBreak(b) || b == '"' || b == '`' {
			l := &ast.Lit{ValuePos: p.pos, Value: "$"}
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
			if p.quote == token.SHL {
				p.quote = token.ILLEGAL
			}
			p.next()
			p.quote = old
		}
		p.gotLit(&pe.Param)
		return pe
	case token.CMDIN, token.CMDOUT:
		ps := &ast.ProcSubst{Op: p.tok, OpPos: p.pos}
		old := p.quote
		p.quote = token.RPAREN
		p.next()
		ps.Stmts = p.stmts()
		p.quote = old
		ps.Rparen = p.matched(ps.OpPos, ps.Op, token.RPAREN)
		return ps
	case token.SQUOTE:
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
	case token.DOLLSQ, token.DQUOTE, token.DOLLDQ:
		q := &ast.Quoted{Quote: p.tok, QuotePos: p.pos}
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
	case token.BQUOTE:
		cs := &ast.CmdSubst{Backquotes: true, Left: p.pos}
		old := p.quote
		p.quote = token.BQUOTE
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

func quotedStop(start token.Token) token.Token {
	switch start {
	case token.DOLLSQ:
		return token.SQUOTE
	case token.DOLLDQ:
		return token.DQUOTE
	}
	return start
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
	if p.tok == token.LIT || p.tok == token.LITWORD {
		p.curErr("not a valid arithmetic operator: %s", p.val)
	}
	newLevel := arithmOpLevel(p.tok)
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
	p.quote = token.LBRACE
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
		p.quote = token.RBRACK
		p.next()
		pe.Ind = &ast.Index{Word: p.getWord()}
		p.quote = token.LBRACE
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
		p.quote = token.QUO
		p.next()
		pe.Repl.Orig = p.getWord()
		if p.tok == token.QUO {
			p.quote = token.RBRACE
			p.next()
			pe.Repl.With = p.getWord()
		}
	} else {
		pe.Exp = &ast.Expansion{Op: p.tok}
		p.quote = token.RBRACE
		p.next()
		pe.Exp.Word = p.getWord()
	}
	p.quote = old
	p.matched(pe.Dollar, token.DOLLBR, token.RBRACE)
	return pe
}

func (p *parser) peekArithmEnd() bool {
	return p.tok == token.RPAREN && p.npos < len(p.src) && p.src[p.npos] == ')'
}

func (p *parser) arithmEnd(left token.Pos, old token.Token) token.Pos {
	if p.peekArithmEnd() {
		p.npos++
	} else {
		p.matchingErr(left, token.DLPAREN, token.DRPAREN)
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

var identRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func (p *parser) getAssign() (*ast.Assign, bool) {
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
	as := &ast.Assign{}
	as.Name = &ast.Lit{ValuePos: p.pos, Value: p.val[:i]}
	if p.val[i] == '+' {
		as.Append = true
		i++
	}
	start := &ast.Lit{ValuePos: p.pos + 1, Value: p.val[i+1:]}
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
			if w, ok := p.gotWord(); !ok {
				p.curErr("array elements must be words")
			} else {
				ae.List = append(ae.List, w)
			}
		}
		ae.Rparen = p.matched(ae.Lparen, token.LPAREN, token.RPAREN)
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
		r.Word = p.followWordTok(r.Op, r.OpPos)
		p.forbidNested = false
		p.heredocs = append(p.heredocs, r)
		p.got(token.STOPPED)
	default:
		r.Word = p.followWordTok(r.Op, r.OpPos)
	}
	s.Redirs = append(s.Redirs, r)
}

func (p *parser) getStmt(readEnd bool) (s *ast.Stmt, gotEnd bool) {
	s = &ast.Stmt{Position: p.pos}
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
			p.next()
			gotEnd = true
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
		s = &ast.Stmt{Position: s.Position, Cmd: b}
	case token.AND:
		p.next()
		s.Background = true
		gotEnd = true
	}
	if readEnd && p.gotSameLine(token.SEMICOLON) {
		gotEnd = true
	}
	return
}

func (p *parser) gotStmtPipe(s *ast.Stmt) *ast.Stmt {
	switch p.tok {
	case token.LPAREN:
		s.Cmd = p.subshell()
	case token.LITWORD:
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
			name := ast.Lit{ValuePos: p.pos, Value: p.val}
			w := p.getWord()
			if p.gotSameLine(token.LPAREN) {
				p.follow(name.ValuePos, "foo(", token.RPAREN)
				s.Cmd = p.funcDecl(name, name.ValuePos)
			} else {
				s.Cmd = p.callExpr(s, w)
			}
		}
	case token.LIT, token.DOLLBR, token.DOLLDP, token.DOLLPR, token.DOLLAR,
		token.CMDIN, token.CMDOUT, token.SQUOTE, token.DOLLSQ,
		token.DQUOTE, token.DOLLDQ, token.BQUOTE, token.DLPAREN:
		w := p.getWord()
		if p.gotSameLine(token.LPAREN) && p.err == nil {
			p.posErr(w.Pos(), "invalid func name: %s", p.wordStr(w))
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
	p.quote = token.RPAREN
	p.next()
	s.Stmts = p.stmts()
	p.quote = old
	s.Rparen = p.matched(s.Lparen, token.LPAREN, token.RPAREN)
	if len(s.Stmts) == 0 {
		p.posErr(s.Lparen, "a subshell must contain at least one statement")
	}
	return s
}

func (p *parser) block() *ast.Block {
	b := &ast.Block{Lbrace: p.pos}
	p.next()
	b.Stmts = p.stmts("}")
	b.Rbrace = p.pos
	if !p.gotRsrv("}") {
		p.posErr(b.Lbrace, `reached %s without matching word { with }`, p.tok)
	}
	return b
}

func (p *parser) ifClause() *ast.IfClause {
	ic := &ast.IfClause{If: p.pos}
	p.next()
	ic.Cond = p.cond("if", ic.If, "then")
	ic.Then = p.followRsrv(ic.If, "if [stmts]", "then")
	ic.ThenStmts = p.followStmts("then", ic.Then, "fi", "elif", "else")
	elifPos := p.pos
	for p.gotRsrv("elif") {
		elf := &ast.Elif{Elif: elifPos}
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

func (p *parser) cond(left string, lpos token.Pos, stop string) ast.Cond {
	if p.tok == token.DLPAREN {
		c := &ast.CStyleCond{Lparen: p.pos}
		old := p.quote
		p.quote = token.DRPAREN
		p.next()
		c.X = p.arithmExpr(token.DLPAREN, c.Lparen, 0, false)
		c.Rparen = p.arithmEnd(c.Lparen, old)
		p.gotSameLine(token.SEMICOLON)
		return c
	}
	stmts := p.followStmts(left, lpos, stop)
	if len(stmts) == 0 {
		return nil
	}
	return &ast.StmtCond{Stmts: stmts}
}

func (p *parser) whileClause() *ast.WhileClause {
	wc := &ast.WhileClause{While: p.pos}
	p.next()
	wc.Cond = p.cond("while", wc.While, "do")
	wc.Do = p.followRsrv(wc.While, "while [stmts]", "do")
	wc.DoStmts = p.followStmts("do", wc.Do, "done")
	wc.Done = p.stmtEnd(wc, "while", "done")
	return wc
}

func (p *parser) untilClause() *ast.UntilClause {
	uc := &ast.UntilClause{Until: p.pos}
	p.next()
	uc.Cond = p.cond("until", uc.Until, "do")
	uc.Do = p.followRsrv(uc.Until, "until [stmts]", "do")
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
		p.quote = token.DRPAREN
		p.next()
		cl.Init = p.arithmExpr(token.DLPAREN, cl.Lparen, 0, false)
		scPos := p.pos
		p.follow(p.pos, "expression", token.SEMICOLON)
		cl.Cond = p.arithmExpr(token.SEMICOLON, scPos, 0, false)
		scPos = p.pos
		p.follow(p.pos, "expression", token.SEMICOLON)
		cl.Post = p.arithmExpr(token.SEMICOLON, scPos, 0, false)
		cl.Rparen = p.arithmEnd(cl.Lparen, old)
		p.gotSameLine(token.SEMICOLON)
		return cl
	}
	wi := &ast.WordIter{}
	if !p.gotLit(&wi.Name) {
		p.followErr(forPos, "for", "a literal")
	}
	if p.gotRsrv("in") {
		for !p.newLine && p.tok != token.EOF && p.tok != token.SEMICOLON {
			if w, ok := p.gotWord(); !ok {
				p.curErr("word list can only contain words")
			} else {
				wi.List = append(wi.List, w)
			}
		}
		p.gotSameLine(token.SEMICOLON)
	} else if !p.gotSameLine(token.SEMICOLON) && !p.newLine {
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
	if p.gotSameLine(token.SEMICOLON) {
		return
	}
	for p.tok != token.EOF && !(p.tok == token.LITWORD && p.val == "esac") {
		pl := &ast.PatternList{}
		p.got(token.LPAREN)
		for p.tok != token.EOF {
			if w, ok := p.gotWord(); !ok {
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
		p.quote = token.DSEMICOLON
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
	}
	return
}

func (p *parser) declClause(local bool) *ast.DeclClause {
	ds := &ast.DeclClause{Declare: p.pos, Local: local}
	p.next()
	for p.tok == token.LITWORD && p.val[0] == '-' {
		ds.Opts = append(ds.Opts, p.getWord())
	}
	for !p.newLine && !stopToken(p.tok) {
		if as, ok := p.getAssign(); ok {
			ds.Assigns = append(ds.Assigns, as)
		} else if w, ok := p.gotWord(); !ok {
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
	p.quote = token.DRPAREN
	p.next()
	p.stopNewline = true
	for !p.newLine && !stopToken(p.tok) && p.tok != token.STOPPED {
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
			p.posErr(w.Pos(), "invalid func name: %s", p.wordStr(w))
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
			token.LAND, token.LOR, token.PIPEALL, p.quote,
			token.DSEMICOLON, token.SEMIFALL, token.DSEMIFALL:
			return ce
		case token.STOPPED:
			p.next()
		case token.LITWORD:
			if p.npos < len(p.src) && (p.src[p.npos] == '>' || p.src[p.npos] == '<') {
				p.doRedirect(s)
				continue
			}
			fallthrough
		case token.LIT, token.DOLLBR, token.DOLLDP, token.DOLLPR,
			token.DOLLAR, token.CMDIN, token.CMDOUT, token.SQUOTE,
			token.DOLLSQ, token.DQUOTE, token.DOLLDQ, token.BQUOTE:
			ce.Args = append(ce.Args, p.getWord())
		case token.GTR, token.SHR, token.LSS, token.DPLIN, token.DPLOUT,
			token.RDRINOUT, token.SHL, token.DHEREDOC,
			token.WHEREDOC, token.RDRALL, token.APPALL:
			p.doRedirect(s)
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
