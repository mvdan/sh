// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"bufio"
	"fmt"
	"io"
)

const (
	_ = -iota
	EOF
	IDENT
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
	'#': true,
	'=': true,
	'&': true,
	'|': true,
	';': true,
	'(': true,
	')': true,
	'{': true,
	'}': true,
}

func (p *parser) next() {
	r, _, err := p.r.ReadRune()
	if err == io.EOF {
		p.tok = EOF
		return
	}
	if err != nil {
		p.errPass(err)
		return
	}
	if reserved[r] {
		p.tok = r
		return
	}
	read := false
	for !reserved[r] && r != '\n' && r != ' ' {
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
	p.tok = IDENT
	return
}

func (p *parser) discardLine() {
	_, err := p.r.ReadBytes('\n')
	if err == io.EOF {
		p.tok = EOF
	} else if err != nil {
		p.errPass(err)
	} else {
		p.next()
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
	case IDENT:
		return "ident"
	default:
		return fmt.Sprintf("%c", tok)
	}
}

func (p *parser) errPass(err error) {
	p.err = err
	p.tok = EOF
}

func (p *parser) errUnexpected() {
	p.err = fmt.Errorf("unexpected token %s", tokStr(p.tok))
	p.tok = EOF
}

func (p *parser) errWanted(tok int32) {
	p.err = fmt.Errorf("unexpected token %s, wanted %s", tokStr(p.tok), tokStr(tok))
	p.tok = EOF
}

func (p *parser) program() {
	p.next()
	for p.tok != EOF {
		if p.err != nil {
			return
		}
		switch {
		case p.got('#'):
			p.discardLine()
		case p.got(IDENT):
			switch {
			case p.got('='):
			case p.got('&'):
				if p.got('&') {
					p.want(IDENT)
				}
			case p.got('|'):
				p.got('|')
				p.want(IDENT)
			}
			p.got(';')
		default:
			p.errUnexpected()
		}
	}
}
