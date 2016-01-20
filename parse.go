// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"bufio"
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
	ELSE
	FI
	WHILE
	DO
	DONE
)

func parse(r io.Reader, name string) error {
	p := &parser{
		r:    bufio.NewReader(r),
		name: name,
		line: 1,
		col:  0,
	}
	p.next()
	p.program()
	return p.err
}

type parser struct {
	r   *bufio.Reader
	err error

	tok int32
	val string

	name string
	line int
	col  int
}

var reserved = map[rune]bool{
	'\n': true,
	'#':  true,
	'=':  true,
	'&':  true,
	'>':  true,
	'<':  true,
	'|':  true,
	';':  true,
	'(':  true,
	')':  true,
	'{':  true,
	'}':  true,
	'$':  true,
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

func (p *parser) next() {
	r := ' '
	var err error
	for space[r] {
		r, _, err = p.r.ReadRune()
		if err == io.EOF {
			p.tok = EOF
			return
		}
		if err != nil {
			p.errPass(err)
			return
		}
		p.col++
	}
	if r == '\\' {
		p.next()
		if p.got('\n') {
			return
		}
		if err := p.r.UnreadRune(); err != nil {
			p.errPass(err)
			return
		}
		p.col--
	}
	if reserved[r] {
		switch r {
		case '#':
			p.discardUpTo('\n')
			p.next()
			return
		case '$':
			p.next()
			if p.got('{') {
				p.discardUpTo('}')
				p.tok = WORD
				return
			}
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
		p.tok = WORD
		return
	}
	rs := []rune{r}
	for !reserved[r] && !quote[r] && !space[r] {
		r, _, err = p.r.ReadRune()
		if err == io.EOF {
			break
		}
		if err != nil {
			p.errPass(err)
			return
		}
		p.col++
		rs = append(rs, r)
	}
	if err != io.EOF && len(rs) > 1 {
		if err := p.r.UnreadRune(); err != nil {
			p.errPass(err)
			return
		}
		p.col--
		rs = rs[:len(rs)-1]
	}
	p.tok = WORD
	p.val = string(rs)
	return
}

func (p *parser) strContent(delim byte) {
	v := []string{string(delim)}
	for {
		s, err := p.r.ReadString(delim)
		if err == io.EOF {
			p.tok = EOF
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
	for _, r := range p.val {
		if r == '\n' {
			p.line++
			p.col = 0
		} else {
			p.col++
		}
	}
}

func (p *parser) discardUpTo(delim byte) {
	b, err := p.r.ReadBytes(delim)
	if err == io.EOF {
		p.tok = EOF
	} else if err != nil {
		p.errPass(err)
	}
	p.col += utf8.RuneCount(b)
}

func (p *parser) peek(tok int32) bool {
	return p.tok == tok || (p.tok == WORD && p.val == tokStr(tok))
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
	EOF:  "EOF",
	WORD: "word",

	IF:    "if",
	THEN:  "then",
	ELSE:  "else",
	FI:    "fi",
	WHILE: "while",
	DO:    "do",
	DONE:  "done",
}

func tokStr(tok int32) string {
	if s, e := tokStrs[tok]; e {
		return s
	}
	return strconv.QuoteRune(tok)
}

func (p *parser) errPass(err error) {
	if p.err == nil {
		p.err = err
	}
	p.tok = EOF
}

func (p *parser) lineErr(format string, v ...interface{}) {
	pos := fmt.Sprintf("%s:%d:%d: ", p.name, p.line, p.col)
	p.errPass(fmt.Errorf(pos+format, v...))
}

func (p *parser) errWantedStr(s string) {
	p.lineErr("unexpected token %s, wanted %s", tokStr(p.tok), s)
}

func (p *parser) errWanted(tok int32) {
	p.errWantedStr(tokStr(tok))
}

func (p *parser) errAfterStr(s string) {
	p.lineErr("unexpected token %s after %s", tokStr(p.tok), s)
}

func (p *parser) program() {
	for p.tok != EOF {
		if p.got('\n') {
			continue
		}
		p.command()
	}
}

func (p *parser) command() {
	switch {
	case p.got('('):
		count := 0
		for p.tok != EOF && !p.peek(')') {
			if p.got('\n') {
				continue
			}
			p.command()
			count++
		}
		if count == 0 {
			p.errWantedStr("command")
		}
		p.want(')')
	case p.got(IF):
		p.command()
		p.want(THEN)
		for p.tok != EOF && !p.peek(FI) && !p.peek(ELSE) {
			if p.got('\n') {
				continue
			}
			p.command()
		}
		if p.got(ELSE) {
			for p.tok != EOF && !p.peek(FI) {
				if p.got('\n') {
					continue
				}
				p.command()
			}
		}
		p.want(FI)
	case p.got(WHILE):
		p.command()
		p.want(DO)
		for p.tok != EOF && !p.peek(DONE) {
			if p.got('\n') {
				continue
			}
			p.command()
		}
		p.want(DONE)
	case p.got(WORD):
		for p.tok != EOF {
			lval := p.val
			switch {
			case p.got(WORD):
			case p.got('='):
				if !ident.MatchString(lval) {
					p.col -= utf8.RuneCountInString(lval)
					p.col--
					p.lineErr("invalid var name %q", lval)
					return
				}
				p.got(WORD)
			case p.got('&'):
				if p.got('&') {
					p.command()
				}
				return
			case p.got('|'):
				p.got('|')
				p.command()
				return
			case p.got('('):
				if !ident.MatchString(lval) {
					p.col -= utf8.RuneCountInString(lval)
					p.col--
					p.lineErr("invalid func name %q", lval)
					return
				}
				p.want(')')
				p.command()
				return
			case p.got('>'):
				p.redirectDest()
			case p.got('<'):
				p.want(WORD)
			case p.got(';'):
				return
			case p.got('\n'):
				return
			default:
				p.errAfterStr("command")
			}
		}
	case p.got('{'):
		for p.tok != EOF && !p.peek('}') {
			if p.got('\n') {
				continue
			}
			p.command()
		}
		p.want('}')
		switch {
		case p.got('&'):
			if p.got('&') {
				p.command()
			}
			return
		case p.got('|'):
			p.got('|')
			p.command()
			return
		case p.got('>'):
			p.redirectDest()
		case p.got('<'):
			p.want(WORD)
		case p.got(';'):
			return
		case p.got('\n'):
			return
		default:
			p.errAfterStr("block")
		}
	default:
		p.errWantedStr("command")
	}
}

func (p *parser) redirectDest() {
	switch {
	case p.got('&'):
		p.want(WORD)
		if !num.MatchString(p.val) {
			p.col -= utf8.RuneCountInString(p.val)
			p.col++
			p.lineErr("invalid fd %q", p.val)
		}
		return
	case p.got('>'):
	}
	p.want(WORD)
}
