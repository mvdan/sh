// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"unicode/utf8"
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
	'#':  true,
	'&':  true,
	'>':  true,
	'<':  true,
	'|':  true,
	';':  true,
	'(':  true,
	')':  true,
}

// like reserved, but these can appear in the middle of a word
var starters = map[rune]bool{
	'{': true,
	'}': true,
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
}

func (p *parser) readOnly(wanted rune) bool {
	r, _, err := p.r.ReadRune()
	if r == wanted {
		p.moveWith(r)
		return true
	}
	if err == nil {
		p.unreadRune()
	}
	return false
}

func (p *parser) next() {
	p.lpos = p.pos
	r := ' '
	var err error
	for space[r] {
		r, err = p.readRune()
		if err != nil {
			return
		}
	}
	p.pos = p.npos
	p.pos.col--
	if r == '\\' && p.readOnly('\n') {
		p.next()
		return
	}
	p.lval = p.val
	if reserved[r] || starters[r] {
		switch r {
		case '#':
			p.advance(COMMENT, p.readUpTo('\n'))
		case '\n':
			p.advance('\n', "")
		default:
			p.advance(p.doToken(r), "")
		}
		return
	}
	rs := []rune{r}
	q := rune(0)
	if r == '"' || r == '\'' {
		q = r
	}
	for {
		r, err = p.readRune()
		if err != nil {
			if q != 0 {
				p.errWanted(Token(q))
			}
			break
		}
		if q != 0 {
			if q == '"' && r == '\\' {
				rs = append(rs, r)
				r, _ = p.readRune()
			} else if r == q {
				q = 0
			}
		} else if r == '"' || r == '\'' {
			q = r
		} else if reserved[r] || space[r] {
			p.npos.col--
			p.unreadRune()
			break
		}
		rs = append(rs, r)
	}
	p.setTok(WORD)
	p.val = string(rs)
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
	p.setTok(tok)
	p.lval = p.val
	p.val = val
}

func (p *parser) setTok(tok Token) {
	p.ltok = p.tok
	p.tok = tok
}

func (p *parser) setEOF() {
	p.advance(EOF, "EOF")
}

func (p *parser) readUpTo(delim byte) string {
	b, err := p.r.ReadBytes(delim)
	cont := b
	if err == io.EOF {
	} else if err != nil {
		p.errPass(err)
	} else {
		cont = cont[:len(b)-1]
	}
	p.npos.col += utf8.RuneCount(b)
	return string(cont)
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

func (p *parser) posErr(pos position, format string, v ...interface{}) {
	prefix := fmt.Sprintf("%s:%d:%d: ", p.name, pos.line, pos.col)
	p.errPass(fmt.Errorf(prefix+format, v...))
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
		com := Comment{
			Text: p.lval,
		}
		p.add(com)
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
	args:
		for p.tok != EOF {
			switch {
			case p.got(WORD):
				p.add(Lit{Val: p.lval})
			case p.got(AND):
				cmd.Background = true
				break args
			case p.got(LAND):
				p.binaryExpr(LAND, cmd)
				return
			case p.got(OR):
				p.binaryExpr(OR, cmd)
				return
			case p.got(LOR):
				p.binaryExpr(LOR, cmd)
				return
			case p.got(LPAREN):
				p.want(RPAREN)
				if !identRe.MatchString(fval) {
					p.posErr(fpos, "invalid func name %q", fval)
					break args
				}
				fun := FuncDecl{
					Name: Lit{Val: fval},
				}
				p.push(&fun.Body)
				p.command()
				p.pop()
				p.popAdd(fun)
				return
			case p.gotRedirect():
			case p.got(SEMICOLON):
				break args
			case p.got('\n'):
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
