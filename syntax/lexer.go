// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"bytes"
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
	p.pos = Pos(p.npos + 1)
	switch q {
	case hdocWord:
		if wordBreak(b) {
			p.tok = illegalTok
			p.spaced = true
			return
		}
	case paramExpRepl:
		switch b {
		case '}':
			p.npos++
			p.tok = rightBrace
		case '/':
			p.npos++
			p.tok = QUO
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
			p.tok = illegalTok
		} else {
			p.advanceLitHdoc()
		}
		return
	case paramExpExp:
		switch b {
		case '}':
			p.npos++
			p.tok = rightBrace
		case '`', '"', '$':
			p.tok = p.dqToken(b)
		default:
			p.advanceLitOther(q)
		}
		return
	case sglQuotes:
		if b == '\'' {
			p.npos++
			p.tok = sglQuote
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
				p.tok = illegalTok
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
	p.pos = Pos(p.npos + 1)
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
				p.f.Comments = append(p.f.Comments, &Comment{
					Hash: p.pos,
					Text: string(bs),
				})
			}
			p.next()
		case '?', '*', '+', '@', '!':
			if p.bash() && p.npos+1 < len(p.src) && p.src[p.npos+1] == '(' {
				switch b {
				case '?':
					p.tok = GQUEST
				case '*':
					p.tok = GMUL
				case '+':
					p.tok = GADD
				case '@':
					p.tok = GAT
				default: // '!'
					p.tok = GNOT
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
		p.tok = rightBrack
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

func (p *parser) regToken(b byte) Token {
	switch b {
	case '\'':
		p.npos++
		return sglQuote
	case '"':
		p.npos++
		return dblQuote
	case '`':
		p.npos++
		return bckQuote
	case '&':
		switch byteAt(p.src, p.npos+1) {
		case '&':
			p.npos += 2
			return AndIf
		case '>':
			if !p.bash() {
				break
			}
			if byteAt(p.src, p.npos+2) == '>' {
				p.npos += 3
				return APPALL
			}
			p.npos += 2
			return RDRALL
		}
		p.npos++
		return And
	case '|':
		switch byteAt(p.src, p.npos+1) {
		case '|':
			p.npos += 2
			return OrIf
		case '&':
			if !p.bash() {
				break
			}
			p.npos += 2
			return PIPEALL
		}
		p.npos++
		return Or
	case '$':
		switch byteAt(p.src, p.npos+1) {
		case '\'':
			if !p.bash() {
				break
			}
			p.npos += 2
			return dollSglQuote
		case '"':
			if !p.bash() {
				break
			}
			p.npos += 2
			return dollDblQuote
		case '{':
			p.npos += 2
			return dollBrace
		case '[':
			if !p.bash() {
				break
			}
			p.npos += 2
			return dollBrack
		case '(':
			if byteAt(p.src, p.npos+2) == '(' {
				p.npos += 3
				return dollDblParen
			}
			p.npos += 2
			return dollParen
		}
		p.npos++
		return dollar
	case '(':
		if p.bash() && byteAt(p.src, p.npos+1) == '(' {
			p.npos += 2
			return dblLeftParen
		}
		p.npos++
		return leftParen
	case ')':
		p.npos++
		return rightParen
	case ';':
		switch byteAt(p.src, p.npos+1) {
		case ';':
			if p.bash() && byteAt(p.src, p.npos+2) == '&' {
				p.npos += 3
				return DblSemiFall
			}
			p.npos += 2
			return DblSemicolon
		case '&':
			if !p.bash() {
				break
			}
			p.npos += 2
			return SemiFall
		}
		p.npos++
		return semicolon
	case '<':
		switch byteAt(p.src, p.npos+1) {
		case '<':
			if b := byteAt(p.src, p.npos+2); b == '-' {
				p.npos += 3
				return DHEREDOC
			} else if p.bash() && b == '<' {
				p.npos += 3
				return WHEREDOC
			}
			p.npos += 2
			return SHL
		case '>':
			p.npos += 2
			return RDRINOUT
		case '&':
			p.npos += 2
			return DPLIN
		case '(':
			if !p.bash() {
				break
			}
			p.npos += 2
			return CMDIN
		}
		p.npos++
		return LSS
	default: // '>'
		switch byteAt(p.src, p.npos+1) {
		case '>':
			p.npos += 2
			return SHR
		case '&':
			p.npos += 2
			return DPLOUT
		case '|':
			p.npos += 2
			return CLBOUT
		case '(':
			if !p.bash() {
				break
			}
			p.npos += 2
			return CMDOUT
		}
		p.npos++
		return GTR
	}
}

func (p *parser) dqToken(b byte) Token {
	switch b {
	case '"':
		p.npos++
		return dblQuote
	case '`':
		p.npos++
		return bckQuote
	default: // '$'
		switch byteAt(p.src, p.npos+1) {
		case '{':
			p.npos += 2
			return dollBrace
		case '[':
			if !p.bash() {
				break
			}
			p.npos += 2
			return dollBrack
		case '(':
			if byteAt(p.src, p.npos+2) == '(' {
				p.npos += 3
				return dollDblParen
			}
			p.npos += 2
			return dollParen
		}
		p.npos++
		return dollar
	}
}

func (p *parser) paramToken(b byte) Token {
	switch b {
	case '}':
		p.npos++
		return rightBrace
	case ':':
		switch byteAt(p.src, p.npos+1) {
		case '+':
			p.npos += 2
			return CADD
		case '-':
			p.npos += 2
			return CSUB
		case '?':
			p.npos += 2
			return CQUEST
		case '=':
			p.npos += 2
			return CASSIGN
		}
		p.npos++
		return COLON
	case '+':
		p.npos++
		return ADD
	case '-':
		p.npos++
		return SUB
	case '?':
		p.npos++
		return QUEST
	case '=':
		p.npos++
		return ASSIGN
	case '%':
		if byteAt(p.src, p.npos+1) == '%' {
			p.npos += 2
			return DREM
		}
		p.npos++
		return REM
	case '#':
		if byteAt(p.src, p.npos+1) == '#' {
			p.npos += 2
			return DHASH
		}
		p.npos++
		return HASH
	case '[':
		p.npos++
		return leftBrack
	case '^':
		if byteAt(p.src, p.npos+1) == '^' {
			p.npos += 2
			return DXOR
		}
		p.npos++
		return XOR
	case ',':
		if byteAt(p.src, p.npos+1) == ',' {
			p.npos += 2
			return DCOMMA
		}
		p.npos++
		return COMMA
	default: // '/'
		if byteAt(p.src, p.npos+1) == '/' {
			p.npos += 2
			return DQUO
		}
		p.npos++
		return QUO
	}
}

func (p *parser) arithmToken(b byte) Token {
	switch b {
	case '!':
		if byteAt(p.src, p.npos+1) == '=' {
			p.npos += 2
			return NEQ
		}
		p.npos++
		return NOT
	case '=':
		if byteAt(p.src, p.npos+1) == '=' {
			p.npos += 2
			return EQL
		}
		p.npos++
		return ASSIGN
	case '(':
		p.npos++
		return leftParen
	case ')':
		p.npos++
		return rightParen
	case '&':
		switch byteAt(p.src, p.npos+1) {
		case '&':
			p.npos += 2
			return AndIf
		case '=':
			p.npos += 2
			return ANDASSGN
		}
		p.npos++
		return And
	case '|':
		switch byteAt(p.src, p.npos+1) {
		case '|':
			p.npos += 2
			return OrIf
		case '=':
			p.npos += 2
			return ORASSGN
		}
		p.npos++
		return Or
	case '<':
		switch byteAt(p.src, p.npos+1) {
		case '<':
			if byteAt(p.src, p.npos+2) == '=' {
				p.npos += 3
				return SHLASSGN
			}
			p.npos += 2
			return SHL
		case '=':
			p.npos += 2
			return LEQ
		}
		p.npos++
		return LSS
	case '>':
		switch byteAt(p.src, p.npos+1) {
		case '>':
			if byteAt(p.src, p.npos+2) == '=' {
				p.npos += 3
				return SHRASSGN
			}
			p.npos += 2
			return SHR
		case '=':
			p.npos += 2
			return GEQ
		}
		p.npos++
		return GTR
	case '+':
		switch byteAt(p.src, p.npos+1) {
		case '+':
			p.npos += 2
			return INC
		case '=':
			p.npos += 2
			return ADDASSGN
		}
		p.npos++
		return ADD
	case '-':
		switch byteAt(p.src, p.npos+1) {
		case '-':
			p.npos += 2
			return DEC
		case '=':
			p.npos += 2
			return SUBASSGN
		}
		p.npos++
		return SUB
	case '%':
		if byteAt(p.src, p.npos+1) == '=' {
			p.npos += 2
			return REMASSGN
		}
		p.npos++
		return REM
	case '*':
		switch byteAt(p.src, p.npos+1) {
		case '*':
			p.npos += 2
			return POW
		case '=':
			p.npos += 2
			return MULASSGN
		}
		p.npos++
		return MUL
	case '/':
		if byteAt(p.src, p.npos+1) == '=' {
			p.npos += 2
			return QUOASSGN
		}
		p.npos++
		return QUO
	case '^':
		if byteAt(p.src, p.npos+1) == '=' {
			p.npos += 2
			return XORASSGN
		}
		p.npos++
		return XOR
	case ',':
		p.npos++
		return COMMA
	case '?':
		p.npos++
		return QUEST
	default: // ':'
		p.npos++
		return COLON
	}
}

func (p *parser) advanceLitOther(q quoteState) {
	bs := p.litBuf[:0]
	for {
		if p.npos >= len(p.src) {
			p.tok, p.val = _LitWord, string(bs)
			return
		}
		b := p.src[p.npos]
		switch {
		case b == '\\': // escaped byte follows
			if p.npos == len(p.src)-1 {
				p.npos++
				bs = append(bs, '\\')
				p.tok, p.val = _LitWord, string(bs)
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
				p.tok, p.val = _LitWord, string(bs)
				return
			}
		case b == '`', b == '$':
			p.tok, p.val = _Lit, string(bs)
			return
		case q == paramExpExp:
			if b == '}' {
				p.tok, p.val = _LitWord, string(bs)
				return
			} else if b == '"' {
				p.tok, p.val = _Lit, string(bs)
				return
			}
		case q == paramExpRepl:
			if b == '}' || b == '/' {
				p.tok, p.val = _LitWord, string(bs)
				return
			}
		case q == paramExpInd && wordBreak(b):
		case wordBreak(b), regOps(b), q&allArithmExpr != 0 && arithmOps(b),
			q == paramExpName && paramOps(b, p.bash()),
			q&allRbrack != 0 && b == ']':
			p.tok, p.val = _LitWord, string(bs)
			return
		}
		bs = append(bs, p.src[p.npos])
		p.npos++
	}
}

func (p *parser) advanceLitNone() {
	var i int
	tok := _Lit
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
			tok = _LitWord
			break loop
		case '?', '*', '+', '@', '!':
			if p.bash() && i+1 < len(p.src) && p.src[i+1] == '(' {
				break loop
			}
		case '`':
			if p.quote == subCmdBckquo {
				tok = _LitWord
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
		tok = _LitWord
	}
	p.tok, p.val = tok, string(p.src[p.npos:i])
	p.npos = i
}

func (p *parser) advanceLitNoneCont(bs []byte) {
	for {
		if p.npos >= len(p.src) {
			p.tok, p.val = _LitWord, string(bs)
			return
		}
		switch p.src[p.npos] {
		case '\\': // escaped byte follows
			if p.npos == len(p.src)-1 {
				p.npos++
				bs = append(bs, '\\')
				p.tok, p.val = _LitWord, string(bs)
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
			p.tok, p.val = _LitWord, string(bs)
			return
		case '`':
			if p.quote == subCmdBckquo {
				p.tok, p.val = _LitWord, string(bs)
				return
			}
			fallthrough
		case '"', '\'', '$':
			p.tok, p.val = _Lit, string(bs)
			return
		default:
			bs = append(bs, p.src[p.npos])
			p.npos++
		}
	}
}

func (p *parser) advanceLitDquote() {
	var i int
	tok := _Lit
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
			tok = _LitWord
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
			p.tok, p.val = _LitWord, string(p.src[p.npos:n])
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
				p.tok, p.val = _LitWord, string(p.src[p.npos:n])
				p.npos = n + len(p.hdocStop)
				p.hdocStop = nil
				return
			}
		}
	}
	p.tok, p.val = _Lit, string(p.src[p.npos:i])
	p.npos = i
}

func (p *parser) hdocLitWord() Word {
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
	l := p.lit(Pos(pos+1), string(p.src[pos:end]))
	return Word{Parts: p.singleWps(l)}
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
	p.tok = _LitWord
	if end == -1 {
		p.val = string(p.src[p.npos:])
		p.npos = len(p.src)
		return
	}
	p.val = string(p.src[p.npos : p.npos+end])
	p.npos += end
}

func testUnaryOp(val string) Token {
	switch val {
	case "!":
		return NOT
	case "-e", "-a":
		return TEXISTS
	case "-f":
		return TREGFILE
	case "-d":
		return TDIRECT
	case "-c":
		return TCHARSP
	case "-b":
		return TBLCKSP
	case "-p":
		return TNMPIPE
	case "-S":
		return TSOCKET
	case "-L", "-h":
		return TSMBLINK
	case "-g":
		return TSGIDSET
	case "-u":
		return TSUIDSET
	case "-r":
		return TREAD
	case "-w":
		return TWRITE
	case "-x":
		return TEXEC
	case "-s":
		return TNOEMPTY
	case "-t":
		return TFDTERM
	case "-z":
		return TEMPSTR
	case "-n":
		return TNEMPSTR
	case "-o":
		return TOPTSET
	case "-v":
		return TVARSET
	case "-R":
		return TNRFVAR
	default:
		return illegalTok
	}
}

func testBinaryOp(val string) Token {
	switch val {
	case "=":
		return ASSIGN
	case "==":
		return EQL
	case "=~":
		return TREMATCH
	case "!=":
		return NEQ
	case "-nt":
		return TNEWER
	case "-ot":
		return TOLDER
	case "-ef":
		return TDEVIND
	case "-eq":
		return TEQL
	case "-ne":
		return TNEQ
	case "-le":
		return TLEQ
	case "-ge":
		return TGEQ
	case "-lt":
		return TLSS
	case "-gt":
		return TGTR
	default:
		return illegalTok
	}
}
