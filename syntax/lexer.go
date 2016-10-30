// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"bytes"

	"github.com/mvdan/sh/ast"
	"github.com/mvdan/sh/token"
)

const (
	_ token.Token = -iota
	_EOF
	_LIT
	_LITWORD
	_LET
)

// bytes that form or start a token
func regOps(b byte) bool {
	return b == ';' || b == '"' || b == '\'' || b == '(' ||
		b == ')' || b == '$' || b == '|' || b == '&' ||
		b == '>' || b == '<' || b == '`'
}

// tokenize these inside parameter expansions
func paramOps(b byte, bash bool) bool {
	switch b {
	case '}', '#', ':', '-', '+', '=', '?', '%', '[', '/':
		return true
	case '^', ',':
		return bash
	}
	return false
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
	if p.tok == _EOF || p.npos >= len(p.src) {
		p.tok = _EOF
		return
	}
	p.spaced, p.newLine = false, false
	b, q := p.src[p.npos], p.quote
	p.pos = token.Pos(p.npos + 1)
	switch q {
	case hdocWord:
		if wordBreak(b) {
			p.tok = token.ILLEGAL
			p.spaced = true
			return
		}
	case paramExpRepl:
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
	case dblQuotes:
		if b == '`' || b == '"' || b == '$' {
			p.tok = p.dqToken(b)
		} else {
			p.advanceLitDquote()
		}
		return
	case hdocBody, hdocBodyTabs:
		if b == '`' || b == '$' {
			p.tok = p.dqToken(b)
		} else if p.hdocStop == nil {
			p.tok = token.ILLEGAL
		} else {
			p.advanceLitHdoc()
		}
		return
	case paramExpExp:
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
	case sglQuotes:
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
			if p.quote == arithmExprLet {
				p.tok = token.ILLEGAL
				p.newLine, p.spaced = true, true
				return
			}
			p.spaced = true
			if p.npos < len(p.src) {
				p.npos++
			}
			p.f.Lines = append(p.f.Lines, p.npos)
			p.newLine = true
			if len(p.heredocs) > p.buriedHdocs {
				p.doHeredocs()
				if p.tok == _EOF {
					return
				}
			}
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
			p.tok = _EOF
			return
		}
		b = p.src[p.npos]
	}
	p.pos = token.Pos(p.npos + 1)
	switch {
	case q&allRegTokens != 0:
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
		case '?', '*', '+', '@', '!':
			if p.bash() && p.npos+1 < len(p.src) && p.src[p.npos+1] == '(' {
				switch b {
				case '?':
					p.tok = token.GQUEST
				case '*':
					p.tok = token.GMUL
				case '+':
					p.tok = token.GADD
				case '@':
					p.tok = token.GAT
				default: // '!'
					p.tok = token.GNOT
				}
				p.npos += 2
			} else {
				p.advanceLitNone()
			}
		default:
			p.advanceLitNone()
		}
	case q == paramExpName && paramOps(b, p.bash()):
		p.tok = p.paramToken(b)
	case q&allArithmExpr != 0 && arithmOps(b):
		p.tok = p.arithmToken(b)
	case q&allRbrack != 0 && b == ']':
		p.npos++
		p.tok = token.RBRACK
	case q == testRegexp:
		p.advanceLitRe()
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
			if !p.bash() {
				break
			}
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
			if !p.bash() {
				break
			}
			p.npos += 2
			return token.PIPEALL
		}
		p.npos++
		return token.OR
	case '$':
		switch byteAt(p.src, p.npos+1) {
		case '\'':
			if !p.bash() {
				break
			}
			p.npos += 2
			return token.DOLLSQ
		case '"':
			if !p.bash() {
				break
			}
			p.npos += 2
			return token.DOLLDQ
		case '{':
			p.npos += 2
			return token.DOLLBR
		case '[':
			if !p.bash() {
				break
			}
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
		if p.bash() && byteAt(p.src, p.npos+1) == '(' {
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
			if p.bash() && byteAt(p.src, p.npos+2) == '&' {
				p.npos += 3
				return token.DSEMIFALL
			}
			p.npos += 2
			return token.DSEMICOLON
		case '&':
			if !p.bash() {
				break
			}
			p.npos += 2
			return token.SEMIFALL
		}
		p.npos++
		return token.SEMICOLON
	case '<':
		switch byteAt(p.src, p.npos+1) {
		case '<':
			if b := byteAt(p.src, p.npos+2); b == '-' {
				p.npos += 3
				return token.DHEREDOC
			} else if p.bash() && b == '<' {
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
			if !p.bash() {
				break
			}
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
		case '|':
			p.npos += 2
			return token.CLBOUT
		case '(':
			if !p.bash() {
				break
			}
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
			if !p.bash() {
				break
			}
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
	case '^':
		if byteAt(p.src, p.npos+1) == '^' {
			p.npos += 2
			return token.DXOR
		}
		p.npos++
		return token.XOR
	case ',':
		if byteAt(p.src, p.npos+1) == ',' {
			p.npos += 2
			return token.DCOMMA
		}
		p.npos++
		return token.COMMA
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

func (p *parser) advanceLitOther(q quoteState) {
	bs := p.litBuf[:0]
	for {
		if p.npos >= len(p.src) {
			p.tok, p.val = _LITWORD, string(bs)
			return
		}
		b := p.src[p.npos]
		switch {
		case b == '\\': // escaped byte follows
			if p.npos == len(p.src)-1 {
				p.npos++
				bs = append(bs, '\\')
				p.tok, p.val = _LITWORD, string(bs)
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
		case q == sglQuotes:
			switch b {
			case '\n':
				p.f.Lines = append(p.f.Lines, p.npos+1)
			case '\'':
				p.tok, p.val = _LITWORD, string(bs)
				return
			}
		case b == '`', b == '$':
			p.tok, p.val = _LIT, string(bs)
			return
		case q == paramExpExp:
			if b == '}' {
				p.tok, p.val = _LITWORD, string(bs)
				return
			} else if b == '"' {
				p.tok, p.val = _LIT, string(bs)
				return
			}
		case q == paramExpRepl:
			if b == '}' || b == '/' {
				p.tok, p.val = _LITWORD, string(bs)
				return
			}
		case q == paramExpInd && wordBreak(b):
		case wordBreak(b), regOps(b), q&allArithmExpr != 0 && arithmOps(b),
			q == paramExpName && paramOps(b, p.bash()),
			q&allRbrack != 0 && b == ']':
			p.tok, p.val = _LITWORD, string(bs)
			return
		}
		bs = append(bs, p.src[p.npos])
		p.npos++
	}
}

func (p *parser) advanceLitNone() {
	var i int
	tok := _LIT
	p.asPos = 0
loop:
	for i = p.npos; i < len(p.src); i++ {
		switch p.src[i] {
		case '\\': // escaped byte follows
			if i == len(p.src)-1 {
				break
			}
			if i++; p.src[i] == '\n' {
				p.f.Lines = append(p.f.Lines, i+1)
				bs := p.src[p.npos : i-1]
				p.npos = i + 1
				p.advanceLitNoneCont(bs)
				return
			}
		case ' ', '\t', '\n', '\r', '&', '>', '<', '|', ';', '(', ')':
			tok = _LITWORD
			break loop
		case '?', '*', '+', '@', '!':
			if p.bash() && i+1 < len(p.src) && p.src[i+1] == '(' {
				break loop
			}
		case '`':
			if p.quote == subCmdBckquo {
				tok = _LITWORD
			}
			break loop
		case '"', '\'', '$':
			break loop
		case '=':
			p.asPos = i - p.npos
			if p.bash() && p.asPos > 0 && p.src[p.npos+p.asPos-1] == '+' {
				p.asPos-- // a+=b
			}
		}
	}
	if i == len(p.src) {
		tok = _LITWORD
	}
	p.tok, p.val = tok, string(p.src[p.npos:i])
	p.npos = i
}

func (p *parser) advanceLitNoneCont(bs []byte) {
	for {
		if p.npos >= len(p.src) {
			p.tok, p.val = _LITWORD, string(bs)
			return
		}
		switch p.src[p.npos] {
		case '\\': // escaped byte follows
			if p.npos == len(p.src)-1 {
				p.npos++
				bs = append(bs, '\\')
				p.tok, p.val = _LITWORD, string(bs)
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
			p.tok, p.val = _LITWORD, string(bs)
			return
		case '`':
			if p.quote == subCmdBckquo {
				p.tok, p.val = _LITWORD, string(bs)
				return
			}
			fallthrough
		case '"', '\'', '$':
			p.tok, p.val = _LIT, string(bs)
			return
		default:
			bs = append(bs, p.src[p.npos])
			p.npos++
		}
	}
}

func (p *parser) advanceLitDquote() {
	var i int
	tok := _LIT
loop:
	for i = p.npos; i < len(p.src); i++ {
		switch p.src[i] {
		case '\\': // escaped byte follows
			if i == len(p.src)-1 {
				break
			}
			if i++; p.src[i] == '\n' {
				p.f.Lines = append(p.f.Lines, i+1)
			}
		case '"':
			tok = _LITWORD
			break loop
		case '`', '$':
			break loop
		case '\n':
			p.f.Lines = append(p.f.Lines, i+1)
		}
	}
	p.tok, p.val = tok, string(p.src[p.npos:i])
	p.npos = i
}

func (p *parser) isHdocEnd(i int) bool {
	end := p.hdocStop
	if end == nil || len(p.src) < i+len(end) {
		return false
	}
	if !bytes.Equal(end, p.src[i:i+len(end)]) {
		return false
	}
	return len(p.src) == i+len(end) || p.src[i+len(end)] == '\n'
}

func (p *parser) advanceLitHdoc() {
	n := p.npos
	if p.quote == hdocBodyTabs {
		for n < len(p.src) && p.src[n] == '\t' {
			n++
		}
	}
	if p.isHdocEnd(n) {
		if n > p.npos {
			p.tok, p.val = _LITWORD, string(p.src[p.npos:n])
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
			if p.quote == hdocBodyTabs {
				for n < len(p.src) && p.src[n] == '\t' {
					n++
				}
			}
			if p.isHdocEnd(n) {
				p.tok, p.val = _LITWORD, string(p.src[p.npos:n])
				p.npos = n + len(p.hdocStop)
				p.hdocStop = nil
				return
			}
		}
	}
	p.tok, p.val = _LIT, string(p.src[p.npos:i])
	p.npos = i
}

func (p *parser) hdocLitWord() ast.Word {
	pos := p.npos
	end := pos
	for p.npos < len(p.src) {
		end = p.npos
		bs, found := p.readUntil('\n')
		p.npos += len(bs) + 1
		if found {
			p.f.Lines = append(p.f.Lines, p.npos)
		}
		if p.quote == hdocBodyTabs {
			for end < len(p.src) && p.src[end] == '\t' {
				end++
			}
		}
		if p.isHdocEnd(end) {
			break
		}
	}
	if p.npos == len(p.src) {
		end = p.npos
	}
	l := p.lit(token.Pos(pos+1), string(p.src[pos:end]))
	return ast.Word{Parts: p.singleWps(l)}
}

func (p *parser) readUntil(b byte) ([]byte, bool) {
	rem := p.src[p.npos:]
	if i := bytes.IndexByte(rem, b); i >= 0 {
		return rem[:i], true
	}
	return rem, false
}

func (p *parser) advanceLitRe() {
	end := bytes.Index(p.src[p.npos:], []byte(" ]]"))
	p.tok = _LITWORD
	if end == -1 {
		p.val = string(p.src[p.npos:])
		p.npos = len(p.src)
		return
	}
	p.val = string(p.src[p.npos : p.npos+end])
	p.npos += end
}

func testUnaryOp(val string) token.Token {
	switch val {
	case "!":
		return token.NOT
	case "-e", "-a":
		return token.TEXISTS
	case "-f":
		return token.TREGFILE
	case "-d":
		return token.TDIRECT
	case "-c":
		return token.TCHARSP
	case "-b":
		return token.TBLCKSP
	case "-p":
		return token.TNMPIPE
	case "-S":
		return token.TSOCKET
	case "-L", "-h":
		return token.TSMBLINK
	case "-g":
		return token.TSGIDSET
	case "-u":
		return token.TSUIDSET
	case "-r":
		return token.TREAD
	case "-w":
		return token.TWRITE
	case "-x":
		return token.TEXEC
	case "-s":
		return token.TNOEMPTY
	case "-t":
		return token.TFDTERM
	case "-z":
		return token.TEMPSTR
	case "-n":
		return token.TNEMPSTR
	case "-o":
		return token.TOPTSET
	case "-v":
		return token.TVARSET
	case "-R":
		return token.TNRFVAR
	default:
		return token.ILLEGAL
	}
}

func testBinaryOp(val string) token.Token {
	switch val {
	case "=":
		return token.ASSIGN
	case "==":
		return token.EQL
	case "=~":
		return token.TREMATCH
	case "!=":
		return token.NEQ
	case "-nt":
		return token.TNEWER
	case "-ot":
		return token.TOLDER
	case "-ef":
		return token.TDEVIND
	case "-eq":
		return token.TEQL
	case "-ne":
		return token.TNEQ
	case "-le":
		return token.TLEQ
	case "-ge":
		return token.TGEQ
	case "-lt":
		return token.TLSS
	case "-gt":
		return token.TGTR
	default:
		return token.ILLEGAL
	}
}
