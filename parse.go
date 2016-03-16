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
	"unicode/utf8"
)

const (
	_ = -iota
	EOF
	WORD
	IF
	THEN
	ELIF
	ELSE
	FI
	WHILE
	DO
	DONE
)

func parse(r io.Reader, name string) (prog, error) {
	p := &parser{
		r:    bufio.NewReader(r),
		name: name,
		line: 1,
		col:  0,
	}
	p.push(&p.prog.stmts)
	p.next()
	p.program()
	return p.prog, p.err
}

type parser struct {
	r   *bufio.Reader
	err error

	tok  int32
	lval string
	val  string

	name string
	line int
	col  int

	prog  prog
	stack []interface{}
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
	r, _, err := p.r.ReadRune()
	if err != nil {
		if err == io.EOF {
			p.col++
			p.eof()
		} else {
			p.errPass(err)
		}
		return 0, err
	}
	p.col++
	return r, nil
}

func (p *parser) unreadRune() error {
	if err := p.r.UnreadRune(); err != nil {
		p.errPass(err)
		return err
	}
	return nil
}

func (p *parser) next() {
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
	if r == '\\' {
		p.next()
		if p.got('\n') {
			return
		}
		if err := p.unreadRune(); err != nil {
			return
		}
		p.col--
	}
	if reserved[r] || starters[r] {
		switch r {
		case '#':
			p.discardUpTo('\n')
			com := comment{
				text: p.val,
			}
			p.add(com)
			p.next()
			return
		case '\n':
			p.line++
			p.col = 0
			p.tok = '\n'
			return
		default:
			p.tok = r
			return
		}
	}
	if quote[r] {
		p.strContent(byte(r))
		if p.tok == EOF {
			return
		}
		p.col--
		p.tok = WORD
		return
	}
	rs := []rune{r}
	for !reserved[r] && !quote[r] && !space[r] {
		r, err = p.readRune()
		if err == io.EOF {
			break
		}
		if err != nil {
			return
		}
		rs = append(rs, r)
	}
	if err != io.EOF && len(rs) > 1 {
		if err := p.unreadRune(); err != nil {
			return
		}
		rs = rs[:len(rs)-1]
	}
	p.col--
	p.tok = WORD
	p.val = string(rs)
	return
}

func (p *parser) eof() {
	p.tok = EOF
	p.val = "EOF"
}

func (p *parser) strContent(delim byte) {
	v := []string{string(delim)}
	p.col++
	for {
		s, err := p.r.ReadString(delim)
		for _, r := range s {
			if r == '\n' {
				p.line++
				p.col = 0
			} else {
				p.col++
			}
		}
		if err == io.EOF {
			p.eof()
			p.errWanted(rune(delim))
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
		p.col++
	} else if err != nil {
		p.errPass(err)
	} else {
		cont = cont[:len(b)-1]
	}
	p.val = string(cont)
	p.col += utf8.RuneCount(b)
}

func (p *parser) peek(tok int32) bool {
	return p.tok == tok || (p.tok == WORD && p.val == tokStrs[tok])
}

func (p *parser) got(tok int32) bool {
	if p.peek(tok) {
		p.next()
		return true
	}
	return false
}

func (p *parser) want(tok int32) {
	if !p.peek(tok) {
		p.errWanted(tok)
		return
	}
	p.next()
}

var tokStrs = map[int32]string{
	IF:    "if",
	THEN:  "then",
	ELIF:  "elif",
	ELSE:  "else",
	FI:    "fi",
	WHILE: "while",
	DO:    "do",
	DONE:  "done",
}

var tokNames = map[int32]string{
	EOF:  `EOF`,
	WORD: `word`,

	IF:    `"if"`,
	THEN:  `"then"`,
	ELIF:  `"elif"`,
	ELSE:  `"else"`,
	FI:    `"fi"`,
	WHILE: `"while"`,
	DO:    `"do"`,
	DONE:  `"done"`,
}

func tokName(tok int32) string {
	if s, e := tokNames[tok]; e {
		return s
	}
	return strconv.QuoteRune(tok)
}

func (p *parser) errPass(err error) {
	if p.err == nil {
		p.err = err
	}
	p.eof()
}

func (p *parser) lineErr(format string, v ...interface{}) {
	pos := fmt.Sprintf("%s:%d:%d: ", p.name, p.line, p.col)
	p.errPass(fmt.Errorf(pos+format, v...))
}

func (p *parser) errWantedStr(s string) {
	p.lineErr("unexpected token %s, wanted %s", tokName(p.tok), s)
}

func (p *parser) errWanted(tok int32) {
	p.errWantedStr(tokName(tok))
}

func (p *parser) errAfterStr(s string) {
	p.lineErr("unexpected token %s after %s", tokName(p.tok), s)
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
	case p.got('\n'):
		if !p.peek(EOF) {
			p.command()
		}
	case p.got('('):
		var sub subshell
		p.push(&sub.stmts)
		count := 0
		for p.tok != EOF && !p.peek(')') {
			p.command()
			count++
		}
		if count == 0 {
			p.errWantedStr("command")
		}
		p.want(')')
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
	args:
		for p.tok != EOF {
			switch {
			case p.got(WORD):
				p.add(lit{val: p.lval})
			case p.got('&'):
				if p.got('&') {
					b := binaryExpr{op: "&&"}
					p.push(&b.Y)
					p.command()
					p.pop()
					p.push(&b.X)
					p.add(cmd)
					p.pop()
					p.popAdd(b)
					return
				}
				break args
			case p.got('|'):
				b := binaryExpr{op: "|"}
				if p.got('|') {
					b.op = "||"
				}
				p.push(&b.Y)
				p.command()
				p.pop()
				p.push(&b.X)
				p.add(cmd)
				p.pop()
				p.popAdd(b)
				return
			case p.got('('):
				if !ident.MatchString(p.lval) {
					p.col -= utf8.RuneCountInString(p.lval)
					p.col--
					p.lineErr("invalid func name %q", p.lval)
					break args
				}
				fun := funcDecl{
					name: lit{val: p.lval},
				}
				p.want(')')
				p.push(&fun.body)
				p.command()
				p.pop()
				p.popAdd(fun)
				return
			case p.peek('>'), p.peek('<'):
				p.redirect()
			case p.got(';'):
				break args
			case p.got('\n'):
				break args
			default:
				p.errAfterStr("command")
			}
		}
		p.popAdd(cmd)
	case p.got('{'):
		var bl block
		p.push(&bl.stmts)
		for p.tok != EOF && !p.peek('}') {
			// TODO: remove? breaks "{\n}"
			if p.got('\n') {
				continue
			}
			p.command()
		}
		p.want('}')
		p.popAdd(bl)
		if p.tok != EOF {
			switch {
			case p.got('&'):
				if p.got('&') {
					p.command()
				}
			case p.got('|'):
				p.got('|')
				p.command()
			case p.peek('>'), p.peek('<'):
				p.redirect()
			case p.got(';'):
			case p.got('\n'):
			default:
				p.errAfterStr("block")
			}
		}
	default:
		p.errWantedStr("command")
	}
}

func (p *parser) redirect() {
	var r redirect
	switch {
	case p.got('>'):
		r.op = ">"
		switch {
		case p.got('&'):
			p.want(WORD)
			if !num.MatchString(p.lval) {
				p.col -= utf8.RuneCountInString(p.lval)
				p.col++
				p.lineErr("invalid fd %q", p.lval)
			}
			r.obj = lit{val: "&" + p.lval}
		case p.got('>'):
			r.op = ">>"
			fallthrough
		default:
			p.want(WORD)
			r.obj = lit{val: p.lval}
		}
	case p.got('<'):
		r.op = "<"
		p.want(WORD)
		r.obj = lit{val: p.lval}
	}
	p.add(r)
}
