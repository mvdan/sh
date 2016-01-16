// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
)

const (
	_ = -iota
	EOF
	WORD
)

func parse(r io.Reader) error {
	p := &parser{
		r: bufio.NewReader(r),
	}
	p.program()
	return p.err
}

type parser struct {
	r   *bufio.Reader
	tok int32
	err error
}

var reserved = map[rune]bool{
	'\n': true,
	'#':  true,
	'=':  true,
	'&':  true,
	'|':  true,
	';':  true,
	'(':  true,
	')':  true,
	'{':  true,
	'}':  true,
}

var space = map[rune]bool{
	' ':  true,
	'\t': true,
}

func (p *parser) next() {
	if p.err != nil {
		return
	}
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
	}
	if reserved[r] {
		p.tok = r
		return
	}
	read := false
	for !reserved[r] && !space[r] {
		r, _, err = p.r.ReadRune()
		if err == io.EOF {
			break
		}
		if err != nil {
			p.errPass(err)
			return
		}
		read = true
	}
	if read {
		if err := p.r.UnreadRune(); err != nil {
			p.errPass(err)
			return
		}
	}
	p.tok = WORD
	return
}

func (p *parser) discardLine() {
	_, err := p.r.ReadBytes('\n')
	if err == io.EOF {
		p.tok = EOF
	} else if err != nil {
		p.errPass(err)
	} else {
		p.got('\n')
	}
}

func (p *parser) got(tok int32) bool {
	if p.tok == tok {
		p.next()
		return true
	}
	return false
}

func (p *parser) want(tok int32) {
	if p.tok != tok {
		p.errWanted(tok)
		return
	}
	p.next()
}

func tokStr(tok int32) string {
	switch tok {
	case EOF:
		return "EOF"
	case WORD:
		return "word"
	default:
		return strconv.QuoteRune(tok)
	}
}

func (p *parser) errPass(err error) {
	p.err = err
	p.tok = EOF
}

func (p *parser) errUnexpected() {
	p.errPass(fmt.Errorf("unexpected token %s", tokStr(p.tok)))
}

func (p *parser) errWanted(tok int32) {
	p.errPass(fmt.Errorf("unexpected token %s, wanted %s", tokStr(p.tok), tokStr(tok)))
}

func (p *parser) program() {
	p.next()
	for p.tok != EOF {
		p.command()
	}
}

func (p *parser) command() {
	switch {
	case p.got('\n'):
	case p.got('#'):
		p.discardLine()
	case p.got(WORD):
		switch {
		case p.got('='):
		case p.got('&'):
			if p.got('&') {
				p.command()
			}
		case p.got('|'):
			p.got('|')
			p.command()
		}
		p.got(';')
		p.got('\n')
	case p.got('{'):
		for !p.got('}') {
			p.command()
		}
		p.got(';')
		p.got('\n')
	default:
		p.errUnexpected()
	}
}
