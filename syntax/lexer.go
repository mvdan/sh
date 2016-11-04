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
func paramOps(b byte) bool {
	return b == '}' || b == '#' || b == ':' || b == '-' || b == '+' ||
		b == '=' || b == '?' || b == '%' || b == '[' || b == '/' ||
		b == '^' || b == ','
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
			p.tok = Quo
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
					p.tok = globQuest
				case '*':
					p.tok = globMul
				case '+':
					p.tok = globAdd
				case '@':
					p.tok = globAt
				default: // '!'
					p.tok = globNot
				}
				p.npos += 2
			} else {
				p.advanceLitNone()
			}
		default:
			p.advanceLitNone()
		}
	case q == paramExpName && paramOps(b):
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
			return AndExpr
		case '>':
			if !p.bash() {
				break
			}
			if byteAt(p.src, p.npos+2) == '>' {
				p.npos += 3
				return appAll
			}
			p.npos += 2
			return rdrAll
		}
		p.npos++
		return And
	case '|':
		switch byteAt(p.src, p.npos+1) {
		case '|':
			p.npos += 2
			return OrExpr
		case '&':
			if !p.bash() {
				break
			}
			p.npos += 2
			return pipeAll
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
				return dblSemiFall
			}
			p.npos += 2
			return dblSemicolon
		case '&':
			if !p.bash() {
				break
			}
			p.npos += 2
			return semiFall
		}
		p.npos++
		return semicolon
	case '<':
		switch byteAt(p.src, p.npos+1) {
		case '<':
			if b := byteAt(p.src, p.npos+2); b == '-' {
				p.npos += 3
				return dashHdoc
			} else if p.bash() && b == '<' {
				p.npos += 3
				return wordHdoc
			}
			p.npos += 2
			return Shl
		case '>':
			p.npos += 2
			return rdrInOut
		case '&':
			p.npos += 2
			return dplIn
		case '(':
			if !p.bash() {
				break
			}
			p.npos += 2
			return cmdIn
		}
		p.npos++
		return Lss
	default: // '>'
		switch byteAt(p.src, p.npos+1) {
		case '>':
			p.npos += 2
			return Shr
		case '&':
			p.npos += 2
			return dplOut
		case '|':
			p.npos += 2
			return clbOut
		case '(':
			if !p.bash() {
				break
			}
			p.npos += 2
			return cmdOut
		}
		p.npos++
		return Gtr
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
			return ColAdd
		case '-':
			p.npos += 2
			return ColSub
		case '?':
			p.npos += 2
			return ColQuest
		case '=':
			p.npos += 2
			return ColAssgn
		}
		p.npos++
		return Colon
	case '+':
		p.npos++
		return Add
	case '-':
		p.npos++
		return Sub
	case '?':
		p.npos++
		return Quest
	case '=':
		p.npos++
		return Assgn
	case '%':
		if byteAt(p.src, p.npos+1) == '%' {
			p.npos += 2
			return dblRem
		}
		p.npos++
		return Rem
	case '#':
		if byteAt(p.src, p.npos+1) == '#' {
			p.npos += 2
			return dblHash
		}
		p.npos++
		return Hash
	case '[':
		p.npos++
		return leftBrack
	case '^':
		if byteAt(p.src, p.npos+1) == '^' {
			p.npos += 2
			return dblXor
		}
		p.npos++
		return Xor
	case ',':
		if byteAt(p.src, p.npos+1) == ',' {
			p.npos += 2
			return dblComma
		}
		p.npos++
		return Comma
	default: // '/'
		if byteAt(p.src, p.npos+1) == '/' {
			p.npos += 2
			return dblQuo
		}
		p.npos++
		return Quo
	}
}

func (p *parser) arithmToken(b byte) Token {
	switch b {
	case '!':
		if byteAt(p.src, p.npos+1) == '=' {
			p.npos += 2
			return Neq
		}
		p.npos++
		return Not
	case '=':
		if byteAt(p.src, p.npos+1) == '=' {
			p.npos += 2
			return Eql
		}
		p.npos++
		return Assgn
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
			return AndExpr
		case '=':
			p.npos += 2
			return AndAssgn
		}
		p.npos++
		return And
	case '|':
		switch byteAt(p.src, p.npos+1) {
		case '|':
			p.npos += 2
			return OrExpr
		case '=':
			p.npos += 2
			return OrAssgn
		}
		p.npos++
		return Or
	case '<':
		switch byteAt(p.src, p.npos+1) {
		case '<':
			if byteAt(p.src, p.npos+2) == '=' {
				p.npos += 3
				return ShlAssgn
			}
			p.npos += 2
			return Shl
		case '=':
			p.npos += 2
			return Leq
		}
		p.npos++
		return Lss
	case '>':
		switch byteAt(p.src, p.npos+1) {
		case '>':
			if byteAt(p.src, p.npos+2) == '=' {
				p.npos += 3
				return ShrAssgn
			}
			p.npos += 2
			return Shr
		case '=':
			p.npos += 2
			return Geq
		}
		p.npos++
		return Gtr
	case '+':
		switch byteAt(p.src, p.npos+1) {
		case '+':
			p.npos += 2
			return Inc
		case '=':
			p.npos += 2
			return AddAssgn
		}
		p.npos++
		return Add
	case '-':
		switch byteAt(p.src, p.npos+1) {
		case '-':
			p.npos += 2
			return Dec
		case '=':
			p.npos += 2
			return SubAssgn
		}
		p.npos++
		return Sub
	case '%':
		if byteAt(p.src, p.npos+1) == '=' {
			p.npos += 2
			return RemAssgn
		}
		p.npos++
		return Rem
	case '*':
		switch byteAt(p.src, p.npos+1) {
		case '*':
			p.npos += 2
			return Pow
		case '=':
			p.npos += 2
			return MulAssgn
		}
		p.npos++
		return Mul
	case '/':
		if byteAt(p.src, p.npos+1) == '=' {
			p.npos += 2
			return QuoAssgn
		}
		p.npos++
		return Quo
	case '^':
		if byteAt(p.src, p.npos+1) == '=' {
			p.npos += 2
			return XorAssgn
		}
		p.npos++
		return Xor
	case ',':
		p.npos++
		return Comma
	case '?':
		p.npos++
		return Quest
	default: // ':'
		p.npos++
		return Colon
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
		case wordBreak(b), regOps(b), q&allArithmExpr != 0 && arithmOps(b),
			q == paramExpName && paramOps(b),
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
		return Not
	case "-e", "-a":
		return tsExists
	case "-f":
		return tsRegFile
	case "-d":
		return tsDirect
	case "-c":
		return tsCharSp
	case "-b":
		return tsBlckSp
	case "-p":
		return tsNmPipe
	case "-S":
		return tsSocket
	case "-L", "-h":
		return tsSmbLink
	case "-g":
		return tsGIDSet
	case "-u":
		return tsUIDSet
	case "-r":
		return tsRead
	case "-w":
		return tsWrite
	case "-x":
		return tsExec
	case "-s":
		return tsNoEmpty
	case "-t":
		return tsFdTerm
	case "-z":
		return tsEmpStr
	case "-n":
		return tsNempStr
	case "-o":
		return tsOptSet
	case "-v":
		return tsVarSet
	case "-R":
		return tsRefVar
	default:
		return illegalTok
	}
}

func testBinaryOp(val string) Token {
	switch val {
	case "=":
		return Assgn
	case "==":
		return Eql
	case "!=":
		return Neq
	case "=~":
		return tsReMatch
	case "-nt":
		return tsNewer
	case "-ot":
		return tsOlder
	case "-ef":
		return tsDevIno
	case "-eq":
		return tsEql
	case "-ne":
		return tsNeq
	case "-le":
		return tsLeq
	case "-ge":
		return tsGeq
	case "-lt":
		return tsLss
	case "-gt":
		return tsGtr
	default:
		return illegalTok
	}
}
