// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

type token int

const (
	ILLEGAL token = -iota
	EOF
	COMMENT
	WORD

	IF
	THEN
	ELIF
	ELSE
	FI
	WHILE
	DO
	DONE

	AND
	LAND
	OR
	LOR

	LPAREN
	LBRACE

	RPAREN
	RBRACE
	SEMICOLON

	LSS
	GTR
)

func parse(r io.Reader, name string) (prog, error) {
	p := &parser{
		r:    bufio.NewReader(r),
		name: name,
		pos: position{
			line: 1,
			col:  0,
		},
		npos: position{
			line: 1,
			col:  1,
		},
	}
	p.push(&p.prog.stmts)
	p.next()
	p.program()
	return p.prog, p.err
}

type parser struct {
	r   *bufio.Reader
	err error

	tok  token
	lval string
	val  string

	name string
	lpos position
	pos  position
	npos position

	prog  prog
	stack []interface{}
}

type position struct {
	line int
	col  int
}

type node interface {
	fmt.Stringer
}

type lit struct {
	val string
}

func (l lit) String() string {
	return l.val
}

func nodeJoin(ns []node, sep string) string {
	var b bytes.Buffer
	for i, n := range ns {
		if i > 0 {
			io.WriteString(&b, sep)
		}
		io.WriteString(&b, n.String())
	}
	return b.String()
}

type prog struct {
	stmts []node
}

func (p prog) String() string {
	return nodeJoin(p.stmts, "; ")
}

type command struct {
	args []node
}

func (c command) String() string {
	nodes := make([]node, 0, len(c.args))
	for _, l := range c.args {
		nodes = append(nodes, l)
	}
	return nodeJoin(nodes, " ")
}

type redirect struct {
	op  string
	obj node
}

func (r redirect) String() string {
	return r.op + r.obj.String()
}

type subshell struct {
	stmts []node
}

func (s subshell) String() string {
	return "( " + nodeJoin(s.stmts, "; ") + "; )"
}

type block struct {
	stmts []node
}

func (b block) String() string {
	return "{ " + nodeJoin(b.stmts, "; ") + "; }"
}

type ifStmt struct {
	cond      node
	thenStmts []node
	elifs     []node
	elseStmts []node
}

func (s ifStmt) String() string {
	var b bytes.Buffer
	io.WriteString(&b, "if ")
	io.WriteString(&b, s.cond.String())
	io.WriteString(&b, "; then ")
	io.WriteString(&b, nodeJoin(s.thenStmts, "; "))
	for _, n := range s.elifs {
		e := n.(elif)
		io.WriteString(&b, "; ")
		io.WriteString(&b, e.String())
	}
	if len(s.elseStmts) > 0 {
		io.WriteString(&b, "; else ")
		io.WriteString(&b, nodeJoin(s.elseStmts, "; "))
	}
	io.WriteString(&b, "; fi")
	return b.String()
}

type elif struct {
	cond      node
	thenStmts []node
}

func (e elif) String() string {
	var b bytes.Buffer
	io.WriteString(&b, "elif ")
	io.WriteString(&b, e.cond.String())
	io.WriteString(&b, "; then ")
	io.WriteString(&b, nodeJoin(e.thenStmts, "; "))
	return b.String()
}

type whileStmt struct {
	cond    node
	doStmts []node
}

func (w whileStmt) String() string {
	var b bytes.Buffer
	io.WriteString(&b, "while ")
	io.WriteString(&b, w.cond.String())
	io.WriteString(&b, "; do ")
	io.WriteString(&b, nodeJoin(w.doStmts, "; "))
	io.WriteString(&b, "; done")
	return b.String()
}

type binaryExpr struct {
	X, Y node
	op   string
}

func (b binaryExpr) String() string {
	return fmt.Sprintf("%s %s %s", b.X, b.op, b.Y)
}

type comment struct {
	text string
}

func (c comment) String() string {
	return "#" + c.text
}

type funcDecl struct {
	name lit
	body node
}

func (f funcDecl) String() string {
	return fmt.Sprintf("%s() %s", f.name, f.body)
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

var quote = map[rune]bool{
	'"':  true,
	'\'': true,
}

var space = map[rune]bool{
	' ':  true,
	'\t': true,
}

var (
	ident = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	num   = regexp.MustCompile(`^[1-9][0-9]*$`)
)

func (p *parser) readRune() (rune, error) {
	if p.tok == EOF {
		return 0, io.EOF
	}
	r, _, err := p.r.ReadRune()
	if err != nil {
		if err == io.EOF {
			p.eof()
		} else {
			p.errPass(err)
		}
		return 0, err
	}
	p.npos.col++
	return r, nil
}

func (p *parser) unreadRune() {
	p.npos.col--
	if p.tok == EOF {
		return
	}
	if err := p.r.UnreadRune(); err != nil {
		panic(err)
	}
}

func (p *parser) readOnly(wanted rune) bool {
	r, _ := p.readRune()
	if r == wanted {
		return true
	}
	if p.tok != EOF {
		p.unreadRune()
	}
	return false
}

func (p *parser) next() {
	p.lpos = p.pos
	p.pos = p.npos
	p.lval = p.val
	if p.tok == EOF {
		return
	}
	r := ' '
	var err error
	for space[r] {
		r, err = p.readRune()
		if err != nil {
			return
		}
	}
	if r == '\\' && p.readOnly('\n') {
		p.next()
		return
	}
	if reserved[r] || starters[r] {
		switch r {
		case '#':
			p.discardUpTo('\n')
			com := comment{
				text: p.val,
			}
			p.add(com)
			p.tok = COMMENT
			return
		case '\n':
			p.npos.line++
			p.npos.col = 1
			p.tok = '\n'
			return
		}
		p.tok = p.doToken(r)
		return
	}
	if quote[r] {
		p.strContent(byte(r))
		if p.tok == EOF {
			return
		}
		p.tok = WORD
		return
	}
	rs := []rune{r}
	for {
		r, err = p.readRune()
		if err == io.EOF {
			break
		}
		if err != nil {
			return
		}
		if reserved[r] || quote[r] || space[r] {
			p.unreadRune()
			break
		}
		rs = append(rs, r)
	}
	p.tok = WORD
	p.val = string(rs)
	return
}

func (p *parser) doToken(r rune) token {
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
		return SEMICOLON
	case '<':
		return LSS
	case '>':
		return GTR
	default:
		return token(r)
	}
}

func (p *parser) eof() {
	p.tok = EOF
	p.val = "EOF"
}

func (p *parser) strContent(delim byte) {
	v := []string{string(delim)}
	for {
		s, err := p.r.ReadString(delim)
		for _, r := range s {
			if r == '\n' {
				p.npos.line++
				p.npos.col = 0
			} else {
				p.npos.col++
			}
		}
		if err == io.EOF {
			p.eof()
			p.errWanted(token(delim))
		} else if err != nil {
			p.errPass(err)
		}
		v = append(v, s)
		if delim == '\'' {
			break
		}
		if len(s) > 1 && s[len(s)-2] == '\\' && s[len(s)-1] == delim {
			continue
		}
		break
	}
	p.val = strings.Join(v, "")
}

func (p *parser) discardUpTo(delim byte) {
	b, err := p.r.ReadBytes(delim)
	cont := b
	if err == io.EOF {
		p.eof()
	} else if err != nil {
		p.errPass(err)
	} else {
		cont = cont[:len(b)-1]
	}
	p.val = string(cont)
	p.npos.col += utf8.RuneCount(b)
}

// We can't simply have these as tokens as they can sometimes be valid
// words, e.g. `echo if`.
var reservedWords = map[token]string{
	IF:    "if",
	THEN:  "then",
	ELIF:  "elif",
	ELSE:  "else",
	FI:    "fi",
	WHILE: "while",
	DO:    "do",
	DONE:  "done",
}

func (p *parser) peek(tok token) bool {
	return p.tok == tok || (p.tok == WORD && p.val == reservedWords[tok])
}

func (p *parser) got(tok token) bool {
	if p.peek(tok) {
		p.next()
		return true
	}
	return false
}

func (p *parser) want(tok token) {
	if !p.peek(tok) {
		p.errWanted(tok)
		return
	}
	p.next()
}

var tokNames = map[token]string{
	ILLEGAL: `ILLEGAL`,
	EOF:     `EOF`,
	COMMENT: `comment`,
	WORD:    `word`,

	IF:    "if",
	THEN:  "then",
	ELIF:  "elif",
	ELSE:  "else",
	FI:    "fi",
	WHILE: "while",
	DO:    "do",
	DONE:  "done",

	AND:  "&",
	LAND: "&&",
	OR:   "|",
	LOR:  "||",

	LPAREN: "(",
	LBRACE: "{",

	RPAREN:    ")",
	RBRACE:    "}",
	SEMICOLON: ";",

	LSS: "<",
	GTR: ">",
}

func (t token) String() string {
	if s, e := tokNames[t]; e {
		return s
	}
	return string(t)
}

func (p *parser) errPass(err error) {
	if p.err == nil {
		p.err = err
	}
	p.eof()
}

func (p *parser) posErr(pos position, format string, v ...interface{}) {
	if p.err != nil {
		return
	}
	prefix := fmt.Sprintf("%s:%d:%d: ", p.name, pos.line, pos.col)
	p.errPass(fmt.Errorf(prefix+format, v...))
}

func (p *parser) curErr(format string, v ...interface{}) {
	p.posErr(p.pos, format, v...)
}

func (p *parser) lastErr(format string, v ...interface{}) {
	p.posErr(p.lpos, format, v...)
}

func (p *parser) errWantedStr(s string) {
	if p.tok == EOF {
		p.pos = p.npos
	}
	p.curErr("unexpected token %s - wanted %s", p.tok, s)
}

func (p *parser) errWanted(tok token) {
	p.errWantedStr(tok.String())
}

func (p *parser) errAfterStr(s string) {
	if p.tok == EOF {
		p.pos = p.npos
	}
	p.curErr("unexpected token %s after %s", p.tok, s)
}

func (p *parser) add(n node) {
	cur := p.stack[len(p.stack)-1]
	switch x := cur.(type) {
	case *[]node:
		*x = append(*x, n)
	case *node:
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

func (p *parser) popAdd(n node) {
	p.pop()
	p.add(n)
}

func (p *parser) program() {
	for p.tok != EOF {
		p.command()
	}
}

func (p *parser) command() {
	switch {
	case p.got('\n'), p.got(COMMENT):
		if !p.peek(EOF) {
			p.command()
		}
	case p.got(LPAREN):
		var sub subshell
		p.push(&sub.stmts)
		count := 0
		for p.tok != EOF && !p.peek(RPAREN) {
			p.command()
			count++
		}
		if count == 0 {
			p.errWantedStr("command")
		}
		p.want(RPAREN)
		p.popAdd(sub)
	case p.got(IF):
		var ifs ifStmt
		p.push(&ifs.cond)
		p.command()
		p.pop()
		p.want(THEN)
		p.push(&ifs.thenStmts)
		for p.tok != EOF && !p.peek(FI) && !p.peek(ELIF) && !p.peek(ELSE) {
			p.command()
		}
		p.pop()
		p.push(&ifs.elifs)
		for p.got(ELIF) {
			var elf elif
			p.push(&elf.cond)
			p.command()
			p.pop()
			p.want(THEN)
			p.push(&elf.thenStmts)
			for p.tok != EOF && !p.peek(FI) && !p.peek(ELIF) && !p.peek(ELSE) {
				p.command()
			}
			p.popAdd(elf)
		}
		if p.got(ELSE) {
			p.pop()
			p.push(&ifs.elseStmts)
			for p.tok != EOF && !p.peek(FI) {
				p.command()
			}
		}
		p.want(FI)
		p.popAdd(ifs)
	case p.got(WHILE):
		var whl whileStmt
		p.push(&whl.cond)
		p.command()
		p.pop()
		p.want(DO)
		p.push(&whl.doStmts)
		for p.tok != EOF && !p.peek(DONE) {
			p.command()
		}
		p.want(DONE)
		p.popAdd(whl)
	case p.got(WORD):
		var cmd command
		p.push(&cmd.args)
		p.add(lit{val: p.lval})
		first := p.lpos
	args:
		for p.tok != EOF {
			switch {
			case p.got(WORD):
				p.add(lit{val: p.lval})
			case p.got(AND):
				break args
			case p.got(LAND):
				b := binaryExpr{op: "&&"}
				p.push(&b.Y)
				p.command()
				p.pop()
				p.push(&b.X)
				p.add(cmd)
				p.pop()
				p.popAdd(b)
				return
			case p.got(OR):
				p.binaryExpr("|", cmd)
				return
			case p.got(LOR):
				p.binaryExpr("||", cmd)
				return
			case p.got(LPAREN):
				if !ident.MatchString(p.lval) {
					p.posErr(first, "invalid func name %q", p.lval)
					break args
				}
				fun := funcDecl{
					name: lit{val: p.lval},
				}
				p.want(RPAREN)
				p.push(&fun.body)
				p.command()
				p.pop()
				p.popAdd(fun)
				return
			case p.peek(LSS), p.peek(GTR):
				p.redirect()
			case p.got(SEMICOLON):
				break args
			case p.got('\n'):
				break args
			case p.got(COMMENT):
			default:
				p.errAfterStr("command")
			}
		}
		p.popAdd(cmd)
	case p.got(LBRACE):
		var bl block
		p.push(&bl.stmts)
		for p.tok != EOF && !p.peek(RBRACE) {
			// TODO: remove? breaks "{\n}"
			if p.got('\n') {
				continue
			}
			p.command()
		}
		p.want(RBRACE)
		p.popAdd(bl)
		if p.tok != EOF {
			switch {
			case p.got(AND):
			case p.got(LAND):
				p.command()
			case p.got(OR):
				p.command()
			case p.got(LOR):
				p.command()
			case p.peek(LSS), p.peek(GTR):
				p.redirect()
			case p.got(SEMICOLON):
			case p.got('\n'):
			default:
				p.errAfterStr("block")
			}
		}
	default:
		p.errWantedStr("command")
	}
}

func (p *parser) binaryExpr(op string, left node) {
	b := binaryExpr{op: op}
	p.push(&b.Y)
	p.command()
	p.pop()
	p.push(&b.X)
	p.add(left)
	p.pop()
	p.popAdd(b)
}

func (p *parser) redirect() {
	var r redirect
	switch {
	case p.got(GTR):
		r.op = ">"
		switch {
		case p.got(AND):
			p.want(WORD)
			if !num.MatchString(p.lval) {
				p.lastErr("invalid fd %q", p.lval)
			}
			r.obj = lit{val: "&" + p.lval}
		case p.got(GTR):
			r.op = ">>"
			fallthrough
		default:
			p.want(WORD)
			r.obj = lit{val: p.lval}
		}
	case p.got(LSS):
		r.op = "<"
		p.want(WORD)
		r.obj = lit{val: p.lval}
	}
	p.add(r)
}
