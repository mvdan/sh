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

func Parse(r io.Reader, name string) (Prog, error) {
	p := &parser{
		r:    bufio.NewReader(r),
		name: name,
		npos: Position{
			line: 1,
			col:  1,
		},
		stops: [][]Token{nil},
	}
	p.next()
	prog := p.program()
	return prog, p.err
}

type parser struct {
	r    *bufio.Reader
	name string

	err error

	spaced bool
	quote  rune

	ltok Token
	tok  Token
	lval string
	val  string

	// backup position to unread a rune
	bpos Position

	lpos Position
	pos  Position
	npos Position

	stops [][]Token

	// to not include ')' in a literal
	quotedCmdSubst bool
}

type Position struct {
	line int
	col  int
}

func (p *parser) readRune() (rune, error) {
	r, _, err := p.r.ReadRune()
	if err != nil {
		if err == io.EOF {
			p.advanceTok(EOF)
		} else {
			p.errPass(err)
		}
		return 0, err
	}
	p.moveWith(r)
	return r, nil
}

func (p *parser) moveWith(r rune) {
	p.bpos = p.npos
	if r == '\n' {
		p.npos.line++
		p.npos.col = 1
	} else {
		p.npos.col++
	}
}

func (p *parser) unreadRune() {
	if err := p.r.UnreadRune(); err != nil {
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
		p.readRune()
		return true
	}
	return false
}

var (
	reserved = map[rune]bool{
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
	starters = map[rune]bool{
		'{': true,
		'}': true,
		'#': true,
	}
	space = map[rune]bool{
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
	var r rune
	p.spaced = false
	for {
		var err error
		if r, err = p.readRune(); err != nil {
			return
		}
		if p.quote != 0 || !space[r] {
			break
		}
		p.spaced = true
	}
	p.pos = p.npos
	p.pos.col--
	switch {
	case r == '\\' && p.readOnly('\n'):
		p.next()
	case reserved[r], starters[r]:
		// Between double quotes, only under certain
		// circumstnaces do we tokenize
		if p.quote == '"' {
			switch {
			case r == '"', r == '$':
			case p.tok == EXP:
			case r == ')' && p.quotedCmdSubst:
			default:
				p.advanceLit(p.readLit(r))
				return
			}
		}
		p.advanceTok(doToken(r, p.readOnly))
	default:
		p.advanceLit(p.readLit(r))
	}
}

func (p *parser) readLit(r rune) string { return string(p.readLitRunes(r)) }
func (p *parser) readLitRunes(r rune) (rs []rune) {
	var lpos Position
	for {
		appendRune := true
		switch {
		case p.quote != '\'' && r == '\\': // escaped rune
			r, _ = p.readRune()
			if r != '\n' {
				rs = append(rs, '\\', r)
			}
			appendRune = false
		case p.quote != '\'' && r == '$': // end of lit
			p.unreadRune()
			return
		case p.quote == '"':
			if r == p.quote || (p.quotedCmdSubst && r == ')') {
				p.unreadRune()
				return
			}
		case p.quote == '\'':
			if r == p.quote {
				p.quote = 0
			}
		case r == '\'':
			p.quote = '\''
			lpos = p.npos
			lpos.col--
		case reserved[r], space[r]: // end of lit
			p.unreadRune()
			return
		}
		if appendRune {
			rs = append(rs, r)
		}
		var err error
		if r, err = p.readRune(); err != nil {
			if p.quote != 0 {
				p.wantQuote(lpos, Token(p.quote))
			}
			break
		}
	}
	return
}

func (p *parser) advanceTok(tok Token)  { p.advanceBoth(tok, tok.String()) }
func (p *parser) advanceLit(val string) { p.advanceBoth(LIT, val) }
func (p *parser) advanceBoth(tok Token, val string) {
	if p.tok != EOF {
		p.ltok = p.tok
		p.lval = p.val
	}
	p.tok = tok
	p.val = val
}

func (p *parser) readUntil(closing Token) (string, bool) {
	var b bytes.Buffer
	for {
		r, err := p.readRune()
		if err != nil {
			return b.String(), false
		}
		tok := doToken(r, p.readOnly)
		if tok == closing {
			return b.String(), true
		}
		fmt.Fprint(&b, tok)
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
	var b bytes.Buffer
	for p.tok != EOF {
		l := p.readLine()
		if l == s {
			return b.String(), true
		}
		fmt.Fprintln(&b, l)
	}
	return b.String(), false
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

func (p *parser) wantFollowStmt(left string, s *Stmt) {
	if !p.gotStmt(s) {
		p.followErr(left, "a statement")
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
	return fmt.Sprintf("%s:%d:%d: %s", e.fname, e.pos.line, e.pos.col, e.text)
}

func (p *parser) posErr(pos Position, format string, v ...interface{}) {
	p.errPass(lineErr{
		fname: p.name,
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

func (p *parser) program() (pr Prog) {
	p.stmts(&pr.Stmts)
	return
}

func (p *parser) stmts(stmts *[]Stmt, stop ...Token) (count int) {
	for p.tok != EOF && !p.peekAny(stop...) {
		var s Stmt
		if !p.gotStmt(&s) && p.tok != EOF {
			if !p.peekAny(stop...) {
				p.invalidStmtStart()
			}
			break
		}
		if s.Node != nil {
			*stmts = append(*stmts, s)
		}
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

func (p *parser) stmtsLimited(stmts *[]Stmt, stop ...Token) int {
	p.stops = append(p.stops, stop)
	count := p.stmts(stmts, stop...)
	p.stops = p.stops[:len(p.stops)-1]
	return count
}

func (p *parser) gotWord(w *Word) bool {
	return p.readParts(&w.Parts) > 0
}

func (p *parser) gotLit(l *Lit) bool {
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
			p.next()
			lpos := p.lpos
			p.readParts(&dq.Parts)
			p.quote = 0
			p.wantQuote(lpos, '"')
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
	return p.peekAny(RDROUT, APPEND, RDRIN, HEREDOC, DPLIN, DPLOUT)
}

func (p *parser) gotStmt(s *Stmt) bool {
	for p.gotAny('#', '\n') {
		if p.ltok == '#' {
			p.readLine()
			p.next()
		}
	}
	addRedir := func() {
		s.Redirs = append(s.Redirs, p.redirect())
	}
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
	default:
		return false
	}
	for p.peekRedir() {
		addRedir()
	}
	if p.got(AND) {
		s.Background = true
	}
	if !p.peekStop() {
		p.curErr("statements must be separated by ; or a newline")
	}
	if p.gotAny(OR, LAND, LOR) {
		left := *s
		*s = Stmt{Node: p.binaryExpr(p.ltok, left)}
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
	b.Op = op
	p.wantFollowStmt(op.String(), &b.Y)
	b.X = left
	return
}

func (p *parser) redirect() (r Redirect) {
	p.gotLit(&r.N)
	r.Op = p.tok
	p.next()
	switch r.Op {
	case HEREDOC:
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
	lpos := p.lpos
	if p.stmtsLimited(&s.Stmts, RPAREN) < 1 {
		p.curErr("a subshell must contain one or more statements")
	}
	p.wantMatched(lpos, LPAREN)
	return
}

func (p *parser) block() (b Block) {
	lpos := p.lpos
	if p.stmts(&b.Stmts, RBRACE) < 1 {
		p.curErr("a block must contain one or more statements")
	}
	p.wantMatched(lpos, LBRACE)
	return
}

func (p *parser) ifStmt() (ifs IfStmt) {
	p.wantFollowStmt(`"if"`, &ifs.Cond)
	p.wantFollow(`"if x"`, THEN)
	p.stmts(&ifs.ThenStmts, FI, ELIF, ELSE)
	for p.got(ELIF) {
		var elf Elif
		p.wantFollowStmt(`"elif"`, &elf.Cond)
		p.wantFollow(`"elif x"`, THEN)
		p.stmts(&elf.ThenStmts, FI, ELIF, ELSE)
		ifs.Elifs = append(ifs.Elifs, elf)
	}
	if p.got(ELSE) {
		p.stmts(&ifs.ElseStmts, FI)
	}
	p.wantStmtEnd("if", FI)
	return
}

func (p *parser) whileStmt() (ws WhileStmt) {
	p.wantFollowStmt(`"while"`, &ws.Cond)
	p.wantFollow(`"while x"`, DO)
	p.stmts(&ws.DoStmts, DONE)
	p.wantStmtEnd("while", DONE)
	return
}

func (p *parser) forStmt() (fs ForStmt) {
	p.wantFollowLit(`"for"`, &fs.Name)
	p.wantFollow(`"for foo"`, IN)
	p.wordList(&fs.WordList)
	p.wantFollow(`"for foo in list"`, DO)
	p.stmts(&fs.DoStmts, DONE)
	p.wantStmtEnd("for", DONE)
	return
}

func (p *parser) caseStmt() (cs CaseStmt) {
	p.wantFollowWord(`"case"`, &cs.Word)
	p.wantFollow(`"case x"`, IN)
	if p.patLists(&cs.List) < 1 {
		p.followErr(`"case x in"`, "one or more patterns")
	}
	p.wantStmtEnd("case", ESAC)
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
	fpos := p.pos
	var w Word
	p.gotWord(&w)
	if p.got(LPAREN) {
		return p.funcDecl(w.String(), fpos)
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

func (p *parser) funcDecl(name string, pos Position) (fd FuncDecl) {
	if !p.got(RPAREN) {
		p.curErr(`functions must start like "foo()"`)
	}
	if !identRe.MatchString(name) {
		p.posErr(pos, "invalid func name: %s", name)
	}
	fd.Name.Value = name
	p.wantFollowStmt(`"foo()"`, &fd.Body)
	return
}
