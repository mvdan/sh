// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import "bytes"

// bytes that form or start a token
func regOps(r rune) bool {
	switch r {
	case ';', '"', '\'', '(', ')', '$', '|', '&', '>', '<', '`':
		return true
	}
	return false
}

// tokenize these inside parameter expansions
func paramOps(r rune) bool {
	switch r {
	case '}', '#', ':', '-', '+', '=', '?', '%', '[', ']', '/', '^', ',':
		return true
	}
	return false
}

// tokenize these inside arithmetic expansions
func arithmOps(r rune) bool {
	switch r {
	case '+', '-', '!', '*', '/', '%', '(', ')', '^', '<', '>', ':', '=',
		',', '?', '|', '&', ']':
		return true
	}
	return false
}

func wordBreak(r rune) bool {
	switch r {
	case ' ', '\t', '\n', ';', '&', '>', '<', '|', '(', ')', '\r':
		return true
	}
	return false
}

func (p *parser) next() {
	if p.npos >= len(p.src) {
		p.tok = _EOF
		return
	}
	p.spaced, p.newLine = false, false
	r := rune(p.src[p.npos])
	p.pos = Pos(p.npos + 1)
	switch p.quote {
	case hdocWord:
		if wordBreak(r) {
			p.tok = illegalTok
			return
		}
	case paramExpRepl:
		switch r {
		case '}', '/':
			p.tok = p.paramToken(r)
		case '`', '"', '$':
			p.tok = p.dqToken(r)
		default:
			p.advanceLitOther()
		}
		return
	case dblQuotes:
		switch r {
		case '`', '"', '$':
			p.tok = p.dqToken(r)
		default:
			p.advanceLitDquote()
		}
		return
	case hdocBody, hdocBodyTabs:
		if r == '`' || r == '$' {
			p.tok = p.dqToken(r)
		} else if p.hdocStop == nil {
			p.tok = illegalTok
		} else {
			p.advanceLitHdoc()
		}
		return
	case paramExpExp:
		switch r {
		case '}':
			p.npos++
			p.tok = rightBrace
		case '`', '"', '$':
			p.tok = p.dqToken(r)
		default:
			p.advanceLitOther()
		}
		return
	case sglQuotes:
		if r == '\'' {
			p.npos++
			p.tok = sglQuote
		} else {
			p.advanceLitOther()
		}
		return
	}
skipSpace:
	for {
		switch r {
		case ' ', '\t', '\r':
			p.spaced = true
			p.npos++
		case '\n':
			p.spaced, p.newLine = true, true
			if p.quote == arithmExprLet {
				p.tok = illegalTok
				return
			}
			p.npos++
			p.f.Lines = append(p.f.Lines, p.npos)
			if len(p.heredocs) > p.buriedHdocs {
				if p.doHeredocs(); p.tok == _EOF {
					return
				}
			}
		case '\\':
			if byteAt(p.src, p.npos+1) == '\n' {
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
		r = rune(p.src[p.npos])
	}
	p.pos = Pos(p.npos + 1)
	switch {
	case p.quote&allRegTokens != 0:
		switch r {
		case ';', '"', '\'', '(', ')', '$', '|', '&', '>', '<', '`':
			p.tok = p.regToken(r)
		case '#':
			p.npos++
			bs, _ := p.readLine()
			p.npos += len(bs)
			if p.mode&ParseComments > 0 {
				p.f.Comments = append(p.f.Comments, &Comment{
					Hash: p.pos,
					Text: string(bs),
				})
			}
			p.next()
		case '?', '*', '+', '@', '!':
			if byteAt(p.src, p.npos+1) == '(' {
				switch r {
				case '?':
					p.tok = globQuest
				case '*':
					p.tok = globStar
				case '+':
					p.tok = globPlus
				case '@':
					p.tok = globAt
				default: // '!'
					p.tok = globExcl
				}
				p.npos += 2
			} else {
				p.advanceLitNone()
			}
		default:
			p.advanceLitNone()
		}
	case p.quote&allArithmExpr != 0 && arithmOps(r):
		p.tok = p.arithmToken(r)
	case p.quote&allParamExp != 0 && paramOps(r):
		p.tok = p.paramToken(r)
	case p.quote == testRegexp:
		if regOps(r) && r != '(' {
			p.tok = p.regToken(r)
		} else {
			p.advanceLitRe()
		}
	case regOps(r):
		p.tok = p.regToken(r)
	default:
		p.advanceLitOther()
	}
}

func byteAt(src []byte, i int) rune {
	if i >= len(src) {
		return 0
	}
	return rune(src[i])
}

func (p *parser) regToken(r rune) token {
	switch r {
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
			return andAnd
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
		return and
	case '|':
		switch byteAt(p.src, p.npos+1) {
		case '|':
			p.npos += 2
			return orOr
		case '&':
			if !p.bash() {
				break
			}
			p.npos += 2
			return pipeAll
		}
		p.npos++
		return or
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
			if r := byteAt(p.src, p.npos+2); r == '-' {
				p.npos += 3
				return dashHdoc
			} else if p.bash() && r == '<' {
				p.npos += 3
				return wordHdoc
			}
			p.npos += 2
			return hdoc
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
		return rdrIn
	default: // '>'
		switch byteAt(p.src, p.npos+1) {
		case '>':
			p.npos += 2
			return appOut
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
		return rdrOut
	}
}

func (p *parser) dqToken(r rune) token {
	switch r {
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

func (p *parser) paramToken(r rune) token {
	switch r {
	case '}':
		p.npos++
		return rightBrace
	case ':':
		switch byteAt(p.src, p.npos+1) {
		case '+':
			p.npos += 2
			return colPlus
		case '-':
			p.npos += 2
			return colMinus
		case '?':
			p.npos += 2
			return colQuest
		case '=':
			p.npos += 2
			return colAssgn
		}
		p.npos++
		return colon
	case '+':
		p.npos++
		return plus
	case '-':
		p.npos++
		return minus
	case '?':
		p.npos++
		return quest
	case '=':
		p.npos++
		return assgn
	case '%':
		if byteAt(p.src, p.npos+1) == '%' {
			p.npos += 2
			return dblPerc
		}
		p.npos++
		return perc
	case '#':
		if byteAt(p.src, p.npos+1) == '#' {
			p.npos += 2
			return dblHash
		}
		p.npos++
		return hash
	case '[':
		p.npos++
		return leftBrack
	case '^':
		if byteAt(p.src, p.npos+1) == '^' {
			p.npos += 2
			return dblCaret
		}
		p.npos++
		return caret
	case ',':
		if byteAt(p.src, p.npos+1) == ',' {
			p.npos += 2
			return dblComma
		}
		p.npos++
		return comma
	default: // '/'
		if byteAt(p.src, p.npos+1) == '/' {
			p.npos += 2
			return dblSlash
		}
		p.npos++
		return slash
	}
}

func (p *parser) arithmToken(r rune) token {
	switch r {
	case '!':
		if byteAt(p.src, p.npos+1) == '=' {
			p.npos += 2
			return nequal
		}
		p.npos++
		return exclMark
	case '=':
		if byteAt(p.src, p.npos+1) == '=' {
			p.npos += 2
			return equal
		}
		p.npos++
		return assgn
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
			return andAnd
		case '=':
			p.npos += 2
			return andAssgn
		}
		p.npos++
		return and
	case '|':
		switch byteAt(p.src, p.npos+1) {
		case '|':
			p.npos += 2
			return orOr
		case '=':
			p.npos += 2
			return orAssgn
		}
		p.npos++
		return or
	case '<':
		switch byteAt(p.src, p.npos+1) {
		case '<':
			if byteAt(p.src, p.npos+2) == '=' {
				p.npos += 3
				return shlAssgn
			}
			p.npos += 2
			return hdoc
		case '=':
			p.npos += 2
			return lequal
		}
		p.npos++
		return rdrIn
	case '>':
		switch byteAt(p.src, p.npos+1) {
		case '>':
			if byteAt(p.src, p.npos+2) == '=' {
				p.npos += 3
				return shrAssgn
			}
			p.npos += 2
			return appOut
		case '=':
			p.npos += 2
			return gequal
		}
		p.npos++
		return rdrOut
	case '+':
		switch byteAt(p.src, p.npos+1) {
		case '+':
			p.npos += 2
			return addAdd
		case '=':
			p.npos += 2
			return addAssgn
		}
		p.npos++
		return plus
	case '-':
		switch byteAt(p.src, p.npos+1) {
		case '-':
			p.npos += 2
			return subSub
		case '=':
			p.npos += 2
			return subAssgn
		}
		p.npos++
		return minus
	case '%':
		if byteAt(p.src, p.npos+1) == '=' {
			p.npos += 2
			return remAssgn
		}
		p.npos++
		return perc
	case '*':
		switch byteAt(p.src, p.npos+1) {
		case '*':
			p.npos += 2
			return power
		case '=':
			p.npos += 2
			return mulAssgn
		}
		p.npos++
		return star
	case '/':
		if byteAt(p.src, p.npos+1) == '=' {
			p.npos += 2
			return quoAssgn
		}
		p.npos++
		return slash
	case '^':
		if byteAt(p.src, p.npos+1) == '=' {
			p.npos += 2
			return xorAssgn
		}
		p.npos++
		return caret
	case ']':
		p.npos++
		return rightBrack
	case ',':
		p.npos++
		return comma
	case '?':
		p.npos++
		return quest
	default: // ':'
		p.npos++
		return colon
	}
}

func (p *parser) advanceLitOther() {
	bs := p.litBuf[:0]
	tok := _LitWord
loop:
	for p.npos < len(p.src) {
		r := rune(p.src[p.npos])
		switch r {
		case '\\': // escaped byte follows
			if p.npos++; p.npos == len(p.src) {
				bs = append(bs, '\\')
				break loop
			}
			r = rune(p.src[p.npos])
			p.npos++
			if r == '\n' {
				p.f.Lines = append(p.f.Lines, p.npos)
			} else {
				bs = append(bs, '\\', byte(r))
			}
			continue
		case '\n':
			switch p.quote {
			case sglQuotes, paramExpRepl, paramExpExp:
			default:
				break loop
			}
			p.f.Lines = append(p.f.Lines, p.npos+1)
		case '\'':
			switch p.quote {
			case paramExpExp, paramExpRepl:
			default:
				break loop
			}
		case '"', '`', '$':
			if p.quote != sglQuotes {
				tok = _Lit
				break loop
			}
		case '}':
			if p.quote&allParamExp != 0 {
				break loop
			}
		case '/':
			if p.quote&allParamExp != 0 && p.quote != paramExpExp {
				break loop
			}
		case ']':
			if p.quote&allRbrack != 0 {
				break loop
			}
		case '!', '*':
			if p.quote&allArithmExpr != 0 {
				break loop
			}
			if byteAt(p.src, p.npos+1) == '(' {
				tok = _Lit
				break loop
			}
		case ':', '=', '%', '?', '^', ',':
			if p.quote&allArithmExpr != 0 || p.quote&allParamReg != 0 {
				break loop
			}
			if r == '?' && byteAt(p.src, p.npos+1) == '(' {
				tok = _Lit
				break loop
			}
		case '#', '[':
			if p.quote&allParamReg != 0 {
				break loop
			}
		case '+', '-':
			switch p.quote {
			case paramExpInd, paramExpLen, paramExpOff,
				paramExpExp, paramExpRepl, sglQuotes:
			default:
				break loop
			}
			if r == '+' && byteAt(p.src, p.npos+1) == '(' {
				tok = _Lit
				break loop
			}
		case '@':
			if byteAt(p.src, p.npos+1) == '(' {
				tok = _Lit
				break loop
			}
		case ' ', '\t', ';', '&', '>', '<', '|', '(', ')', '\r':
			switch p.quote {
			case paramExpExp, paramExpRepl, sglQuotes:
			default:
				break loop
			}
		}
		bs = append(bs, byte(r))
		p.npos++
	}
	p.tok, p.val = tok, string(bs)
}

func (p *parser) advanceLitNone() {
	bs := p.litBuf[:0]
	p.asPos = 0
	tok := _LitWord
loop:
	for p.npos < len(p.src) {
		r := rune(p.src[p.npos])
		switch r {
		case '\\': // escaped byte follows
			if p.npos++; p.npos == len(p.src) {
				bs = append(bs, '\\')
				break loop
			}
			if r = rune(p.src[p.npos]); r == '\n' {
				p.npos++
				p.f.Lines = append(p.f.Lines, p.npos)
				continue
			}
			bs = append(bs, '\\')
		case '>', '<':
			if byteAt(p.src, p.npos+1) == '(' {
				tok = _Lit
			}
			break loop
		case ' ', '\t', '\n', '\r', '&', '|', ';', '(', ')':
			break loop
		case '`':
			if p.quote != subCmdBckquo {
				tok = _Lit
			}
			break loop
		case '"', '\'', '$':
			tok = _Lit
			break loop
		case '?', '*', '+', '@', '!':
			if byteAt(p.src, p.npos+1) == '(' {
				tok = _Lit
				break loop
			}
		case '=':
			p.asPos = len(bs)
			if p.bash() && p.asPos > 0 && p.src[p.npos-1] == '+' {
				p.asPos-- // a+=r
			}
		}
		bs = append(bs, byte(r))
		p.npos++
	}
	p.tok, p.val = tok, string(bs)
}

func (p *parser) advanceLitDquote() {
	bs := p.litBuf[:0]
	tok := _LitWord
loop:
	for p.npos < len(p.src) {
		r := rune(p.src[p.npos])
		switch r {
		case '\\': // escaped byte follows
			if p.npos++; p.npos == len(p.src) {
				break loop
			}
			bs = append(bs, '\\')
			if r = rune(p.src[p.npos]); r == '\n' {
				p.f.Lines = append(p.f.Lines, p.npos+1)
			}
		case '"':
			break loop
		case '`', '$':
			tok = _Lit
			break loop
		case '\n':
			p.f.Lines = append(p.f.Lines, p.npos+1)
		}
		bs = append(bs, byte(r))
		p.npos++
	}
	p.tok, p.val = tok, string(bs)
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
		for byteAt(p.src, n) == '\t' {
			n++
		}
	}
	if p.isHdocEnd(n) {
		p.tok, p.val = _LitWord, string(p.src[p.npos:n])
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
				for byteAt(p.src, n) == '\t' {
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

func (p *parser) hdocLitWord() *Word {
	pos := p.npos
	end := pos
	for p.npos < len(p.src) {
		end = p.npos
		bs, found := p.readLine()
		p.npos += len(bs) + 1
		if found {
			p.f.Lines = append(p.f.Lines, p.npos)
		}
		if p.quote == hdocBodyTabs {
			for byteAt(p.src, end) == '\t' {
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
	oldNpos := p.npos
	p.npos = end // since we're slicing until end
	l := p.lit(Pos(pos+1), string(p.src[pos:end]))
	p.npos = oldNpos
	return p.word(p.singleWps(l))
}

func (p *parser) readLine() ([]byte, bool) {
	rem := p.src[p.npos:]
	if i := bytes.IndexByte(rem, '\n'); i >= 0 {
		return rem[:i], true
	}
	return rem, false
}

func (p *parser) advanceLitRe() {
	lparens := 0
	bs := p.litBuf[:0]
byteLoop:
	for p.npos < len(p.src) {
		r := rune(p.src[p.npos])
		switch r {
		case '(':
			lparens++
		case ')':
			lparens--
		case ' ', '\t', '\r', '\n':
			if lparens == 0 {
				break byteLoop
			}
		}
		bs = append(bs, byte(r))
		p.npos++
	}
	p.tok, p.val = _LitWord, string(bs)
}

func testUnaryOp(val string) token {
	switch val {
	case "!":
		return exclMark
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

func testBinaryOp(val string) token {
	switch val {
	case "=":
		return assgn
	case "==":
		return equal
	case "!=":
		return nequal
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
