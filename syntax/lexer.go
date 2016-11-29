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

func (p *parser) rune() rune {
	p.npos++
	r := byteAt(p.src, p.npos)
	if r == '\n' {
		p.f.Lines = append(p.f.Lines, p.npos+1)
	}
	return r
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
			p.advanceLitOther(r)
		}
		return
	case dblQuotes:
		switch r {
		case '`', '"', '$':
			p.tok = p.dqToken(r)
		default:
			p.advanceLitDquote(r)
		}
		return
	case hdocBody, hdocBodyTabs:
		if r == '`' || r == '$' {
			p.tok = p.dqToken(r)
		} else if p.hdocStop == nil {
			p.tok = illegalTok
		} else {
			p.advanceLitHdoc(r)
		}
		return
	case paramExpExp:
		switch r {
		case '}':
			p.rune()
			p.tok = rightBrace
		case '`', '"', '$':
			p.tok = p.dqToken(r)
		default:
			p.advanceLitOther(r)
		}
		return
	case sglQuotes:
		if r == '\'' {
			p.rune()
			p.tok = sglQuote
		} else {
			p.advanceLitOther(r)
		}
		return
	}
skipSpace:
	for {
		switch r {
		case ' ', '\t', '\r':
			p.spaced = true
			r = p.rune()
		case '\n':
			p.spaced, p.newLine = true, true
			if p.quote == arithmExprLet {
				p.tok = illegalTok
				return
			}
			r = p.rune()
			if len(p.heredocs) > p.buriedHdocs {
				if p.doHeredocs(); p.tok == _EOF {
					return
				}
				r = byteAt(p.src, p.npos)
			}
		case '\\':
			if byteAt(p.src, p.npos+1) == '\n' {
				p.rune()
				r = p.rune()
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
	}
	p.pos = Pos(p.npos + 1)
	switch {
	case p.quote&allRegTokens != 0:
		switch r {
		case ';', '"', '\'', '(', ')', '$', '|', '&', '>', '<', '`':
			p.tok = p.regToken(r)
		case '#':
			p.rune()
			bs, _ := p.readLine(p.litBuf[:0])
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
				p.rune()
				p.rune()
			} else {
				p.advanceLitNone(r)
			}
		default:
			p.advanceLitNone(r)
		}
	case p.quote&allArithmExpr != 0 && arithmOps(r):
		p.tok = p.arithmToken(r)
	case p.quote&allParamExp != 0 && paramOps(r):
		p.tok = p.paramToken(r)
	case p.quote == testRegexp:
		if regOps(r) && r != '(' {
			p.tok = p.regToken(r)
		} else {
			p.advanceLitRe(r)
		}
	case regOps(r):
		p.tok = p.regToken(r)
	default:
		p.advanceLitOther(r)
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
		p.rune()
		return sglQuote
	case '"':
		p.rune()
		return dblQuote
	case '`':
		p.rune()
		return bckQuote
	case '&':
		switch p.rune() {
		case '&':
			p.rune()
			return andAnd
		case '>':
			if !p.bash() {
				break
			}
			if p.rune() == '>' {
				p.rune()
				return appAll
			}
			return rdrAll
		}
		return and
	case '|':
		switch p.rune() {
		case '|':
			p.rune()
			return orOr
		case '&':
			if !p.bash() {
				break
			}
			p.rune()
			return pipeAll
		}
		return or
	case '$':
		switch p.rune() {
		case '\'':
			if !p.bash() {
				break
			}
			p.rune()
			return dollSglQuote
		case '"':
			if !p.bash() {
				break
			}
			p.rune()
			return dollDblQuote
		case '{':
			p.rune()
			return dollBrace
		case '[':
			if !p.bash() {
				break
			}
			p.rune()
			return dollBrack
		case '(':
			if p.rune() == '(' {
				p.rune()
				return dollDblParen
			}
			return dollParen
		}
		return dollar
	case '(':
		if p.rune() == '(' && p.bash() {
			p.rune()
			return dblLeftParen
		}
		return leftParen
	case ')':
		p.rune()
		return rightParen
	case ';':
		switch p.rune() {
		case ';':
			if p.rune() == '&' && p.bash() {
				p.rune()
				return dblSemiFall
			}
			return dblSemicolon
		case '&':
			if !p.bash() {
				break
			}
			p.rune()
			return semiFall
		}
		return semicolon
	case '<':
		switch p.rune() {
		case '<':
			if r = p.rune(); r == '-' {
				p.rune()
				return dashHdoc
			} else if r == '<' && p.bash() {
				p.rune()
				return wordHdoc
			}
			return hdoc
		case '>':
			p.rune()
			return rdrInOut
		case '&':
			p.rune()
			return dplIn
		case '(':
			if !p.bash() {
				break
			}
			p.rune()
			return cmdIn
		}
		return rdrIn
	default: // '>'
		switch p.rune() {
		case '>':
			p.rune()
			return appOut
		case '&':
			p.rune()
			return dplOut
		case '|':
			p.rune()
			return clbOut
		case '(':
			if !p.bash() {
				break
			}
			p.rune()
			return cmdOut
		}
		return rdrOut
	}
}

func (p *parser) dqToken(r rune) token {
	switch r {
	case '"':
		p.rune()
		return dblQuote
	case '`':
		p.rune()
		return bckQuote
	default: // '$'
		switch p.rune() {
		case '{':
			p.rune()
			return dollBrace
		case '[':
			if !p.bash() {
				break
			}
			p.rune()
			return dollBrack
		case '(':
			if p.rune() == '(' {
				p.rune()
				return dollDblParen
			}
			return dollParen
		}
		return dollar
	}
}

func (p *parser) paramToken(r rune) token {
	switch r {
	case '}':
		p.rune()
		return rightBrace
	case ':':
		switch p.rune() {
		case '+':
			p.rune()
			return colPlus
		case '-':
			p.rune()
			return colMinus
		case '?':
			p.rune()
			return colQuest
		case '=':
			p.rune()
			return colAssgn
		}
		return colon
	case '+':
		p.rune()
		return plus
	case '-':
		p.rune()
		return minus
	case '?':
		p.rune()
		return quest
	case '=':
		p.rune()
		return assgn
	case '%':
		if p.rune() == '%' {
			p.rune()
			return dblPerc
		}
		return perc
	case '#':
		if p.rune() == '#' {
			p.rune()
			return dblHash
		}
		return hash
	case '[':
		p.rune()
		return leftBrack
	case '^':
		if p.rune() == '^' {
			p.rune()
			return dblCaret
		}
		return caret
	case ',':
		if p.rune() == ',' {
			p.rune()
			return dblComma
		}
		return comma
	default: // '/'
		if p.rune() == '/' {
			p.rune()
			return dblSlash
		}
		return slash
	}
}

func (p *parser) arithmToken(r rune) token {
	switch r {
	case '!':
		if p.rune() == '=' {
			p.rune()
			return nequal
		}
		return exclMark
	case '=':
		if p.rune() == '=' {
			p.rune()
			return equal
		}
		return assgn
	case '(':
		p.rune()
		return leftParen
	case ')':
		p.rune()
		return rightParen
	case '&':
		switch p.rune() {
		case '&':
			p.rune()
			return andAnd
		case '=':
			p.rune()
			return andAssgn
		}
		return and
	case '|':
		switch p.rune() {
		case '|':
			p.rune()
			return orOr
		case '=':
			p.rune()
			return orAssgn
		}
		return or
	case '<':
		switch p.rune() {
		case '<':
			if p.rune() == '=' {
				p.rune()
				return shlAssgn
			}
			return hdoc
		case '=':
			p.rune()
			return lequal
		}
		return rdrIn
	case '>':
		switch p.rune() {
		case '>':
			if p.rune() == '=' {
				p.rune()
				return shrAssgn
			}
			return appOut
		case '=':
			p.rune()
			return gequal
		}
		return rdrOut
	case '+':
		switch p.rune() {
		case '+':
			p.rune()
			return addAdd
		case '=':
			p.rune()
			return addAssgn
		}
		return plus
	case '-':
		switch p.rune() {
		case '-':
			p.rune()
			return subSub
		case '=':
			p.rune()
			return subAssgn
		}
		return minus
	case '%':
		if p.rune() == '=' {
			p.rune()
			return remAssgn
		}
		return perc
	case '*':
		switch p.rune() {
		case '*':
			p.rune()
			return power
		case '=':
			p.rune()
			return mulAssgn
		}
		return star
	case '/':
		if p.rune() == '=' {
			p.rune()
			return quoAssgn
		}
		return slash
	case '^':
		if p.rune() == '=' {
			p.rune()
			return xorAssgn
		}
		return caret
	case ']':
		p.rune()
		return rightBrack
	case ',':
		p.rune()
		return comma
	case '?':
		p.rune()
		return quest
	default: // ':'
		p.rune()
		return colon
	}
}

func (p *parser) advanceLitOther(r rune) {
	bs := p.litBuf[:0]
	tok := _LitWord
loop:
	for p.npos < len(p.src) {
		switch r {
		case '\\': // escaped byte follows
			if r = p.rune(); p.npos == len(p.src) {
				bs = append(bs, '\\')
				break loop
			}
			if r != '\n' {
				bs = append(bs, '\\', byte(r))
			}
			r = p.rune()
			continue
		case '\n':
			switch p.quote {
			case sglQuotes, paramExpRepl, paramExpExp:
			default:
				break loop
			}
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
		r = p.rune()
	}
	p.tok, p.val = tok, string(bs)
}

func (p *parser) advanceLitNone(r rune) {
	bs := p.litBuf[:0]
	p.asPos = 0
	tok := _LitWord
loop:
	for p.npos < len(p.src) {
		switch r {
		case '\\': // escaped byte follows
			if r = p.rune(); p.npos == len(p.src) {
				bs = append(bs, '\\')
				break loop
			}
			if r == '\n' {
				r = p.rune()
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
		r = p.rune()
	}
	p.tok, p.val = tok, string(bs)
}

func (p *parser) advanceLitDquote(r rune) {
	bs := p.litBuf[:0]
	tok := _LitWord
loop:
	for p.npos < len(p.src) {
		switch r {
		case '\\': // escaped byte follows
			if r = p.rune(); p.npos == len(p.src) {
				break loop
			}
			bs = append(bs, '\\')
		case '"':
			break loop
		case '`', '$':
			tok = _Lit
			break loop
		}
		bs = append(bs, byte(r))
		r = p.rune()
	}
	p.tok, p.val = tok, string(bs)
}

func (p *parser) advanceLitHdoc(r rune) {
	bs := p.litBuf[:0]
	if p.quote == hdocBodyTabs {
		for r == '\t' {
			bs = append(bs, byte(r))
			r = p.rune()
		}
	}
	endOff := len(bs)
loop:
	for p.npos < len(p.src) {
		switch r {
		case '\\': // escaped byte follows
			bs = append(bs, byte(r))
			if r = p.rune(); p.npos == len(p.src) {
				break loop
			}
		case '`', '$':
			break loop
		case '\n':
			if bytes.Equal(bs[endOff:], p.hdocStop) {
				bs = bs[:endOff]
				p.hdocStop = nil
				break loop
			}
			bs = append(bs, byte(r))
			r = p.rune()
			if p.quote == hdocBodyTabs {
				for r == '\t' {
					bs = append(bs, byte(r))
					r = p.rune()
				}
			}
			if p.npos >= len(p.src) {
				break loop
			}
			endOff = len(bs)
		}
		bs = append(bs, byte(r))
		r = p.rune()
	}
	if bytes.Equal(bs[endOff:], p.hdocStop) {
		p.hdocStop = nil
		bs = bs[:endOff]
	}
	p.tok, p.val = _Lit, string(bs)
}

func (p *parser) hdocLitWord() *Word {
	bs := p.litBuf[:0]
	r := byteAt(p.src, p.npos)
	pos := p.npos
	for p.npos < len(p.src) {
		if p.quote == hdocBodyTabs {
			for r == '\t' {
				bs = append(bs, byte(r))
				r = p.rune()
			}
		}
		endOff := len(bs)
		var found bool
		bs, found = p.readLine(bs)
		if bytes.Equal(bs[endOff:], p.hdocStop) {
			bs = bs[:endOff]
			break
		}
		r = byteAt(p.src, p.npos)
		if found {
			bs = append(bs, byte(r))
			r = p.rune()
		}
	}
	l := p.lit(Pos(pos+1), string(bs))
	return p.word(p.singleWps(l))
}

func (p *parser) readLine(bs []byte) ([]byte, bool) {
	rem := p.src[p.npos:]
	if i := bytes.IndexByte(rem, '\n'); i >= 0 {
		if i > 0 {
			p.npos += i
			p.f.Lines = append(p.f.Lines, p.npos+1)
			bs = append(bs, rem[:i]...)
		}
		return bs, true
	}
	p.npos = len(p.src)
	bs = append(bs, rem...)
	return bs, false
}

func (p *parser) advanceLitRe(r rune) {
	lparens := 0
	bs := p.litBuf[:0]
byteLoop:
	for p.npos < len(p.src) {
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
		r = p.rune()
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
