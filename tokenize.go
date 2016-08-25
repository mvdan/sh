// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import "github.com/mvdan/sh/token"

func byteAt(src []byte, i int) byte {
	if i >= len(src) {
		return 0
	}
	return src[i]
}

func (p *parser) regToken(b byte) token.Token {
	switch b {
	case '\'':
		p.npos++
		return token.SQUOTE
	case '"':
		p.npos++
		return token.DQUOTE
	case '`':
		p.npos++
		return token.BQUOTE
	case '&':
		switch byteAt(p.src, p.npos+1) {
		case '&':
			p.npos += 2
			return token.LAND
		case '>':
			if byteAt(p.src, p.npos+2) == '>' {
				p.npos += 3
				return token.APPALL
			}
			p.npos += 2
			return token.RDRALL
		}
		p.npos++
		return token.AND
	case '|':
		switch byteAt(p.src, p.npos+1) {
		case '|':
			p.npos += 2
			return token.LOR
		case '&':
			p.npos += 2
			return token.PIPEALL
		}
		p.npos++
		return token.OR
	case '$':
		switch byteAt(p.src, p.npos+1) {
		case '\'':
			p.npos += 2
			return token.DOLLSQ
		case '"':
			p.npos += 2
			return token.DOLLDQ
		case '{':
			p.npos += 2
			return token.DOLLBR
		case '(':
			if byteAt(p.src, p.npos+2) == '(' {
				p.npos += 3
				return token.DOLLDP
			}
			p.npos += 2
			return token.DOLLPR
		}
		p.npos++
		return token.DOLLAR
	case '(':
		if p.mode&PosixComformant == 0 && byteAt(p.src, p.npos+1) == '(' {
			p.npos += 2
			return token.DLPAREN
		}
		p.npos++
		return token.LPAREN
	case ')':
		p.npos++
		return token.RPAREN
	case ';':
		switch byteAt(p.src, p.npos+1) {
		case ';':
			if byteAt(p.src, p.npos+2) == '&' {
				p.npos += 3
				return token.DSEMIFALL
			}
			p.npos += 2
			return token.DSEMICOLON
		case '&':
			p.npos += 2
			return token.SEMIFALL
		}
		p.npos++
		return token.SEMICOLON
	case '<':
		switch byteAt(p.src, p.npos+1) {
		case '<':
			switch byteAt(p.src, p.npos+2) {
			case '-':
				p.npos += 3
				return token.DHEREDOC
			case '<':
				p.npos += 3
				return token.WHEREDOC
			}
			p.npos += 2
			return token.SHL
		case '>':
			p.npos += 2
			return token.RDRINOUT
		case '&':
			p.npos += 2
			return token.DPLIN
		case '(':
			p.npos += 2
			return token.CMDIN
		}
		p.npos++
		return token.LSS
	default: // '>'
		switch byteAt(p.src, p.npos+1) {
		case '>':
			p.npos += 2
			return token.SHR
		case '&':
			p.npos += 2
			return token.DPLOUT
		case '(':
			p.npos += 2
			return token.CMDOUT
		}
		p.npos++
		return token.GTR
	}
}

func (p *parser) dqToken(b byte) token.Token {
	switch b {
	case '"':
		p.npos++
		return token.DQUOTE
	case '`':
		p.npos++
		return token.BQUOTE
	default: // '$'
		switch byteAt(p.src, p.npos+1) {
		case '{':
			p.npos += 2
			return token.DOLLBR
		case '(':
			if byteAt(p.src, p.npos+2) == '(' {
				p.npos += 3
				return token.DOLLDP
			}
			p.npos += 2
			return token.DOLLPR
		}
		p.npos++
		return token.DOLLAR
	}
}

func (p *parser) paramToken(b byte) token.Token {
	switch b {
	case '}':
		p.npos++
		return token.RBRACE
	case ':':
		switch byteAt(p.src, p.npos+1) {
		case '+':
			p.npos += 2
			return token.CADD
		case '-':
			p.npos += 2
			return token.CSUB
		case '?':
			p.npos += 2
			return token.CQUEST
		case '=':
			p.npos += 2
			return token.CASSIGN
		}
		p.npos++
		return token.COLON
	case '+':
		p.npos++
		return token.ADD
	case '-':
		p.npos++
		return token.SUB
	case '?':
		p.npos++
		return token.QUEST
	case '=':
		p.npos++
		return token.ASSIGN
	case '%':
		if byteAt(p.src, p.npos+1) == '%' {
			p.npos += 2
			return token.DREM
		}
		p.npos++
		return token.REM
	case '#':
		if byteAt(p.src, p.npos+1) == '#' {
			p.npos += 2
			return token.DHASH
		}
		p.npos++
		return token.HASH
	case '[':
		p.npos++
		return token.LBRACK
	default: // '/'
		if byteAt(p.src, p.npos+1) == '/' {
			p.npos += 2
			return token.DQUO
		}
		p.npos++
		return token.QUO
	}
}

func (p *parser) arithmToken(b byte) token.Token {
	switch b {
	case '!':
		if byteAt(p.src, p.npos+1) == '=' {
			p.npos += 2
			return token.NEQ
		}
		p.npos++
		return token.NOT
	case '=':
		if byteAt(p.src, p.npos+1) == '=' {
			p.npos += 2
			return token.EQL
		}
		p.npos++
		return token.ASSIGN
	case '(':
		p.npos++
		return token.LPAREN
	case ')':
		p.npos++
		return token.RPAREN
	case '&':
		switch byteAt(p.src, p.npos+1) {
		case '&':
			p.npos += 2
			return token.LAND
		case '=':
			p.npos += 2
			return token.ANDASSGN
		}
		p.npos++
		return token.AND
	case '|':
		switch byteAt(p.src, p.npos+1) {
		case '|':
			p.npos += 2
			return token.LOR
		case '=':
			p.npos += 2
			return token.ORASSGN
		}
		p.npos++
		return token.OR
	case '<':
		switch byteAt(p.src, p.npos+1) {
		case '<':
			if byteAt(p.src, p.npos+2) == '=' {
				p.npos += 3
				return token.SHLASSGN
			}
			p.npos += 2
			return token.SHL
		case '=':
			p.npos += 2
			return token.LEQ
		}
		p.npos++
		return token.LSS
	case '>':
		switch byteAt(p.src, p.npos+1) {
		case '>':
			if byteAt(p.src, p.npos+2) == '=' {
				p.npos += 3
				return token.SHRASSGN
			}
			p.npos += 2
			return token.SHR
		case '=':
			p.npos += 2
			return token.GEQ
		}
		p.npos++
		return token.GTR
	case '+':
		switch byteAt(p.src, p.npos+1) {
		case '+':
			p.npos += 2
			return token.INC
		case '=':
			p.npos += 2
			return token.ADDASSGN
		}
		p.npos++
		return token.ADD
	case '-':
		switch byteAt(p.src, p.npos+1) {
		case '-':
			p.npos += 2
			return token.DEC
		case '=':
			p.npos += 2
			return token.SUBASSGN
		}
		p.npos++
		return token.SUB
	case '%':
		if byteAt(p.src, p.npos+1) == '=' {
			p.npos += 2
			return token.REMASSGN
		}
		p.npos++
		return token.REM
	case '*':
		switch byteAt(p.src, p.npos+1) {
		case '*':
			p.npos += 2
			return token.POW
		case '=':
			p.npos += 2
			return token.MULASSGN
		}
		p.npos++
		return token.MUL
	case '/':
		if byteAt(p.src, p.npos+1) == '=' {
			p.npos += 2
			return token.QUOASSGN
		}
		p.npos++
		return token.QUO
	case '^':
		if byteAt(p.src, p.npos+1) == '=' {
			p.npos += 2
			return token.XORASSGN
		}
		p.npos++
		return token.XOR
	case ',':
		p.npos++
		return token.COMMA
	case '?':
		p.npos++
		return token.QUEST
	default: // ':'
		p.npos++
		return token.COLON
	}
}
