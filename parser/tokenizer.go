// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package parser

import (
	"bytes"
	"io"

	"github.com/mvdan/sh/ast"
	"github.com/mvdan/sh/token"
)

// bytes that form or start a token
func regOps(b byte) bool {
	return b == ';' || b == '"' || b == '\'' || b == '(' ||
		b == ')' || b == '$' || b == '|' || b == '&' ||
		b == '>' || b == '<' || b == '`'
}

// tokenize these inside parameter expansions
func paramOps(b byte) bool {
	return b == '}' || b == '#' || b == ':' || b == '-' ||
		b == '+' || b == '=' || b == '?' || b == '%' ||
		b == '[' || b == '/'
}

// tokenize these inside arithmetic expansions
func arithmOps(b byte) bool {
	return b == '+' || b == '-' || b == '!' || b == '*' ||
		b == '/' || b == '%' || b == '(' || b == ')' ||
		b == '^' || b == '<' || b == '>' || b == ':' ||
		b == '=' || b == ',' || b == '?' || b == '|' ||
		b == '&'
}

func wordBreak(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n' ||
		b == '&' || b == '>' || b == '<' || b == '|' ||
		b == ';' || b == '(' || b == ')'
}

func (p *parser) next() {
	if p.tok == token.EOF {
		return
	}
	if p.npos >= len(p.src) {
		p.errPass(io.EOF)
		return
	}
	b := p.src[p.npos]
	if p.quote == token.SHL && p.hdocStop == nil {
		return
	}
	if p.tok == token.STOPPED && b == '\n' {
		p.npos++
		p.f.Lines = append(p.f.Lines, p.npos)
		p.doHeredocs()
		if p.tok == token.EOF || p.npos >= len(p.src) {
			p.errPass(io.EOF)
			return
		}
		b = p.src[p.npos]
		p.spaced, p.newLine = true, true
	} else {
		p.spaced, p.newLine = false, false
	}
	q := p.quote
	p.pos = token.Pos(p.npos + 1)
	switch q {
	case token.QUO:
		switch b {
		case '}':
			p.npos++
			p.tok = token.RBRACE
		case '/':
			p.npos++
			p.tok = token.QUO
		case '`', '"', '$':
			p.tok = p.dqToken(b)
		default:
			p.advanceLitOther(q)
		}
		return
	case token.DQUOTE:
		if b == '`' || b == '"' || b == '$' {
			p.tok = p.dqToken(b)
		} else {
			p.advanceLitDquote()
		}
		return
	case token.SHL:
		if b == '`' || b == '$' {
			p.tok = p.dqToken(b)
		} else {
			p.advanceLitHdoc()
		}
		return
	case token.RBRACE:
		switch b {
		case '}':
			p.npos++
			p.tok = token.RBRACE
		case '`', '"', '$':
			p.tok = p.dqToken(b)
		default:
			p.advanceLitOther(q)
		}
		return
	case token.SQUOTE:
		if b == '\'' {
			p.npos++
			p.tok = token.SQUOTE
		} else {
			p.advanceLitOther(q)
		}
		return
	}
skipSpace:
	for {
		switch b {
		case ' ', '\t', '\r':
			p.spaced = true
			p.npos++
		case '\n':
			if p.stopNewline {
				p.stopNewline = false
				p.tok = token.STOPPED
				return
			}
			p.spaced = true
			if p.npos < len(p.src) {
				p.npos++
			}
			p.f.Lines = append(p.f.Lines, p.npos)
			p.newLine = true
		case '\\':
			if p.npos < len(p.src)-1 && p.src[p.npos+1] == '\n' {
				p.npos += 2
				p.f.Lines = append(p.f.Lines, p.npos)
			} else {
				break skipSpace
			}
		default:
			break skipSpace
		}
		if p.npos >= len(p.src) {
			p.errPass(io.EOF)
			return
		}
		b = p.src[p.npos]
	}
	p.pos = token.Pos(p.npos + 1)
	switch {
	case q == token.ILLEGAL, q == token.RPAREN, q == token.BQUOTE, q == token.DSEMICOLON:
		switch b {
		case ';', '"', '\'', '(', ')', '$', '|', '&', '>', '<', '`':
			p.tok = p.regToken(b)
		case '#':
			p.npos++
			bs, _ := p.readUntil('\n')
			p.npos += len(bs)
			if p.mode&ParseComments > 0 {
				p.f.Comments = append(p.f.Comments, &ast.Comment{
					Hash: p.pos,
					Text: string(bs),
				})
			}
			p.next()
		default:
			p.advanceLitNone()
		}
	case q == token.LBRACE && paramOps(b):
		p.tok = p.paramToken(b)
	case (q == token.DRPAREN || q == token.DOLLBK) && arithmOps(b):
		p.tok = p.arithmToken(b)
	case (q == token.RBRACK || q == token.DOLLBK) && b == ']':
		p.npos++
		p.tok = token.RBRACK
	case regOps(b):
		p.tok = p.regToken(b)
	default:
		p.advanceLitOther(q)
	}
}

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
		case '[':
			p.npos += 2
			return token.DOLLBK
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
		case '[':
			p.npos += 2
			return token.DOLLBK
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

func (p *parser) advanceLitOther(q token.Token) {
	bs := make([]byte, 0, 8)
	for {
		if p.npos >= len(p.src) {
			p.tok, p.val = token.LIT, string(bs)
			return
		}
		b := p.src[p.npos]
		switch {
		case b == '\\': // escaped byte follows
			if p.npos == len(p.src)-1 {
				p.npos++
				bs = append(bs, '\\')
				p.tok, p.val = token.LIT, string(bs)
				return
			}
			b = p.src[p.npos+1]
			p.npos += 2
			if b == '\n' {
				p.f.Lines = append(p.f.Lines, p.npos)
			} else {
				bs = append(bs, '\\', b)
			}
			continue
		case q == token.SQUOTE:
			switch b {
			case '\n':
				p.f.Lines = append(p.f.Lines, p.npos+1)
			case '\'':
				p.tok, p.val = token.LIT, string(bs)
				return
			}
		case b == '`', b == '$':
			p.tok, p.val = token.LIT, string(bs)
			return
		case q == token.RBRACE:
			if b == '}' || b == '"' {
				p.tok, p.val = token.LIT, string(bs)
				return
			}
		case q == token.QUO:
			if b == '/' || b == '}' {
				p.tok, p.val = token.LIT, string(bs)
				return
			}
		case wordBreak(b), regOps(b),
			(q == token.DRPAREN || q == token.DOLLBK) && arithmOps(b),
			q == token.LBRACE && paramOps(b),
			(q == token.RBRACK || q == token.DOLLBK) && b == ']':
			p.tok, p.val = token.LIT, string(bs)
			return
		}
		bs = append(bs, p.src[p.npos])
		p.npos++
	}
}

func (p *parser) advanceLitNone() {
	var i int
	tok := token.LIT
loop:
	for i = p.npos; i < len(p.src); i++ {
		switch p.src[i] {
		case '\\': // escaped byte follows
			if i == len(p.src)-1 {
				break
			}
			i++
			if p.src[i] == '\n' {
				p.f.Lines = append(p.f.Lines, i+1)
				bs := p.src[p.npos : i-1]
				p.npos = i + 1
				p.advanceLitNoneCont(bs)
				return
			}
		case ' ', '\t', '\n', '\r', '&', '>', '<', '|', ';', '(', ')':
			tok = token.LITWORD
			break loop
		case '`':
			if p.quote == token.BQUOTE {
				tok = token.LITWORD
			}
			break loop
		case '"', '\'', '$':
			break loop
		}
	}
	if i == len(p.src) {
		tok = token.LITWORD
	}
	p.tok, p.val = tok, string(p.src[p.npos:i])
	p.npos = i
}

func (p *parser) advanceLitNoneCont(bs []byte) {
	for {
		if p.npos >= len(p.src) {
			p.tok, p.val = token.LITWORD, string(bs)
			return
		}
		switch p.src[p.npos] {
		case '\\': // escaped byte follows
			if p.npos == len(p.src)-1 {
				p.npos++
				bs = append(bs, '\\')
				p.tok, p.val = token.LIT, string(bs)
				return
			}
			b := p.src[p.npos+1]
			p.npos += 2
			if b == '\n' {
				p.f.Lines = append(p.f.Lines, p.npos)
			} else {
				bs = append(bs, '\\', b)
			}
		case ' ', '\t', '\n', '\r', '&', '>', '<', '|', ';', '(', ')':
			p.tok, p.val = token.LITWORD, string(bs)
			return
		case '`':
			if p.quote == token.BQUOTE {
				p.tok, p.val = token.LITWORD, string(bs)
				return
			}
			fallthrough
		case '"', '\'', '$':
			p.tok, p.val = token.LIT, string(bs)
			return
		default:
			bs = append(bs, p.src[p.npos])
			p.npos++
		}
	}
}

func (p *parser) advanceLitDquote() {
	var i int
loop:
	for i = p.npos; i < len(p.src); i++ {
		switch p.src[i] {
		case '\\': // escaped byte follows
			if i++; len(p.src) > i && p.src[i] == '\n' {
				p.f.Lines = append(p.f.Lines, i+1)
			}
		case '`', '"', '$':
			break loop
		case '\n':
			p.f.Lines = append(p.f.Lines, i+1)
		}
	}
	p.tok, p.val = token.LIT, string(p.src[p.npos:i])
	p.npos = i
}

func (p *parser) isHdocEnd(i int) bool {
	end := p.hdocStop
	if len(p.src) < i+len(end) {
		return false
	}
	if !bytes.Equal(end, p.src[i:i+len(end)]) {
		return false
	}
	return len(p.src) == i+len(end) || p.src[i+len(end)] == '\n'
}

func (p *parser) advanceLitHdoc() {
	n := p.npos
	for p.hdocTabs && n < len(p.src) && p.src[n] == '\t' {
		n++
	}
	if p.isHdocEnd(n) {
		if n > p.npos {
			p.tok, p.val = token.LIT, string(p.src[p.npos:n])
		}
		p.npos = n + len(p.hdocStop)
		p.hdocStop = nil
		return
	}
	var i int
loop:
	for i = p.npos; i < len(p.src); i++ {
		switch p.src[i] {
		case '\\': // escaped byte follows
			if i++; i == len(p.src) {
				break loop
			}
			if p.src[i] == '\n' {
				p.f.Lines = append(p.f.Lines, i+1)
			}
		case '`', '$':
			break loop
		case '\n':
			n := i + 1
			p.f.Lines = append(p.f.Lines, n)
			for p.hdocTabs && n < len(p.src) && p.src[n] == '\t' {
				n++
			}
			if p.isHdocEnd(n) {
				p.tok, p.val = token.LIT, string(p.src[p.npos:n])
				p.npos = n + len(p.hdocStop)
				p.hdocStop = nil
				return
			}
		}
	}
	p.tok, p.val = token.LIT, string(p.src[p.npos:i])
	p.npos = i
}

func (p *parser) hdocLitWord() ast.Word {
	var buf bytes.Buffer
	end := p.hdocStop
	pos := p.npos + 1
	for p.npos < len(p.src) {
		bs, found := p.readUntil('\n')
		n := p.npos
		p.npos += len(bs) + 1
		if found {
			p.f.Lines = append(p.f.Lines, p.npos)
		}
		for p.hdocTabs && n < len(p.src) && p.src[n] == '\t' {
			n++
		}
		if p.isHdocEnd(n) {
			// add trailing tabs
			buf.Write(bs[:len(bs)-len(end)])
			break
		}
		buf.Write(bs)
		if found {
			buf.WriteByte('\n')
		}
	}
	l := &ast.Lit{Value: buf.String(), ValuePos: token.Pos(pos)}
	return ast.Word{Parts: []ast.WordPart{l}}
}

func (p *parser) readUntil(b byte) ([]byte, bool) {
	rem := p.src[p.npos:]
	if i := bytes.IndexByte(rem, b); i >= 0 {
		return rem[:i], true
	}
	return rem, false
}
