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
	if p.tok != EOF {
		p.invalidStmtStart()
	}
	return p.file, p.err
}

type parser struct {
	br *bufio.Reader

	file File
	err  error

	spaced, newLine, gotEnd bool

	ltok, tok Token
	lval, val string

	lpos, pos, npos Pos

	// stack of stop tokens
	stops [][]Token
}

func (p *parser) curStops() []Token { return p.stops[len(p.stops)-1] }
func (p *parser) newStops(stop ...Token) {
	p.stops = append(p.stops, stop)
}
func (p *parser) addStops(stop ...Token) {
	p.newStops(append(p.curStops(), stop...)...)
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

func (p *parser) moveBytes(n int) {
	for i := 0; i < n; i++ {
		p.readByte()
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
		p.moveBytes(len(s))
		return true
	}
	return false
}

var (
	reserved = map[byte]bool{
		'&': true,
		'>': true,
		'<': true,
		'|': true,
		';': true,
		'(': true,
		')': true,
		'$': true,
		'"': true,
		'`': true,
	}
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
		if p.peekString("\\\n") {
			p.moveBytes(2)
			continue
		}
		var err error
		if b, err = p.peekByte(); err != nil {
			p.readByte()
			return
		}
		if p.doubleQuoted() || !space[b] {
			break
		}
		p.readByte()
		p.pos = p.npos
		p.spaced = true
		if b == '\n' {
			p.newLine = true
		}
	}
	switch {
	case b == '#':
		p.advanceBoth('#', p.readLine())
	case reserved[b]:
		// Between double quotes, only under certain
		// circumstnaces do we tokenize
		if p.doubleQuoted() {
			switch {
			case b == '`', b == '"', b == '$', p.tok == EXP:
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
	var qpos Pos
	for {
		b, err := p.peekByte()
		if err != nil {
			if qpos.IsValid() {
				p.readByte()
				p.wantQuote(qpos, '\'')
			}
			return
		}
		switch {
		case qpos.IsValid():
			if b == '\'' {
				qpos = Pos{}
			}
		case b == '\\': // escaped byte
			p.readByte()
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
		case b == '\'':
			qpos = p.npos
		case reserved[b], space[b]: // end of lit
			return
		}
		p.readByte()
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
			p.moveBytes(len(s))
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
	s, found := p.readUntil(tokNames[right])
	if found {
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

func (p *parser) readUntilLine(s string) (string, bool) {
	var buf bytes.Buffer
	for p.tok != EOF {
		if s == p.readLine() {
			return buf.String(), true
		}
	}
	return buf.String(), false
}

// TODO: it's reserved words, not literals
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
	p.stmts(sts, stop...)
	if len(*sts) < 1 && !p.newLine && !p.gotAny(SEMICOLON) {
		p.followErr(left, "a statement list")
	}
}

func (p *parser) wantFollowWord(left string, w *Word) {
	if !p.gotWord(w) {
		p.followErr(left, "a word")
	}
}

func (p *parser) wantStmtEnd(name string, tok Token) {
	if !p.got(tok) {
		p.curErr(`%s statement must end with %q`, name, tok)
	}
}

func (p *parser) closingErr(lpos Pos, s string) {
	p.posErr(lpos, `reached %s without closing %s`, p.tok, s)
}

func (p *parser) wantQuote(lpos Pos, b byte) {
	tok := Token(b)
	if !p.got(tok) {
		p.closingErr(lpos, fmt.Sprintf("quote %s", tok))
	}
}

func (p *parser) matchingErr(lpos Pos, left, right Token) {
	p.posErr(lpos, `reached %s without matching token %s with %s`,
		p.tok, left, right)
}

func (p *parser) wantMatched(lpos Pos, left, right Token) {
	if !p.got(right) {
		p.matchingErr(lpos, left, right)
	}
}

func (p *parser) errPass(err error) {
	if p.err == nil {
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

func (p *parser) stmts(sts *[]Stmt, stop ...Token) {
	if p.peek(SEMICOLON) {
		return
	}
	for p.tok != EOF && !p.peekAny(stop...) {
		var s Stmt
		if !p.gotStmt(&s, true) {
			if p.tok != EOF && !p.peekAny(stop...) {
				p.invalidStmtStart()
			}
			break
		}
		*sts = append(*sts, s)
		if !p.peekAny(stop...) && !p.gotEnd {
			p.curErr("statements must be separated by &, ; or a newline")
			break
		}
	}
}

func (p *parser) invalidStmtStart() {
	switch {
	case p.peekAny(SEMICOLON, AND, OR, LAND, LOR):
		p.curErr("%s can only immediately follow a statement", p.tok)
	case p.peekAny(RBRACE):
		p.curErr("%s can only be used to close a block", p.val)
	case p.peekAny(RPAREN):
		p.curErr("%s can only be used to close a subshell", p.tok)
	default:
		p.curErr("%s is not a valid start for a statement", p.tok)
	}
}

func (p *parser) stmtsLimited(sts *[]Stmt, stop ...Token) {
	p.addStops(stop...)
	p.stmts(sts, p.curStops()...)
	p.popStops()
}

func (p *parser) stmtsNested(sts *[]Stmt, stop Token) {
	p.newStops(stop)
	p.stmts(sts, p.curStops()...)
	p.popStops()
}

func (p *parser) gotWord(w *Word) bool { return p.readParts(&w.Parts) > 0 }
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
		case !p.doubleQuoted() && count > 0 && p.spaced:
			return
		case p.got(LIT):
			n = Lit{
				ValuePos: p.lpos,
				Value:    p.lval,
			}
		case !p.doubleQuoted() && p.peek('"'):
			var dq DblQuoted
			p.addStops('"')
			dq.Quote = p.pos
			p.next()
			p.readParts(&dq.Parts)
			p.popStops()
			p.wantQuote(dq.Quote, '"')
			n = dq
		case !p.quoted('`') && p.peek('`'):
			var bq BckQuoted
			p.addStops('`')
			bq.Quote = p.pos
			p.next()
			p.stmtsNested(&bq.Stmts, '`')
			p.popStops()
			p.wantQuote(bq.Quote, '`')
			n = bq
		case p.peek(EXP):
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
	if p.peekAnyByte('{') {
		lpos := p.npos
		p.readByte()
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
		return ArithmExp{
			Exp:  p.pos,
			Text: p.readUntilMatched(p.pos, DLPAREN, DRPAREN),
		}
	case p.peek(LPAREN):
		var cs CmdSubst
		p.addStops('`')
		p.next()
		cs.Exp = p.lpos
		p.stmtsNested(&cs.Stmts, RPAREN)
		p.popStops()
		p.wantMatched(cs.Exp, LPAREN, RPAREN)
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

func (p *parser) wordList(ws *[]Word) {
	for !p.peekEnd() {
		var w Word
		p.gotWord(&w)
		*ws = append(*ws, w)
	}
	if !p.newLine {
		p.next()
	}
}

func (p *parser) peekEnd() bool {
	return p.tok == EOF || p.newLine || p.peekAny(SEMICOLON, '#')
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
	return p.peekAny(RDROUT, APPEND, RDRIN, DPLIN, DPLOUT, OPRDWR,
		HEREDOC, DHEREDOC)
}

func (p *parser) gotStmt(s *Stmt, wantStop bool) bool {
	p.gotEnd = false
	for p.got('#') {
	}
	addRedir := func() {
		s.Redirs = append(s.Redirs, p.redirect())
	}
	s.Position = p.pos
	if p.got(BANG) {
		s.Negated = true
	}
	for p.peekRedir() {
		addRedir()
	}
	end := true
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
	case p.peek(RBRACE):
		// don't let it be a LIT
		return false
	case p.peekAny(LIT, EXP, '"', '`'):
		s.Node = p.cmdOrFunc(addRedir)
		end = false
	}
	p.gotEnd = end
	for p.peekRedir() {
		addRedir()
		p.gotEnd = false
	}
	if !s.Negated && s.Node == nil && len(s.Redirs) == 0 {
		return false
	}
	if !wantStop {
		return true
	}
	if p.got(AND) {
		s.Background = true
		p.gotEnd = true
	} else {
		if !p.peekStop() {
			p.gotEnd = false
			return true
		}
		if p.gotAny(OR, LAND, LOR) {
			left := *s
			*s = Stmt{
				Position: left.Position,
				Node:     p.binaryExpr(p.ltok, left),
			}
		}
	}
	if p.peekEnd() && !p.newLine {
		p.next()
		p.gotEnd = true
	}
	if p.newLine {
		p.gotEnd = true
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

func unquote(w Word) Word {
	w2 := w
	w2.Parts = nil
	for _, n := range w.Parts {
		if dq, ok := n.(DblQuoted); ok {
			w2.Parts = append(w2.Parts, dq.Parts...)
		} else {
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
		var w Word
		p.wantFollowWord(r.Op.String(), &w)
		del := unquote(w).String()
		s, _ := p.readUntilLine(del)
		s = p.lval + "\n" + s // TODO: dirty hack, don't tokenize heredoc
		body := w.String() + "\n" + s + del
		r.Word = Word{Parts: []Node{Lit{
			ValuePos: w.Pos(),
			Value:    body,
		}}}
	default:
		p.wantFollowWord(r.Op.String(), &r.Word)
	}
	return
}

func (p *parser) subshell() (s Subshell) {
	s.Lparen = p.lpos
	p.stmtsLimited(&s.Stmts, RPAREN)
	p.wantMatched(s.Lparen, LPAREN, RPAREN)
	s.Rparen = p.lpos
	return
}

func (p *parser) block() (b Block) {
	b.Lbrace = p.lpos
	p.stmts(&b.Stmts, RBRACE)
	p.wantMatched(b.Lbrace, LBRACE, RBRACE)
	b.Rbrace = p.lpos
	return
}

func (p *parser) ifStmt() (fs IfStmt) {
	fs.If = p.lpos
	p.wantFollowStmts(`"if"`, &fs.Conds, THEN)
	p.wantFollow(`"if [stmts]"`, THEN)
	p.wantFollowStmts(`"then"`, &fs.ThenStmts, FI, ELIF, ELSE)
	for p.got(ELIF) {
		var elf Elif
		p.wantFollowStmts(`"elif"`, &elf.Conds, THEN)
		elf.Elif = p.lpos
		p.wantFollow(`"elif [stmts]"`, THEN)
		p.wantFollowStmts(`"then"`, &elf.ThenStmts, FI, ELIF, ELSE)
		fs.Elifs = append(fs.Elifs, elf)
	}
	if p.got(ELSE) {
		p.wantFollowStmts(`"else"`, &fs.ElseStmts, FI)
	}
	p.wantStmtEnd("if", FI)
	fs.Fi = p.lpos
	return
}

func (p *parser) whileStmt() (ws WhileStmt) {
	ws.While = p.lpos
	p.wantFollowStmts(`"while"`, &ws.Conds, DO)
	p.wantFollow(`"while [stmts]"`, DO)
	p.wantFollowStmts(`"do"`, &ws.DoStmts, DONE)
	p.wantStmtEnd("while", DONE)
	ws.Done = p.lpos
	return
}

func (p *parser) untilStmt() (us UntilStmt) {
	us.Until = p.lpos
	p.wantFollowStmts(`"until"`, &us.Conds, DO)
	p.wantFollow(`"until [stmts]"`, DO)
	p.wantFollowStmts(`"do"`, &us.DoStmts, DONE)
	p.wantStmtEnd("until", DONE)
	us.Done = p.lpos
	return
}

func (p *parser) forStmt() (fs ForStmt) {
	fs.For = p.lpos
	if !p.gotLit(&fs.Name) {
		p.followErr(`"for"`, "a literal")
	}
	if p.got(IN) {
		p.wordList(&fs.WordList)
	} else if !p.got(SEMICOLON) && !p.newLine {
		p.followErr(`"for foo"`, `"in", ; or a newline`)
	}
	p.wantFollow(`"for foo [in words]"`, DO)
	p.wantFollowStmts(`"do"`, &fs.DoStmts, DONE)
	p.wantStmtEnd("for", DONE)
	fs.Done = p.lpos
	return
}

func (p *parser) caseStmt() (cs CaseStmt) {
	cs.Case = p.lpos
	p.wantFollowWord(`"case"`, &cs.Word)
	p.wantFollow(`"case x"`, IN)
	p.patLists(&cs.List)
	p.wantStmtEnd("case", ESAC)
	cs.Esac = p.lpos
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
	p.wantFollowStmt(`"foo()"`, &fd.Body, false)
	return
}
