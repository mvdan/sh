// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
)

func Parse(r io.Reader, name string) (Prog, error) {
	p := &parser{
		r:    bufio.NewReader(r),
		name: name,
		npos: position{
			line: 1,
			col:  1,
		},
	}
	p.push(&p.prog.Stmts)
	p.next()
	p.program()
	return p.prog, p.err
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
	bpos position

	lpos position
	pos  position
	npos position

	prog  Prog
	stack []interface{}

	// to not include ')' in a literal
	quotedCmdSubst bool
}

type position struct {
	line int
	col  int
}

var reserved = map[rune]bool{
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
var starters = map[rune]bool{
	'{': true,
	'}': true,
	'#': true,
}

var space = map[rune]bool{
	' ':  true,
	'\t': true,
}

var identRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func (p *parser) readRune() (rune, error) {
	r, _, err := p.r.ReadRune()
	if err != nil {
		if err == io.EOF {
			p.setEOF()
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

func (p *parser) readOnly(wanted rune) bool {
	// Don't use our read/unread wrappers to avoid unnecessary
	// position movement and unwanted calls to p.eof()
	r, _, err := p.r.ReadRune()
	if r == wanted {
		p.moveWith(r)
		return true
	}
	if err == nil {
		p.r.UnreadRune()
	}
	return false
}

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
				p.advance(LIT, p.readLit(r))
				return
			}
		}
		switch r {
		case '#':
			p.advance(COMMENT, p.readLine())
		case '\n':
			p.advance('\n', "")
		default:
			p.advance(doToken(r, p.readOnly), "")
		}
	default:
		p.advance(LIT, p.readLit(r))
	}
}

func (p *parser) readLit(r rune) string {
	return string(p.readLitRunes(r))
}

func (p *parser) readLitRunes(r rune) (rs []rune) {
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
				p.errWanted(Token(p.quote))
			}
			break
		}
	}
	return
}

func (p *parser) advance(tok Token, val string) {
	if p.tok != EOF {
		p.ltok = p.tok
		p.lval = p.val
	}
	p.tok = tok
	p.val = val
}

func (p *parser) setEOF() {
	p.advance(EOF, "EOF")
}

func (p *parser) readUntil(tok Token) (string, bool) {
	var rs []rune
	for {
		r, err := p.readRune()
		if err != nil {
			return string(rs), false
		}
		if tok == doToken(r, p.readOnly) {
			return string(rs), true
		}
		rs = append(rs, r)
	}
}

func (p *parser) readUntilWant(tok Token) string {
	s, found := p.readUntil(tok)
	if !found {
		p.errWanted(tok)
	}
	return s
}

func (p *parser) readLine() string {
	s, _ := p.readUntil('\n')
	return s
}

// We can't simply have these as tokens as they can sometimes be valid
// words, e.g. `echo if`.
var reservedLits = map[Token]string{
	IF:    "if",
	THEN:  "then",
	ELIF:  "elif",
	ELSE:  "else",
	FI:    "fi",
	WHILE: "while",
	FOR:   "for",
	IN:    "in",
	DO:    "do",
	DONE:  "done",
	CASE:  "case",
	ESAC:  "esac",
}

func (p *parser) peek(tok Token) bool {
	return p.tok == tok || (p.tok == LIT && p.val == reservedLits[tok])
}

func (p *parser) got(tok Token) bool {
	if p.peek(tok) {
		p.next()
		return true
	}
	return false
}

func (p *parser) want(tok Token) {
	if !p.peek(tok) {
		p.errWanted(tok)
		return
	}
	p.next()
}

func (p *parser) errPass(err error) {
	if p.err == nil {
		p.err = err
	}
	p.setEOF()
}

type lineErr struct {
	fname string
	pos   position
	text  string
}

func (e lineErr) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", e.fname, e.pos.line, e.pos.col, e.text)
}

func (p *parser) posErr(pos position, format string, v ...interface{}) {
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

func (p *parser) errWantedStr(s string) {
	p.curErr("unexpected token %s - wanted %s", p.tok, s)
}

func (p *parser) errWanted(tok Token) {
	p.errWantedStr(tok.String())
}

func (p *parser) errAfterStr(s string) {
	p.curErr("unexpected token %s after %s", p.tok, s)
}

func (p *parser) add(n Node) {
	cur := p.stack[len(p.stack)-1]
	switch x := cur.(type) {
	case *[]Node:
		*x = append(*x, n)
	case *Node:
		if *x != nil {
			panic("single node set twice")
		}
		*x = n
	default:
		panic("unknown type in the stack")
	}
}

func (p *parser) pop() {
	p.stack = p.stack[:len(p.stack)-1]
}

func (p *parser) push(v interface{}) {
	p.stack = append(p.stack, v)
}

func (p *parser) popAdd(n Node) {
	p.pop()
	p.add(n)
}

func (p *parser) program() {
	p.commands()
}

func (p *parser) commands(stop ...Token) int {
	return p.commandsPropagating(false, stop...)
}

func (p *parser) commandsLimited(stop ...Token) int {
	return p.commandsPropagating(true, stop...)
}

func (p *parser) commandsPropagating(propagate bool, stop ...Token) (count int) {
	var cmdStop []Token
	if propagate {
		cmdStop = stop
	}
	for p.tok != EOF {
		for _, tok := range stop {
			if p.peek(tok) {
				return
			}
		}
		if !p.gotCommand(cmdStop...) && p.tok != EOF {
			p.errWantedStr("command")
			break
		}
		count++
	}
	return
}

func (p *parser) word() {
	var w Word
	p.push(&w.Parts)
	if p.readParts() == 0 {
		p.errWantedStr("word")
	}
	p.popAdd(w)
}

func (p *parser) readParts() (count int) {
	for p.tok != EOF {
		switch {
		case p.quote == 0 && count > 0 && p.spaced:
			return
		case p.got(LIT):
			p.add(Lit{Val: p.lval})
		case p.quote == 0 && p.peek('"'):
			var dq DblQuoted
			p.quote = '"'
			p.next()
			p.push(&dq.Parts)
			p.readParts()
			p.pop()
			p.quote = 0
			p.want('"')
			p.add(dq)
		case p.got(EXP):
			switch {
			case p.peek(LBRACE):
				p.add(ParamExp{Text: p.readUntilWant(RBRACE)})
				p.next()
			case p.got(LIT):
				p.add(ParamExp{Short: true, Text: p.lval})
			case p.peek(DLPAREN):
				p.add(ArithmExp{Text: p.readUntilWant(DRPAREN)})
				p.next()
			case p.peek(LPAREN):
				var cs CmdSubst
				p.push(&cs.Stmts)
				p.quotedCmdSubst = p.quote == '"'
				p.next()
				p.commandsLimited(RPAREN)
				p.quotedCmdSubst = false
				p.popAdd(cs)
				p.want(RPAREN)
			}
		default:
			return
		}
		count++
	}
	return
}

func (p *parser) wordList() (count int) {
	var stop = [...]Token{SEMICOLON, '\n'}
	for p.tok != EOF {
		for _, tok := range stop {
			if p.got(tok) {
				return
			}
		}
		p.word()
		count++
	}
	return
}

func (p *parser) gotCommand(stop ...Token) bool {
	gotEnd := func() bool {
		if p.tok == EOF || p.got(SEMICOLON) || p.got('\n') || p.peek(COMMENT) {
			return true
		}
		for _, tok := range stop {
			if p.peek(tok) {
				return true
			}
		}
		return false
	}
	switch {
	case p.got(COMMENT), p.got('\n'):
		if p.tok == EOF {
			return false
		}
		return p.gotCommand(stop...)
	case p.got(LPAREN):
		p.subshellCont(stop...)
	case p.got(LBRACE):
		p.blockCont(stop...)
	case p.got(IF):
		p.ifCont(stop...)
	case p.got(WHILE):
		p.whileCont(stop...)
	case p.got(FOR):
		p.forCont(stop...)
	case p.got(CASE):
		p.caseCont(stop...)
	case p.peek(LIT), p.peek(EXP), p.peek('\''), p.peek('"'):
		var cmd Command
		p.push(&cmd.Args)
		fpos := p.pos
		p.word()
		if p.got(LPAREN) {
			p.want(RPAREN)
			fname := cmd.Args[0].String()
			if !identRe.MatchString(fname) {
				p.posErr(fpos, "invalid func name: %s", fname)
			}
			fun := FuncDecl{
				Name: Lit{Val: fname},
			}
			p.push(&fun.Body)
			p.command(stop...)
			p.pop()
			p.popAdd(fun)
			return true
		}
	args:
		for !gotEnd() {
			switch {
			case p.peek(LIT), p.peek(EXP), p.peek('\''), p.peek('"'):
				p.word()
			case p.got(LAND), p.got(OR), p.got(LOR):
				p.binaryExpr(p.ltok, cmd, stop...)
				return true
			case p.gotRedirect():
			case p.got(AND):
				cmd.Background = true
				break args
			default:
				p.errAfterStr("command")
			}
		}
		p.popAdd(cmd)
		return true
	default:
		return false
	}
	if !gotEnd() {
		p.errAfterStr("statement")
	}
	return true
}

func (p *parser) command(stop ...Token) {
	if !p.gotCommand(stop...) {
		p.errWantedStr("command")
	}
}

func (p *parser) binaryExpr(op Token, left Node, stop ...Token) {
	b := BinaryExpr{Op: op}
	p.push(&b.Y)
	p.command(stop...)
	p.pop()
	b.X = left
	p.popAdd(b)
}

func (p *parser) gotRedirect() bool {
	var r Redirect
	p.push(&r.Obj)
	switch {
	case p.got(RDROUT), p.got(APPEND), p.got(RDRIN):
		r.Op = p.ltok
		p.word()
	default:
		p.pop()
		return false
	}
	p.popAdd(r)
	return true
}

func (p *parser) subshellCont(stop ...Token) {
	var sub Subshell
	p.push(&sub.Stmts)
	if p.commandsLimited(append(stop, RPAREN)...) == 0 {
		p.errWantedStr("command")
	}
	p.want(RPAREN)
	p.popAdd(sub)
}

func (p *parser) blockCont(stop ...Token) {
	var bl Block
	p.push(&bl.Stmts)
	if p.commands(append(stop, RBRACE)...) == 0 {
		p.errWantedStr("command")
	}
	p.want(RBRACE)
	p.popAdd(bl)
}

func (p *parser) ifCont(stop ...Token) {
	var ifs IfStmt
	p.push(&ifs.Cond)
	if !p.gotCommand(stop...) {
		p.curErr(`"if" must be followed by a command`)
	}
	p.pop()
	if !p.got(THEN) {
		p.curErr(`"if x" must be followed by "then"`)
	}
	p.push(&ifs.ThenStmts)
	p.commands(append(stop, FI, ELIF, ELSE)...)
	p.pop()
	p.push(&ifs.Elifs)
	for p.got(ELIF) {
		var elf Elif
		p.push(&elf.Cond)
		if !p.gotCommand(stop...) {
			p.curErr(`"elif" must be followed by a command`)
		}
		p.pop()
		if !p.got(THEN) {
			p.curErr(`"elif x" must be followed by "then"`)
		}
		p.push(&elf.ThenStmts)
		p.commands(append(stop, FI, ELIF, ELSE)...)
		p.popAdd(elf)
	}
	if p.got(ELSE) {
		p.pop()
		p.push(&ifs.ElseStmts)
		p.commands(append(stop, FI)...)
	}
	if !p.got(FI) {
		p.curErr(`if statement must end with a "fi"`)
	}
	p.popAdd(ifs)
}

func (p *parser) whileCont(stop ...Token) {
	var whl WhileStmt
	p.push(&whl.Cond)
	p.command(stop...)
	p.pop()
	p.want(DO)
	p.push(&whl.DoStmts)
	p.commands(append(stop, DONE)...)
	p.want(DONE)
	p.popAdd(whl)
}

func (p *parser) forCont(stop ...Token) {
	var fr ForStmt
	p.want(LIT)
	fr.Name = Lit{Val: p.lval}
	p.want(IN)
	p.push(&fr.WordList)
	p.wordList()
	p.pop()
	p.want(DO)
	p.push(&fr.DoStmts)
	p.commands(append(stop, DONE)...)
	p.want(DONE)
	p.popAdd(fr)
}

func (p *parser) caseCont(stop ...Token) {
	var cs CaseStmt
	p.push(&cs.Word)
	p.word()
	p.pop()
	p.want(IN)
	p.push(&cs.Patterns)
	p.patterns(stop...)
	p.want(ESAC)
	p.popAdd(cs)
}

func (p *parser) patterns(stop ...Token) {
	count := 0
	for p.tok != EOF && !p.peek(ESAC) {
		for p.got('\n') {
		}
		var cp CasePattern
		p.push(&cp.Parts)
		for p.tok != EOF {
			p.word()
			if p.got(RPAREN) {
				break
			}
			p.want(OR)
		}
		p.pop()
		p.push(&cp.Stmts)
		p.commandsLimited(append(stop, DSEMICOLON, ESAC)...)
		p.popAdd(cp)
		count++
		if !p.got(DSEMICOLON) {
			break
		}
		for p.got('\n') {
		}
	}
	if count == 0 {
		p.errWantedStr("pattern")
	}
}
