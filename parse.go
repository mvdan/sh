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

func parse(r io.Reader, name string) error {
	p := &parser{
		r:    bufio.NewReader(r),
		name: name,
		line: 1,
		col:  0,
	}
	p.program()
	return p.err
}

type parser struct {
	r    *bufio.Reader
	tok  int32
	err  error
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
	'"':  true,
	'\'': true,
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
		p.col++
	}
	if reserved[r] {
		if r == '\n' {
			p.line++
			p.col = 0
		}
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
		p.col++
		read = true
	}
	if read {
		if err := p.r.UnreadRune(); err != nil {
			p.errPass(err)
			return
		}
		p.col--
	}
	p.tok = WORD
	return
}

func (p *parser) discardUpTo(delim byte) {
	_, err := p.r.ReadBytes(delim)
	if err == io.EOF {
		p.tok = EOF
	} else if err != nil {
		p.errPass(err)
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

func (p *parser) lineErr(format string, v ...interface{}) {
	pos := fmt.Sprintf("%s:%d:%d: ", p.name, p.line, p.col)
	p.errPass(fmt.Errorf(pos+format, v...))
}

func (p *parser) errUnexpected() {
	p.lineErr("unexpected token %s", tokStr(p.tok))
}

func (p *parser) errWanted(tok int32) {
	p.lineErr("unexpected token %s, wanted %s", tokStr(p.tok), tokStr(tok))
}

func (p *parser) program() {
	p.next()
	for p.tok != EOF {
		if p.got('\n') {
			continue
		}
		p.command()
	}
}

func (p *parser) command() {
	switch {
	case p.got('#'):
		p.discardUpTo('\n')
		p.next()
	case p.got('"'):
		p.strContent('"')
	case p.got('\''):
		p.strContent('\'')
	case p.got(WORD):
		for p.tok != EOF {
			switch {
			case p.got(WORD):
			case p.got('='):
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
				p.want(')')
				p.want('{')
				for !p.got('}') {
					if p.tok == EOF {
						p.errWanted('}')
						break
					}
					if p.got('\n') {
						continue
					}
					p.command()
				}
				return
			case p.got('>'):
				switch {
				case p.got('>'):
				case p.got('&'):
				}
				p.want(WORD)
			case p.got('<'):
				p.want(WORD)
			case p.got(';'):
				return
			case p.got('\n'):
				return
			default:
				p.errUnexpected()
			}
		}
	case p.got('{'):
		for !p.got('}') {
			if p.tok == EOF {
				p.errWanted('}')
				break
			}
			if p.got('\n') {
				continue
			}
			p.command()
		}
		switch {
		case p.got(';'):
		case p.got('\n'):
		}
	default:
		p.errUnexpected()
	}
}

func (p *parser) strContent(delim byte) {
	_, err := p.r.ReadBytes(delim)
	if err != nil {
		p.errPass(err)
	}
}
