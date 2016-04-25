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

func Parse(r io.Reader, name string) (File, error) {
	p := &parser{
		r:     bufio.NewReader(r),
		fname: name,
		npos: Position{
			Line:   1,
			Column: 1,
		},
		stops: [][]Token{nil},
	}
	p.next()
	var file File
	p.stmts(&file.Stmts)
	return file, p.err
}

type parser struct {
	r     *bufio.Reader
	fname string

	err error

	spaced bool
	quote  byte

	ltok, tok Token
	lval, val string

	// backup position to unread a byte
	bpos Position

	lpos, pos, npos Position

	stops [][]Token

	// to not include ')' in a literal
	quotedCmdSubst bool
}

// Position describes an arbitrary position in a source file. Offsets,
// including column numbers, are in bytes.
type Position struct {
	Line   int // line number, starting at 1
	Column int // column number, starting at 1
}

func (p *parser) readByte() (byte, error) {
	b, err := p.r.ReadByte()
	if err != nil {
		if err == io.EOF {
			p.advanceTok(EOF)
		} else {
			p.errPass(err)
		}
		return 0, err
	}
	p.moveWith(b)
	return b, nil
}

func (p *parser) moveWith(b byte) {
	p.bpos = p.npos
	if b == '\n' {
		p.npos.Line++
		p.npos.Column = 1
	} else {
		p.npos.Column++
	}
}

func (p *parser) unreadByte() {
	if err := p.r.UnreadByte(); err != nil {
		panic(err)
	}
	p.npos = p.bpos
}

func (p *parser) peekByte(b byte) bool {
	bs, err := p.r.Peek(1)
	if err != nil {
		return false
	}
	return b == bs[0]
}

func (p *parser) readOnly(b byte) bool {
	if p.peekByte(b) {
		p.readByte()
		return true
	}
	return false
}

var (
	reserved = map[byte]bool{
		'\n': true,
		'&':  true,
		'>':  true,
		'<':  true,
		'|':  true,
		';':  true,
		'(':  true,
		')':  true,
		'$':  true,
		'"':  true,
	}
	// like reserved, but these are only reserved if at the start of a word
	starters = map[byte]bool{
		'{': true,
		'}': true,
		'#': true,
	}
	space = map[byte]bool{
		' ':  true,
		'\t': true,
	}
	matching = map[Token]Token{
		LPAREN:  RPAREN,
		LBRACE:  RBRACE,
		DLPAREN: DRPAREN,
	}
)

func (p *parser) next() {
	p.lpos = p.pos
	var b byte
	p.spaced = false
	p.pos = p.npos
	for {
		var err error
		if b, err = p.readByte(); err != nil {
			return
		}
		if b == '\\' && p.readOnly('\n') {
			continue
		}
		if p.quote != 0 || !space[b] {
			break
		}
		p.pos = p.npos
		p.spaced = true
	}
	if reserved[b] || starters[b] {
		// Between double quotes, only under certain
		// circumstnaces do we tokenize
		if p.quote == '"' {
			switch {
			case b == '"', b == '$', p.tok == EXP:
			case b == ')' && p.quotedCmdSubst:
			default:
				p.advanceReadLit()
				return
			}
		}
		p.advanceTok(doToken(b, p.readOnly))
	} else {
		p.advanceReadLit()
	}
}

func (p *parser) advanceReadLit() {
	p.unreadByte()
	p.advanceBoth(LIT, string(p.readLitBytes()))
}

func (p *parser) readLitBytes() (bs []byte) {
	var lpos Position
	for {
		b, err := p.readByte()
		if err != nil {
			if p.quote != 0 {
				p.wantQuote(lpos, Token(p.quote))
			}
			break
		}
		switch {
		case p.quote != '\'' && b == '\\': // escaped byte
			b, _ = p.readByte()
			if b != '\n' {
				bs = append(bs, '\\', b)
			}
			continue
		case p.quote != '\'' && b == '$': // end of lit
			p.unreadByte()
			return
		case p.quote == '"':
			if b == p.quote || (p.quotedCmdSubst && b == ')') {
				p.unreadByte()
				return
			}
		case p.quote == '\'':
			if b == p.quote {
				p.quote = 0
			}
		case b == '\'':
			p.quote = '\''
			lpos = p.npos
			lpos.Column--
		case reserved[b], space[b]: // end of lit
			p.unreadByte()
			return
		}
		bs = append(bs, b)
	}
	return
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

func (p *parser) readUntil(closing Token) (string, bool) {
	var buf bytes.Buffer
	for {
		b, err := p.readByte()
		if err != nil {
			return buf.String(), false
		}
		tok := doToken(b, p.readOnly)
		if tok == closing {
			return buf.String(), true
		}
		fmt.Fprint(&buf, tok)
	}
}

func (p *parser) readUntilMatched(left Token) string {
	lpos := p.pos
	s, found := p.readUntil(matching[left])
	if found {
		p.next()
	} else {
		p.matchingErr(lpos, left)
	}
	return s
}

func (p *parser) readLine() string {
	s, _ := p.readUntil('\n')
	return s
}

func (p *parser) readUntilLine(s string) (string, bool) {
	var buf bytes.Buffer
	for p.tok != EOF {
		l := p.readLine()
		if l == s {
			return buf.String(), true
		}
		fmt.Fprintln(&buf, l)
	}
	return buf.String(), false
}

func (p *parser) peek(tok Token) bool {
	return p.tok == tok || (p.tok == LIT && p.val == tokNames[tok])
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

func (p *parser) gotAny(toks ...Token) bool {
	for _, tok := range toks {
		if p.got(tok) {
			return true
		}
	}
	return false
}

func (p *parser) followErr(left, right string) {
	p.curErr("%s must be followed by %s", left, right)
}

func (p *parser) wantFollow(left string, tok Token) {
	if !p.got(tok) {
		p.followErr(left, fmt.Sprintf(`%q`, tok))
	}
}

func (p *parser) wantFollowStmt(left string, s *Stmt, wantStop bool) {
	if !p.gotStmt(s, wantStop) {
		p.followErr(left, "a statement")
	}
}

func (p *parser) wantFollowStmts(left string, sts *[]Stmt, stop ...Token) {
	if p.stmts(sts, stop...) < 1 {
		p.followErr(left, "one or more statements")
	}
}

func (p *parser) wantFollowWord(left string, w *Word) {
	if !p.gotWord(w) {
		p.followErr(left, "a word")
	}
}

func (p *parser) wantFollowLit(left string, l *Lit) {
	if !p.gotLit(l) {
		p.followErr(left, "a literal")
	}
}

func (p *parser) wantStmtEnd(name string, tok Token) {
	if !p.got(tok) {
		p.curErr(`%s statement must end with %q`, name, tok)
	}
}

func (p *parser) closingErr(lpos Position, s string) {
	p.posErr(lpos, `reached %s without closing %s`, p.tok, s)
}

func (p *parser) wantQuote(lpos Position, tok Token) {
	if !p.got(tok) {
		p.closingErr(lpos, fmt.Sprintf("quote %s", tok))
	}
}

func (p *parser) matchingErr(lpos Position, left Token) {
	p.posErr(lpos, `reached %s without matching token %s with %s`,
		p.tok, left, matching[left])
}

func (p *parser) wantMatched(lpos Position, left Token) {
	if !p.got(matching[left]) {
		p.matchingErr(lpos, left)
	}
}

func (p *parser) errPass(err error) {
	if p.err == nil {
		p.err = err
	}
	p.advanceTok(EOF)
}

type lineErr struct {
	fname string
	pos   Position
	text  string
}

func (e lineErr) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", e.fname, e.pos.Line, e.pos.Column, e.text)
}

func (p *parser) posErr(pos Position, format string, v ...interface{}) {
	p.errPass(lineErr{
		fname: p.fname,
		pos:   pos,
		text:  fmt.Sprintf(format, v...),
	})
}

func (p *parser) curErr(format string, v ...interface{}) {
	if p.tok == EOF {
		p.pos = p.npos
	}
	p.posErr(p.pos, format, v...)
}

func (p *parser) stmts(sts *[]Stmt, stop ...Token) (count int) {
	for p.tok != EOF && !p.peekAny(stop...) {
		var s Stmt
		if !p.gotStmt(&s, true) {
			if p.tok != EOF && !p.peekAny(stop...) {
				p.invalidStmtStart()
			}
			break
		}
		*sts = append(*sts, s)
		count++
	}
	return
}

func (p *parser) invalidStmtStart() {
	switch p.tok {
	case SEMICOLON, AND, OR, LAND, LOR:
		p.curErr("%s can only immediately follow a statement", p.tok)
	case RBRACE:
		p.curErr("%s can only be used to close a block", p.tok)
	case RPAREN:
		p.curErr("%s can only be used to close a subshell", p.tok)
	default:
		p.curErr("%s is not a valid start for a statement", p.tok)
	}
}

func (p *parser) stmtsLimited(sts *[]Stmt, stop ...Token) int {
	p.stops = append(p.stops, stop)
	count := p.stmts(sts, stop...)
	p.stops = p.stops[:len(p.stops)-1]
	return count
}

func (p *parser) gotWord(w *Word) bool {
	return p.readParts(&w.Parts) > 0
}

func (p *parser) gotLit(l *Lit) bool {
	l.ValuePos = p.pos
	if p.got(LIT) {
		l.Value = p.lval
		return true
	}
	return false
}

func (p *parser) readParts(ns *[]Node) (count int) {
	for p.tok != EOF {
		var n Node
		switch {
		case p.quote == 0 && count > 0 && p.spaced:
			return
		case p.got(LIT):
			n = Lit{
				ValuePos: p.lpos,
				Value:    p.lval,
			}
		case p.quote == 0 && p.peek('"'):
			var dq DblQuoted
			p.quote = '"'
			dq.Quote = p.pos
			p.next()
			p.readParts(&dq.Parts)
			p.quote = 0
			p.wantQuote(dq.Quote, '"')
			n = dq
		case p.got(EXP):
			n = p.exp()
		default:
			return
		}
		*ns = append(*ns, n)
		count++
	}
	return
}

func (p *parser) exp() Node {
	switch {
	case p.peek(LBRACE):
		return ParamExp{
			Exp:  p.pos,
			Text: p.readUntilMatched(LBRACE),
		}
	case p.peek(DLPAREN):
		return ArithmExp{
			Exp:  p.pos,
			Text: p.readUntilMatched(DLPAREN),
		}
	case p.peek(LPAREN):
		var cs CmdSubst
		p.quotedCmdSubst = p.quote == '"'
		p.next()
		cs.Exp = p.lpos
		p.stmtsLimited(&cs.Stmts, RPAREN)
		p.quotedCmdSubst = false
		p.wantMatched(cs.Exp, LPAREN)
		return cs
	default:
		p.next()
		return ParamExp{
			Exp:   p.lpos,
			Short: true,
			Text:  p.lval,
		}
	}
}

func (p *parser) wordList(ws *[]Word) (count int) {
	for p.tok != EOF {
		if p.peekStop() {
			p.gotAny(SEMICOLON, '\n')
			break
		}
		var w Word
		p.gotWord(&w)
		*ws = append(*ws, w)
		count++
	}
	return
}

func (p *parser) peekEnd() bool {
	return p.tok == EOF || p.peekAny(SEMICOLON, '\n', '#')
}

func (p *parser) peekStop() bool {
	if p.peekEnd() || p.peekAny(AND, OR, LAND, LOR) {
		return true
	}
	stop := p.stops[len(p.stops)-1]
	return p.peekAny(stop...)
}

func (p *parser) peekRedir() bool {
	// Can this be done in a way that doesn't involve peeking past
	// the current token?
	if p.peek(LIT) && (p.peekByte('>') || p.peekByte('<')) {
		return true
	}
	return p.peekAny(RDROUT, APPEND, RDRIN, DPLIN, DPLOUT, OPRDWR,
		HEREDOC, DHEREDOC)
}

func (p *parser) gotStmt(s *Stmt, wantStop bool) bool {
	for p.gotAny('#', '\n') {
		if p.ltok == '#' {
			p.readLine()
			p.next()
		}
	}
	addRedir := func() {
		s.Redirs = append(s.Redirs, p.redirect())
	}
	s.Position = p.pos
	for p.peekRedir() {
		addRedir()
	}
	switch {
	case p.got(LPAREN):
		s.Node = p.subshell()
	case p.got(LBRACE):
		s.Node = p.block()
	case p.got(IF):
		s.Node = p.ifStmt()
	case p.got(WHILE):
		s.Node = p.whileStmt()
	case p.got(FOR):
		s.Node = p.forStmt()
	case p.got(CASE):
		s.Node = p.caseStmt()
	case p.peekAny(LIT, EXP, '"'):
		s.Node = p.cmdOrFunc(addRedir)
	}
	for p.peekRedir() {
		addRedir()
	}
	if s.Node == nil && len(s.Redirs) == 0 {
		return false
	}
	if p.got(AND) {
		s.Background = true
	}
	if !wantStop {
		return true
	}
	if !p.peekStop() {
		p.curErr("statements must be separated by ; or a newline")
	}
	if p.gotAny(OR, LAND, LOR) {
		left := *s
		*s = Stmt{
			Position: left.Position,
			Node:     p.binaryExpr(p.ltok, left),
		}
	}
	if p.peekEnd() {
		if p.tok == '#' {
			p.readLine()
		}
		p.next()
	}
	return true
}

func (p *parser) binaryExpr(op Token, left Stmt) (b BinaryExpr) {
	b.OpPos = p.lpos
	b.Op = op
	p.wantFollowStmt(op.String(), &b.Y, true)
	b.X = left
	return
}

func (p *parser) redirect() (r Redirect) {
	p.gotLit(&r.N)
	r.Op = p.tok
	r.OpPos = p.pos
	p.next()
	switch r.Op {
	case HEREDOC, DHEREDOC:
		var l Lit
		lpos := p.pos
		p.wantFollowLit(r.Op.String(), &l)
		del := l.Value
		s, found := p.readUntilLine(del)
		if !found {
			p.closingErr(lpos, fmt.Sprintf(`heredoc %q`, del))
		}
		body := del + "\n" + s + del
		r.Word = Word{Parts: []Node{Lit{
			ValuePos: lpos,
			Value:    body,
		}}}
	default:
		p.wantFollowWord(r.Op.String(), &r.Word)
	}
	return
}

func (p *parser) subshell() (s Subshell) {
	s.Lparen = p.lpos
	if p.stmtsLimited(&s.Stmts, RPAREN) < 1 {
		p.curErr("a subshell must contain one or more statements")
	}
	p.wantMatched(s.Lparen, LPAREN)
	s.Rparen = p.lpos
	return
}

func (p *parser) block() (b Block) {
	b.Lbrace = p.lpos
	if p.stmts(&b.Stmts, RBRACE) < 1 {
		p.curErr("a block must contain one or more statements")
	}
	p.wantMatched(b.Lbrace, LBRACE)
	b.Rbrace = p.lpos
	return
}

func (p *parser) ifStmt() (fs IfStmt) {
	fs.If = p.lpos
	p.wantFollowStmts(`"if"`, &fs.Conds, THEN)
	p.wantFollow(`"if x"`, THEN)
	p.stmts(&fs.ThenStmts, FI, ELIF, ELSE)
	for p.got(ELIF) {
		var elf Elif
		p.wantFollowStmts(`"elif"`, &elf.Conds, THEN)
		elf.Elif = p.lpos
		p.wantFollow(`"elif x"`, THEN)
		p.stmts(&elf.ThenStmts, FI, ELIF, ELSE)
		fs.Elifs = append(fs.Elifs, elf)
	}
	if p.got(ELSE) {
		p.stmts(&fs.ElseStmts, FI)
	}
	p.wantStmtEnd("if", FI)
	fs.Fi = p.lpos
	return
}

func (p *parser) whileStmt() (ws WhileStmt) {
	ws.While = p.lpos
	p.wantFollowStmts(`"while"`, &ws.Conds, DO)
	p.wantFollow(`"while x"`, DO)
	p.stmts(&ws.DoStmts, DONE)
	p.wantStmtEnd("while", DONE)
	ws.Done = p.lpos
	return
}

func (p *parser) forStmt() (fs ForStmt) {
	fs.For = p.lpos
	p.wantFollowLit(`"for"`, &fs.Name)
	for p.got('\n') {
	}
	desc := `"for foo"`
	if p.got(IN) {
		p.wordList(&fs.WordList)
		desc = `"for foo in list"`
	} else {
		p.gotAny(SEMICOLON, '\n')
	}
	p.wantFollow(desc, DO)
	p.stmts(&fs.DoStmts, DONE)
	p.wantStmtEnd("for", DONE)
	fs.Done = p.lpos
	return
}

func (p *parser) caseStmt() (cs CaseStmt) {
	cs.Case = p.lpos
	p.wantFollowWord(`"case"`, &cs.Word)
	for p.got('\n') {
	}
	p.wantFollow(`"case x"`, IN)
	if p.patLists(&cs.List) < 1 {
		p.followErr(`"case x in"`, "one or more patterns")
	}
	p.wantStmtEnd("case", ESAC)
	cs.Esac = p.lpos
	return
}

func (p *parser) patLists(plists *[]PatternList) (count int) {
	for p.tok != EOF && !p.peek(ESAC) {
		for p.got('\n') {
		}
		var pl PatternList
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
		count++
		if !p.got(DSEMICOLON) {
			break
		}
		for p.got('\n') {
		}
	}
	return
}

func (p *parser) cmdOrFunc(addRedir func()) Node {
	var w Word
	p.gotWord(&w)
	if p.got(LPAREN) {
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
	p.wantFollowStmt(`"foo()"`, &fd.Body, false)
	return
}
