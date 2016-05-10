// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"regexp"
)

// Parse reads and parses a shell program with an optional name. It
// returns the parsed program if no issues were encountered. Otherwise,
// an error is returned.
func Parse(r io.Reader, name string) (File, error) {
	p := &parser{
		br: bufio.NewReader(r),
		file: File{
			Name: name,
		},
		npos: Pos{
			Line:   1,
			Column: 1,
		},
		stops: [][]Token{nil},
	}
	p.next()
	p.stmts(&p.file.Stmts)
	return p.file, p.err
}

type parser struct {
	br *bufio.Reader

	file File
	err  error

	spaced, newLine, gotEnd bool
	arithmExp               bool

	ltok, tok Token
	lval, val string

	lpos, pos, npos Pos

	// stack of stop tokens
	stops [][]Token

	stopNewline bool
	heredocs    []Word
}

func (p *parser) curStops() []Token { return p.stops[len(p.stops)-1] }
func (p *parser) newStops(stops ...Token) {
	p.stops = append(p.stops, stops)
}
func (p *parser) addStops(stops ...Token) {
	p.newStops(append(p.curStops(), stops...)...)
}
func (p *parser) popStops() { p.stops = p.stops[:len(p.stops)-1] }

func (p *parser) quoteIndex(b byte) int {
	tok := Token(b)
	for i, stop := range p.curStops() {
		if tok == stop {
			return i
		}
	}
	return -1
}
func (p *parser) quoted(b byte) bool { return p.quoteIndex(b) >= 0 }

// Subshells inside double quotes do not keep spaces, e.g. "$(foo  bar)"
// equals "$(foo bar)"
func (p *parser) doubleQuoted() bool { return p.quoteIndex('"') > p.quoteIndex('`') }

func (p *parser) readByte() (byte, error) {
	b, err := p.br.ReadByte()
	if err != nil {
		p.errPass(err)
		return 0, err
	}
	p.moveWith(b)
	return b, nil
}
func (p *parser) consumeByte() { p.readByte() }
func (p *parser) consumeBytes(n int) {
	for i := 0; i < n; i++ {
		p.consumeByte()
	}
}

func (p *parser) moveWith(b byte) {
	if b == '\n' {
		p.npos.Line++
		p.npos.Column = 1
	} else {
		p.npos.Column++
	}
}

func (p *parser) peekByte() (byte, error) {
	bs, err := p.br.Peek(1)
	if err != nil {
		return 0, err
	}
	return bs[0], nil
}

func (p *parser) peekString(s string) bool {
	bs, err := p.br.Peek(len(s))
	return err == nil && string(bs) == s
}

func (p *parser) peekAnyByte(bs ...byte) bool {
	peek, err := p.br.Peek(1)
	if err != nil {
		return false
	}
	return bytes.IndexByte(bs, peek[0]) >= 0
}

func (p *parser) readOnly(s string) bool {
	if p.peekString(s) {
		p.consumeBytes(len(s))
		return true
	}
	return false
}

var (
	// bytes that form or start a token
	reserved = map[byte]bool{
		'&':  true,
		'>':  true,
		'<':  true,
		'|':  true,
		';':  true,
		'(':  true,
		')':  true,
		'$':  true,
		'"':  true,
		'\'': true,
		'`':  true,
	}
	// subset of the above that mark the end of a word
	wordBreak = map[byte]bool{
		'&': true,
		'>': true,
		'<': true,
		'|': true,
		';': true,
		'(': true,
		')': true,
	}
	// tokenize these inside arithmetic expansions
	arithmOps = map[byte]bool{
		'+': true,
		'-': true,
		'!': true,
		'*': true,
		'/': true,
		'%': true,
	}
	// bytes that will be treated as space
	space = map[byte]bool{
		' ':  true,
		'\t': true,
		'\n': true,
	}
)

func (p *parser) next() {
	p.lpos = p.pos
	var b byte
	p.spaced = false
	p.newLine = false
	p.pos = p.npos
	for {
		if p.readOnly("\\\n") {
			continue
		}
		var err error
		if b, err = p.peekByte(); err != nil {
			p.errPass(err)
			return
		}
		if p.stopNewline && b == '\n' {
			p.advanceTok(STOPPED)
			return
		}
		if p.doubleQuoted() || !space[b] {
			break
		}
		p.consumeByte()
		p.pos = p.npos
		p.spaced = true
		if b == '\n' {
			p.newLine = true
			if len(p.heredocs) > 0 {
				break
			}
		}
	}
	switch {
	case p.newLine && len(p.heredocs) > 0:
		for i, w := range p.heredocs {
			endLine := unquote(w).String()
			if i > 0 {
				p.consumeByte()
			}
			s, _ := p.readHeredocContent(endLine)
			w.Parts[0] = Lit{
				ValuePos: w.Pos(),
				Value:    fmt.Sprintf("%s\n%s", w, s),
			}
		}
		p.heredocs = nil
		p.next()
	case b == '#' && !p.doubleQuoted():
		p.advanceBoth(COMMENT, p.readLine())
	case reserved[b]:
		// Between double quotes, only under certain
		// circumstnaces do we tokenize
		if p.doubleQuoted() {
			switch {
			case b == '`', b == '"', b == '$', p.peek(EXP):
			default:
				p.advanceReadLit()
				return
			}
		}
		tok, _ := doToken(p.readOnly, p.readByte)
		p.advanceTok(tok)
	default:
		p.advanceReadLit()
	}
}

func (p *parser) advanceReadLit() { p.advanceBoth(LIT, string(p.readLitBytes())) }
func (p *parser) readLitBytes() (bs []byte) {
	if p.arithmExp {
		p.spaced = true
	}
	for {
		b, err := p.peekByte()
		if err != nil {
			return
		}
		switch {
		case b == '\\': // escaped byte
			p.consumeByte()
			if b, _ = p.readByte(); b != '\n' {
				bs = append(bs, '\\', b)
			}
			continue
		case b == '$' || b == '`': // end of lit
			return
		case p.doubleQuoted():
			if b == '"' {
				return
			}
		case p.arithmExp && arithmOps[b]:
			if len(bs) > 0 {
				return
			}
			p.consumeByte()
			return []byte{b}
		case reserved[b], space[b]: // end of lit
			return
		}
		p.consumeByte()
		bs = append(bs, b)
	}
}

func (p *parser) advanceTok(tok Token) { p.advanceBoth(tok, tok.String()) }
func (p *parser) advanceBoth(tok Token, val string) {
	if p.tok != EOF {
		p.ltok = p.tok
		p.lval = p.val
	}
	p.tok = tok
	p.val = val
}

func (p *parser) readUntil(s string) (string, bool) {
	var bs []byte
	for {
		if p.peekString(s) {
			return string(bs), true
		}
		b, err := p.readByte()
		if err != nil {
			return string(bs), false
		}
		bs = append(bs, b)
	}
}

func (p *parser) readUntilMatched(lpos Pos, left, right Token) string {
	tokStr := tokNames[right]
	s, found := p.readUntil(tokStr)
	if found {
		p.consumeBytes(len(tokStr))
		p.advanceTok(right)
		p.next()
	} else {
		p.matchingErr(lpos, left, right)
	}
	return s
}

func (p *parser) readLine() string {
	s, _ := p.readUntil("\n")
	return s
}

func (p *parser) readHeredocContent(endLine string) (string, bool) {
	var buf bytes.Buffer
	for p.tok != EOF {
		line := p.readLine()
		if line == endLine {
			fmt.Fprint(&buf, line)
			return buf.String(), true
		}
		fmt.Fprintln(&buf, line)
		p.consumeByte() // newline
	}
	fmt.Fprint(&buf, endLine)
	return buf.String(), false
}

func (p *parser) peek(tok Token) bool {
	for p.tok == COMMENT {
		p.next()
	}
	return p.tok == tok || p.peekReservedWord(tok)
}

func (p *parser) peekReservedWord(tok Token) bool {
	return p.tok == LIT && p.val == tokNames[tok] && p.peekSpaced()
}

func (p *parser) peekSpaced() bool {
	b, err := p.peekByte()
	return err != nil || space[b] || wordBreak[b]
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
	if p.peek(tok) {
		p.next()
		return true
	}
	return false
}

func (p *parser) followErr(lpos Pos, left, right string) {
	p.posErr(lpos, "%s must be followed by %s", left, right)
}

func (p *parser) wantFollow(lpos Pos, left string, tok Token) {
	if !p.got(tok) {
		p.followErr(lpos, left, fmt.Sprintf(`%q`, tok))
	}
}

func (p *parser) wantFollowStmt(lpos Pos, left string, s *Stmt) {
	if !p.gotStmt(s) {
		p.followErr(lpos, left, "a statement")
	}
}

func (p *parser) wantFollowStmts(left string, sts *[]Stmt, stops ...Token) {
	if p.got(SEMICOLON) {
		return
	}
	p.stmts(sts, stops...)
	if len(*sts) < 1 && !p.newLine && !p.got(SEMICOLON) {
		p.followErr(p.lpos, left, "a statement list")
	}
}

func (p *parser) wantFollowWord(left string, w *Word) {
	if !p.gotWord(w) {
		p.followErr(p.lpos, left, "a word")
	}
}

func (p *parser) wantStmtEnd(start Pos, name string, tok Token, pos *Pos) {
	if !p.got(tok) {
		p.posErr(start, `%s statement must end with %q`, name, tok)
	}
	*pos = p.lpos
}

func (p *parser) wantQuote(lpos Pos, b byte) {
	tok := Token(b)
	if !p.got(tok) {
		p.posErr(lpos, `reached %s without closing quote %s`, p.tok, tok)
	}
}

func (p *parser) matchingErr(lpos Pos, left, right Token) {
	p.posErr(lpos, `reached %s without matching token %s with %s`,
		p.tok, left, right)
}

func (p *parser) wantMatched(lpos Pos, left, right Token, rpos *Pos) {
	if !p.got(right) {
		p.matchingErr(lpos, left, right)
	}
	*rpos = p.lpos
}

func (p *parser) errPass(err error) {
	if p.err == nil && err != io.EOF {
		p.err = err
	}
	p.advanceTok(EOF)
}

type lineErr struct {
	pos  Position
	text string
}

func (e lineErr) Error() string {
	return fmt.Sprintf("%s: %s", e.pos, e.text)
}

func (p *parser) posErr(pos Pos, format string, v ...interface{}) {
	p.errPass(lineErr{
		pos: Position{
			Filename: p.file.Name,
			Line:     pos.Line,
			Column:   pos.Column,
		},
		text: fmt.Sprintf(format, v...),
	})
}

func (p *parser) curErr(format string, v ...interface{}) {
	p.posErr(p.pos, format, v...)
}

func (p *parser) stmts(sts *[]Stmt, stops ...Token) {
	// TODO: remove peek(), now needed to ignore any comment tokens
	// and possibly reach EOF
	p.peek(EOF)
	for p.tok != EOF && !p.peekAny(stops...) {
		var s Stmt
		if !p.gotStmt(&s) {
			p.invalidStmtStart()
		}
		*sts = append(*sts, s)
		if !p.peekAny(stops...) && !p.gotEnd {
			p.curErr("statements must be separated by &, ; or a newline")
		}
	}
}

func (p *parser) invalidStmtStart() {
	switch {
	case p.peekAny(SEMICOLON, AND, OR, LAND, LOR):
		p.curErr("%s can only immediately follow a statement", p.tok)
	case p.peek(RBRACE):
		p.curErr("%s can only be used to close a block", p.val)
	case p.peek(RPAREN):
		p.curErr("%s can only be used to close a subshell", p.tok)
	default:
		p.curErr("%s is not a valid start for a statement", p.tok)
	}
}

func (p *parser) stmtsLimited(sts *[]Stmt, stops ...Token) {
	p.addStops(stops...)
	p.stmts(sts, p.curStops()...)
	p.popStops()
}

func (p *parser) stmtsNested(sts *[]Stmt, stop Token) {
	p.newStops(stop)
	p.stmts(sts, p.curStops()...)
	p.popStops()
}

func (p *parser) gotWord(w *Word) bool {
	p.readParts(&w.Parts)
	return len(w.Parts) > 0
}

func (p *parser) gotLit(l *Lit) bool {
	l.ValuePos = p.pos
	if p.got(LIT) {
		l.Value = p.lval
		return true
	}
	return false
}

func (p *parser) readParts(ns *[]Node) {
	for p.tok != EOF {
		n := p.wordPart()
		if n == nil {
			break
		}
		*ns = append(*ns, n)
		if !p.doubleQuoted() && p.spaced {
			break
		}
	}
}

func (p *parser) wordPart() Node {
	switch {
	case p.got(LIT):
		return Lit{
			ValuePos: p.lpos,
			Value:    p.lval,
		}
	case p.peek(EXP):
		return p.exp()
	case p.peek('\''):
		sq := SglQuoted{Quote: p.pos}
		s, found := p.readUntil("'")
		if !found {
			p.wantQuote(sq.Quote, '\'')
		}
		sq.Value = s
		p.consumeByte()
		p.next()
		return sq
	case !p.doubleQuoted() && p.peek('"'):
		dq := DblQuoted{Quote: p.pos}
		p.addStops('"')
		p.next()
		p.readParts(&dq.Parts)
		p.popStops()
		p.wantQuote(dq.Quote, '"')
		return dq
	case !p.quoted('`') && p.peek('`'):
		cs := CmdSubst{Backquotes: true, Left: p.pos}
		p.addStops('`')
		p.next()
		p.stmtsNested(&cs.Stmts, '`')
		p.popStops()
		p.wantMatched(cs.Left, '`', '`', &cs.Right)
		return cs
	}
	return nil
}

func (p *parser) exp() Node {
	if p.peekAnyByte('{') {
		lpos := p.npos
		p.consumeByte()
		return ParamExp{
			Exp:  lpos,
			Text: p.readUntilMatched(lpos, LBRACE, RBRACE),
		}
	}
	if p.readOnly("#") {
		p.advanceBoth(Token('#'), "#")
	} else {
		p.next()
	}
	switch {
	case p.peek(DLPAREN):
		ar := ArithmExp{Exp: p.pos}
		p.arithmExp = true
		p.next()
		p.arithmWords(&ar.Words)
		p.arithmExp = false
		p.wantMatched(ar.Exp, DLPAREN, DRPAREN, &ar.Rparen)
		return ar
	case p.peek(LPAREN):
		cs := CmdSubst{Left: p.pos}
		p.addStops('`')
		p.next()
		p.stmtsNested(&cs.Stmts, RPAREN)
		p.popStops()
		p.wantMatched(cs.Left, LPAREN, RPAREN, &cs.Right)
		return cs
	case p.peekAny('\'', '`', '"'):
		p.curErr("quotes cannot follow a dollar sign")
		return nil
	default:
		p.next()
		return ParamExp{
			Exp:   p.lpos,
			Short: true,
			Text:  p.lval,
		}
	}
}

func (p *parser) readPartsArithm(ns *[]Node) {
	for p.tok != EOF && !p.peek(DRPAREN) {
		n := p.wordPart()
		if n == nil {
			n = Lit{
				Value:    p.val,
				ValuePos: p.pos,
			}
			p.next()
		}
		*ns = append(*ns, n)
		if !p.doubleQuoted() && p.spaced {
			return
		}
	}
}

func (p *parser) arithmWords(ws *[]Word) {
	for p.tok != EOF && !p.peek(DRPAREN) {
		var w Word
		p.readPartsArithm(&w.Parts)
		*ws = append(*ws, w)
	}
}

func (p *parser) wordList(ws *[]Word) {
	for !p.peekEnd() {
		var w Word
		if !p.gotWord(&w) {
			p.curErr("word list can only contain words")
		}
		*ws = append(*ws, w)
	}
	if !p.newLine {
		p.next()
	}
}

func (p *parser) peekEnd() bool {
	// peek for SEMICOLON before checking newLine to consume any
	// comments and set newLine accordingly
	return p.tok == EOF || p.peek(SEMICOLON) || p.newLine
}

func (p *parser) peekStop() bool {
	if p.peekEnd() || p.peekAny(AND, OR, LAND, LOR) {
		return true
	}
	return p.peekAny(p.curStops()...)
}

func (p *parser) peekRedir() bool {
	if p.peek(LIT) && p.peekAnyByte('>', '<') {
		return true
	}
	return p.peekAny(RDROUT, APPEND, RDRIN, DPLIN, DPLOUT, RDRINOUT,
		HEREDOC, DHEREDOC)
}

func (p *parser) gotStmt(s *Stmt) bool {
	if p.peek(RBRACE) {
		// don't let it be a LIT
		return false
	}
	p.gotEnd = false
	s.Position = p.pos
	if p.got(BANG) {
		s.Negated = true
	}
	addRedir := func() {
		s.Redirs = append(s.Redirs, p.redirect())
	}
	for p.peekRedir() {
		addRedir()
	}
	p.gotStmtAndOr(s, addRedir)
	if !p.peekEnd() {
		for p.peekRedir() {
			addRedir()
		}
	}
	if !s.Negated && s.Node == nil && len(s.Redirs) == 0 {
		return false
	}
	if _, ok := s.Node.(FuncDecl); ok {
		return true
	}
	switch {
	case p.got(LAND), p.got(LOR):
		left := *s
		*s = Stmt{
			Position: left.Position,
			Node:     p.binaryExpr(left, addRedir),
		}
		return true
	case p.got(AND):
		s.Background = true
		p.gotEnd = true
	case p.peekStop():
		p.gotEnd = true
	default:
		p.gotEnd = false
	}
	if p.newLine {
		p.gotEnd = true
	} else if p.peekEnd() {
		p.next()
		p.gotEnd = true
	}
	return true
}

func (p *parser) gotStmtAndOr(s *Stmt, addRedir func()) bool {
	s.Position = p.pos
	switch {
	case p.got(LPAREN):
		s.Node = p.subshell()
	case p.got(LBRACE):
		s.Node = p.block()
	case p.got(IF):
		s.Node = p.ifStmt()
	case p.got(WHILE):
		s.Node = p.whileStmt()
	case p.got(UNTIL):
		s.Node = p.untilStmt()
	case p.got(FOR):
		s.Node = p.forStmt()
	case p.got(CASE):
		s.Node = p.caseStmt()
	case p.peekAny(LIT, EXP, '"', '\'', '`'):
		s.Node = p.cmdOrFunc(addRedir)
	default:
		return false
	}
	if p.got(OR) {
		left := *s
		*s = Stmt{
			Position: left.Position,
			Node:     p.binaryExpr(left, addRedir),
		}
	}
	return true
}

func (p *parser) binaryExpr(left Stmt, addRedir func()) BinaryExpr {
	b := BinaryExpr{
		OpPos: p.lpos,
		Op:    p.ltok,
		X:     left,
	}
	if b.Op == LAND || b.Op == LOR {
		p.wantFollowStmt(b.OpPos, b.Op.String(), &b.Y)
	} else if !p.gotStmtAndOr(&b.Y, addRedir) {
		p.followErr(b.OpPos, b.Op.String(), "a statement")
	}
	return b
}

func unquote(w Word) Word {
	w2 := w
	w2.Parts = nil
	for _, n := range w.Parts {
		switch x := n.(type) {
		case SglQuoted:
			w2.Parts = append(w2.Parts, Lit{Value: x.Value})
		case DblQuoted:
			w2.Parts = append(w2.Parts, x.Parts...)
		default:
			w2.Parts = append(w2.Parts, n)
		}
	}
	return w2
}

func (p *parser) redirect() (r Redirect) {
	p.gotLit(&r.N)
	r.Op = p.tok
	r.OpPos = p.pos
	p.next()
	switch r.Op {
	case HEREDOC, DHEREDOC:
		p.stopNewline = true
		p.wantFollowWord(r.Op.String(), &r.Word)
		p.stopNewline = false
		p.heredocs = append(p.heredocs, r.Word)
		if p.tok == STOPPED {
			p.next()
		}
	default:
		p.wantFollowWord(r.Op.String(), &r.Word)
	}
	return
}

func (p *parser) subshell() (s Subshell) {
	s.Lparen = p.lpos
	p.stmtsLimited(&s.Stmts, RPAREN)
	p.wantMatched(s.Lparen, LPAREN, RPAREN, &s.Rparen)
	return
}

func (p *parser) block() (b Block) {
	b.Lbrace = p.lpos
	p.stmts(&b.Stmts, RBRACE)
	p.wantMatched(b.Lbrace, LBRACE, RBRACE, &b.Rbrace)
	return
}

func (p *parser) ifStmt() (fs IfStmt) {
	fs.If = p.lpos
	p.wantFollowStmts(`"if"`, &fs.Conds, THEN)
	p.wantFollow(fs.If, `"if [stmts]"`, THEN)
	p.wantFollowStmts(`"then"`, &fs.ThenStmts, FI, ELIF, ELSE)
	for p.got(ELIF) {
		elf := Elif{Elif: p.lpos}
		p.wantFollowStmts(`"elif"`, &elf.Conds, THEN)
		p.wantFollow(elf.Elif, `"elif [stmts]"`, THEN)
		p.wantFollowStmts(`"then"`, &elf.ThenStmts, FI, ELIF, ELSE)
		fs.Elifs = append(fs.Elifs, elf)
	}
	if p.got(ELSE) {
		p.wantFollowStmts(`"else"`, &fs.ElseStmts, FI)
	}
	p.wantStmtEnd(fs.If, "if", FI, &fs.Fi)
	return
}

func (p *parser) whileStmt() (ws WhileStmt) {
	ws.While = p.lpos
	p.wantFollowStmts(`"while"`, &ws.Conds, DO)
	p.wantFollow(ws.While, `"while [stmts]"`, DO)
	p.wantFollowStmts(`"do"`, &ws.DoStmts, DONE)
	p.wantStmtEnd(ws.While, "while", DONE, &ws.Done)
	return
}

func (p *parser) untilStmt() (us UntilStmt) {
	us.Until = p.lpos
	p.wantFollowStmts(`"until"`, &us.Conds, DO)
	p.wantFollow(us.Until, `"until [stmts]"`, DO)
	p.wantFollowStmts(`"do"`, &us.DoStmts, DONE)
	p.wantStmtEnd(us.Until, "until", DONE, &us.Done)
	return
}

func (p *parser) forStmt() (fs ForStmt) {
	fs.For = p.lpos
	if !p.gotLit(&fs.Name) {
		p.followErr(fs.For, `"for"`, "a literal")
	}
	if p.got(IN) {
		p.wordList(&fs.WordList)
	} else if !p.got(SEMICOLON) && !p.newLine {
		p.followErr(fs.For, `"for foo"`, `"in", ; or a newline`)
	}
	p.wantFollow(fs.For, `"for foo [in words]"`, DO)
	p.wantFollowStmts(`"do"`, &fs.DoStmts, DONE)
	p.wantStmtEnd(fs.For, "for", DONE, &fs.Done)
	return
}

func (p *parser) caseStmt() (cs CaseStmt) {
	cs.Case = p.lpos
	p.wantFollowWord(`"case"`, &cs.Word)
	p.wantFollow(cs.Case, `"case x"`, IN)
	p.patLists(&cs.List)
	p.wantStmtEnd(cs.Case, "case", ESAC, &cs.Esac)
	return
}

func (p *parser) patLists(plists *[]PatternList) {
	if p.got(SEMICOLON) {
		return
	}
	for p.tok != EOF && !p.peek(ESAC) {
		var pl PatternList
		p.got(LPAREN)
		for p.tok != EOF {
			var w Word
			if !p.gotWord(&w) {
				p.curErr("case patterns must consist of words")
			}
			pl.Patterns = append(pl.Patterns, w)
			if p.got(RPAREN) {
				break
			}
			if !p.got(OR) {
				p.curErr("case patterns must be separated with |")
			}
		}
		p.stmtsLimited(&pl.Stmts, DSEMICOLON, ESAC)
		*plists = append(*plists, pl)
		if !p.got(DSEMICOLON) {
			break
		}
	}
}

func (p *parser) cmdOrFunc(addRedir func()) Node {
	var w Word
	p.gotWord(&w)
	if !p.newLine && p.got(LPAREN) {
		return p.funcDecl(w)
	}
	cmd := Command{Args: []Word{w}}
	for !p.peekStop() {
		var w Word
		switch {
		case p.peekRedir():
			addRedir()
		case p.gotWord(&w):
			cmd.Args = append(cmd.Args, w)
		default:
			p.curErr("a command can only contain words and redirects")
		}
	}
	return cmd
}

var identRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func (p *parser) funcDecl(w Word) (fd FuncDecl) {
	if !p.got(RPAREN) {
		p.curErr(`functions must start like "foo()"`)
	}
	fd.Name.Value = w.String()
	if !identRe.MatchString(fd.Name.Value) {
		p.posErr(w.Pos(), "invalid func name: %s", fd.Name.Value)
	}
	fd.Name.ValuePos = w.Pos()
	p.wantFollowStmt(w.Pos(), `"foo()"`, &fd.Body)
	return
}
