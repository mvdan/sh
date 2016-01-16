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

func isToken(r rune) bool {
	return r == '#' || r == '='
}

func (p *parser) next() {
	r, _, err := p.r.ReadRune()
	if err == io.EOF {
		p.tok = EOF
		return
	}
	if err != nil {
		p.tok = EOF
		p.err = err
		return
	}
	if r == ' ' {
		p.next()
		return
	}
	if isToken(r) {
		p.tok = r
		return
	}
	for !isToken(r) {
		r, _, err = p.r.ReadRune()
		if err == io.EOF {
			break
		}
		if err != nil {
			p.err = err
			p.tok = EOF
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
		p.tok = EOF
		p.err = err
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

func (p *parser) program() {
	p.next()
	for p.tok != EOF {
		switch {
		case p.got('#'):
			p.discardLine()
		case p.got(IDENT):
		default:
			p.err = fmt.Errorf("unexpected token")
		}
	}
}
