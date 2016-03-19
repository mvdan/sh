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

var quote = map[rune]bool{
	'"':  true,
	'\'': true,
	'`':  true,
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
	r := ' '
	for space[r] {
		var err error
		if r, err = p.readRune(); err != nil {
			return
		}
	}
	p.pos = p.npos
	p.pos.col--
	switch {
	case r == '\\' && p.readOnly('\n'):
		p.next()
	case reserved[r] || starters[r]:
		switch r {
		case '#':
			p.advance(COMMENT, p.readLine())
		case '\n':
			p.advance('\n', "")
		default:
			p.advance(p.doToken(r), "")
		}
	default:
		p.advance(WORD, p.readWord(r))
	}
}

func (p *parser) readWord(r rune) string {
	var rs []rune
	var q rune
runeLoop:
	for {
		appendRune := true
		switch {
		case q != '\'' && r == '\\': // escaped rune
			r, _ = p.readRune()
			if r != '\n' {
				rs = append(rs, '\\', r)
			}
			appendRune = false
		case q != '\'' && r == '$': // $ continuation
			rs = append(rs, '$')
			switch {
			case p.readOnly('{'):
				rs = append(rs, '{')
				rs = append(rs, p.readIncluding('}')...)
			case p.readOnly('('):
				rs = append(rs, '(')
				rs = append(rs, p.readIncluding(')')...)
			}
			appendRune = false
		case q != 0: // rest of quoted cases
			if r == q {
				q = 0
			}
		case quote[r]: // start of a quoted string
			q = r
		case reserved[r] || space[r]: // end of word
			p.unreadRune()
			break runeLoop
		}
		if appendRune {
			rs = append(rs, r)
		}
		var err error
		if r, err = p.readRune(); err != nil {
			if q != 0 {
				p.errWanted(Token(q))
			}
			break
		}
	}
	return string(rs)
}

func (p *parser) doToken(r rune) Token {
	switch r {
	case '&':
		if p.readOnly('&') {
			return LAND
		}
		return AND
	case '|':
		if p.readOnly('|') {
			return LOR
		}
		return OR
	case '(':
		return LPAREN
	case '{':
		return LBRACE
	case ')':
		return RPAREN
	case '}':
		return RBRACE
	case ';':
		if p.readOnly(';') {
			return DSEMICOLON
		}
		return SEMICOLON
	case '<':
		return LSS
	case '>':
		if p.readOnly('>') {
			return SHR
		}
		return GTR
	default:
		return Token(r)
	}
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

func (p *parser) readUntil(delim rune) ([]rune, bool) {
	var rs []rune
	for {
		r, err := p.readRune()
		if err != nil {
			return rs, false
		}
		if r == delim {
			return rs, true
		}
		rs = append(rs, r)
	}
}

func (p *parser) readIncluding(delim rune) []rune {
	rs, found := p.readUntil(delim)
	if !found {
		p.errWanted(Token(delim))
	}
	return append(rs, delim)
}

func (p *parser) readLine() string {
	rs, _ := p.readUntil('\n')
	return string(rs)
}

// We can't simply have these as tokens as they can sometimes be valid
// words, e.g. `echo if`.
var reservedWords = map[Token]string{
	IF:    "if",
	THEN:  "then",
	ELIF:  "elif",
	ELSE:  "else",
	FI:    "fi",
	WHILE: "while",
	DO:    "do",
	DONE:  "done",
}

func (p *parser) peek(tok Token) bool {
	return p.tok == tok || (p.tok == WORD && p.val == reservedWords[tok])
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
	p.posErr(p.pos, format, v...)
}

func (p *parser) errWantedStr(s string) {
	if p.tok == EOF {
		p.pos = p.npos
	}
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

func (p *parser) commands(stop ...Token) (count int) {
	for p.tok != EOF {
		for _, tok := range stop {
			if p.peek(tok) {
				return
			}
		}
		p.command()
		count++
	}
	return
}

func (p *parser) command() {
	switch {
	case p.got(COMMENT):
		p.add(Comment{
			Text: p.lval,
		})
	case p.got('\n'), p.got(COMMENT):
		if p.tok != EOF {
			p.command()
		}
	case p.got(LPAREN):
		var sub Subshell
		p.push(&sub.Stmts)
		if p.commands(RPAREN) == 0 {
			p.errWantedStr("command")
		}
		p.want(RPAREN)
		p.popAdd(sub)
	case p.got(LBRACE):
		var bl Block
		p.push(&bl.Stmts)
		if p.commands(RBRACE) == 0 {
			p.errWantedStr("command")
		}
		p.want(RBRACE)
		p.popAdd(bl)
	case p.got(IF):
		var ifs IfStmt
		p.push(&ifs.Cond)
		p.command()
		p.pop()
		p.want(THEN)
		p.push(&ifs.ThenStmts)
		p.commands(FI, ELIF, ELSE)
		p.pop()
		p.push(&ifs.Elifs)
		for p.got(ELIF) {
			var elf Elif
			p.push(&elf.Cond)
			p.command()
			p.pop()
			p.want(THEN)
			p.push(&elf.ThenStmts)
			p.commands(FI, ELIF, ELSE)
			p.popAdd(elf)
		}
		if p.got(ELSE) {
			p.pop()
			p.push(&ifs.ElseStmts)
			p.commands(FI)
		}
		p.want(FI)
		p.popAdd(ifs)
	case p.got(WHILE):
		var whl WhileStmt
		p.push(&whl.Cond)
		p.command()
		p.pop()
		p.want(DO)
		p.push(&whl.DoStmts)
		p.commands(DONE)
		p.want(DONE)
		p.popAdd(whl)
	case p.got(WORD):
		var cmd Command
		p.push(&cmd.Args)
		p.add(Lit{Val: p.lval})
		fpos := p.lpos
		fval := p.lval
		if p.got(LPAREN) {
			p.want(RPAREN)
			if !identRe.MatchString(fval) {
				p.posErr(fpos, "invalid func name %q", fval)
			}
			fun := FuncDecl{
				Name: Lit{Val: fval},
			}
			p.push(&fun.Body)
			p.command()
			p.pop()
			p.popAdd(fun)
			return
		}
	args:
		for p.tok != EOF {
			switch {
			case p.got(WORD):
				p.add(Lit{Val: p.lval})
			case p.got(LAND):
				p.binaryExpr(LAND, cmd)
				return
			case p.got(OR):
				p.binaryExpr(OR, cmd)
				return
			case p.got(LOR):
				p.binaryExpr(LOR, cmd)
				return
			case p.gotRedirect():
			case p.got(AND):
				cmd.Background = true
				fallthrough
			case p.got(SEMICOLON), p.got('\n'):
				break args
			default:
				p.errAfterStr("command")
			}
		}
		p.popAdd(cmd)
	default:
		p.errWantedStr("command")
	}
}

func (p *parser) binaryExpr(op Token, left Node) {
	b := BinaryExpr{Op: op}
	p.push(&b.Y)
	p.command()
	p.pop()
	b.X = left
	p.popAdd(b)
}

func (p *parser) gotRedirect() bool {
	var r Redirect
	switch {
	case p.got(GTR):
		r.Op = GTR
		if p.got(AND) {
			p.want(WORD)
			r.Obj = Lit{Val: "&" + p.lval}
		} else {
			p.want(WORD)
			r.Obj = Lit{Val: p.lval}
		}
	case p.got(SHR):
		r.Op = SHR
		p.want(WORD)
		r.Obj = Lit{Val: p.lval}
	case p.got(LSS):
		r.Op = LSS
		p.want(WORD)
		r.Obj = Lit{Val: p.lval}
	default:
		return false
	}
	p.add(r)
	return true
}
