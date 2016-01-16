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
	return p.program()
}

type parser struct {
	r   *bufio.Reader
	tok int32
}

func isIdentChar(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func (p *parser) next() error {
	r, _, err := p.r.ReadRune()
	if err == io.EOF {
		p.tok = EOF
		return nil
	}
	if err != nil {
		return err
	}
	if r == ' ' {
		return p.next()
	}
	if isIdentChar(r) {
		for isIdentChar(r) {
			r, _, err = p.r.ReadRune()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
		}
		p.tok = IDENT
		return nil
	}
	p.tok = r
	return nil
}

func (p *parser) discardLine() error {
	for p.tok != '\n' && p.tok != EOF {
		if err := p.next(); err != nil {
			return err
		}
	}
	return p.next()
}

func (p *parser) got(tok int32) bool {
	if p.tok == tok {
		p.next()
		return true
	}
	return false
}

func (p *parser) program() error {
	if err := p.next(); err != nil {
		return err
	}
	for p.tok != EOF {
		switch {
		case p.got('#'):
			if err := p.discardLine(); err != nil {
				return err
			}
		case p.got(IDENT):
		default:
			return fmt.Errorf("unexpected token")
		}
	}
	return nil
}
