// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

// Token is the set of lexical tokens.
type Token int

// The list of all possible tokens.
const (
	ILLEGAL Token = iota
	STOPPED
	EOF
	LIT
	LITWORD

	// Rest of tokens
	SQUOTE // '
	DQUOTE // "
	BQUOTE // `

	AND  // &
	LAND // &&
	OR   // |
	LOR  // ||

	ASSIGN // =
	DOLLAR // $
	DOLLSQ // $'
	DOLLDQ // $"
	DOLLBR // ${
	DOLLPR // $(
	DOLLDP // $((
	LET    // let
	LBRACE // {
	LPAREN // (

	RBRACE     // }
	RPAREN     // )
	SEMICOLON  // ;
	DSEMICOLON // ;;
	SEMIFALL   // ;&
	DSEMIFALL  // ;;&
	COLON      // :

	LSS // <
	GTR // >
	SHL // <<
	SHR // >>

	ADD   // +
	SUB   // -
	REM   // %
	MUL   // *
	QUO   // /
	XOR   // ^
	NOT   // !
	INC   // ++
	DEC   // --
	POW   // **
	COMMA // ,
	EQL   // ==
	NEQ   // !=
	LEQ   // <=
	GEQ   // >=

	ADDASSGN // +=
	SUBASSGN // -=
	MULASSGN // *=
	QUOASSGN // /=
	REMASSGN // %=
	ANDASSGN // &=
	ORASSGN  // |=
	XORASSGN // ^=
	SHLASSGN // <<=
	SHRASSGN // >>=

	PIPEALL  // |&
	RDRINOUT // <>
	DPLIN    // <&
	DPLOUT   // >&
	DHEREDOC // <<-
	WHEREDOC // <<<
	CMDIN    // <(
	CMDOUT   // >(
	RDRALL   // &>
	APPALL   // &>>

	CADD    // :+
	CSUB    // :-
	QUEST   // ?
	CQUEST  // :?
	CASSIGN // :=
	DREM    // %%
	HASH    // #
	DHASH   // ##
	LBRACK  // [
	RBRACK  // ]
	DQUO    // //

	DLPAREN // ((
	DRPAREN // ))
)

// Pos is the internal representation of a position within a source
// file.
type Pos int

// Position describes a position within a source file including the line
// and column location. A Position is valid if the line number is > 0.
type Position struct {
	Offset int // offset, starting at 0
	Line   int // line number, starting at 1
	Column int // column number, starting at 1 (byte count)
}

func (f *File) Position(p Pos) (pos Position) {
	off := int(p)
	pos.Offset = off
	if i := searchInts(f.lines, off); i >= 0 {
		pos.Line, pos.Column = i+1, off-f.lines[i]
	}
	return
}

// Inlined version of:
// sort.Search(len(a), func(i int) bool { return a[i] > x }) - 1
func searchInts(a []int, x int) int {
	i, j := 0, len(a)
	for i < j {
		h := i + (j-i)/2
		if a[h] <= x {
			i = h + 1
		} else {
			j = h
		}
	}
	return i - 1
}

func posMax(p1, p2 Pos) Pos {
	if p2 > p1 {
		return p2
	}
	return p1
}

var (
	tokNames = map[Token]string{
		ILLEGAL: `ILLEGAL`,
		STOPPED: `STOPPED`,
		EOF:     `EOF`,
		LIT:     `lit`,
		LITWORD: `litword`,

		DLPAREN: "((",
		DRPAREN: "))",

		SQUOTE: `'`,
		DQUOTE: `"`,
		BQUOTE: "`",

		AND:  "&",
		LAND: "&&",
		OR:   "|",
		LOR:  "||",

		DOLLAR:     "$",
		DOLLSQ:     "$'",
		DOLLDQ:     `$"`,
		DOLLBR:     `${`,
		DOLLPR:     `$(`,
		DOLLDP:     `$((`,
		LET:        "let",
		LBRACE:     "{",
		LPAREN:     "(",
		RBRACE:     "}",
		RPAREN:     ")",
		SEMICOLON:  ";",
		DSEMICOLON: ";;",
		SEMIFALL:   ";&",
		DSEMIFALL:  ";;&",

		LSS:      "<",
		GTR:      ">",
		SHL:      "<<",
		SHR:      ">>",
		PIPEALL:  "|&",
		RDRINOUT: "<>",
		DPLIN:    "<&",
		DPLOUT:   ">&",
		DHEREDOC: "<<-",
		WHEREDOC: "<<<",
		CMDIN:    "<(",
		CMDOUT:   ">(",
		RDRALL:   "&>",
		APPALL:   "&>>",

		COLON:   ":",
		ADD:     "+",
		CADD:    ":+",
		SUB:     "-",
		CSUB:    ":-",
		QUEST:   "?",
		CQUEST:  ":?",
		ASSIGN:  "=",
		CASSIGN: ":=",
		REM:     "%",
		DREM:    "%%",
		HASH:    "#",
		DHASH:   "##",
		LBRACK:  "[",
		RBRACK:  "]",
		QUO:     "/",
		DQUO:    "//",

		MUL:   "*",
		XOR:   "^",
		NOT:   "!",
		INC:   "++",
		DEC:   "--",
		POW:   "**",
		COMMA: ",",
		EQL:   "==",
		NEQ:   "!=",
		LEQ:   "<=",
		GEQ:   ">=",

		ADDASSGN: "+=",
		SUBASSGN: "-=",
		MULASSGN: "*=",
		QUOASSGN: "/=",
		REMASSGN: "%=",
		ANDASSGN: "&=",
		ORASSGN:  "|=",
		XORASSGN: "^=",
		SHLASSGN: "<<=",
		SHRASSGN: ">>=",
	}
)

func (t Token) String() string { return tokNames[t] }

func byteAt(src []byte, i int) byte {
	if i >= len(src) {
		return 0
	}
	return src[i]
}

func (p *parser) doRegToken(b byte) Token {
	switch b {
	case '\'':
		p.npos++
		return SQUOTE
	case '"':
		p.npos++
		return DQUOTE
	case '`':
		p.npos++
		return BQUOTE
	case '&':
		switch byteAt(p.src, p.npos+1) {
		case '&':
			p.npos += 2
			return LAND
		case '>':
			if byteAt(p.src, p.npos+2) == '>' {
				p.npos += 3
				return APPALL
			}
			p.npos += 2
			return RDRALL
		}
		p.npos++
		return AND
	case '|':
		switch byteAt(p.src, p.npos+1) {
		case '|':
			p.npos += 2
			return LOR
		case '&':
			p.npos += 2
			return PIPEALL
		}
		p.npos++
		return OR
	case '$':
		switch byteAt(p.src, p.npos+1) {
		case '\'':
			p.npos += 2
			return DOLLSQ
		case '"':
			p.npos += 2
			return DOLLDQ
		case '{':
			p.npos += 2
			return DOLLBR
		case '(':
			if byteAt(p.src, p.npos+2) == '(' {
				p.npos += 3
				return DOLLDP
			}
			p.npos += 2
			return DOLLPR
		}
		p.npos++
		return DOLLAR
	case '(':
		p.npos++
		return LPAREN
	case ')':
		p.npos++
		return RPAREN
	case ';':
		switch byteAt(p.src, p.npos+1) {
		case ';':
			if byteAt(p.src, p.npos+2) == '&' {
				p.npos += 3
				return DSEMIFALL
			}
			p.npos += 2
			return DSEMICOLON
		case '&':
			p.npos += 2
			return SEMIFALL
		}
		p.npos++
		return SEMICOLON
	case '<':
		switch byteAt(p.src, p.npos+1) {
		case '<':
			switch byteAt(p.src, p.npos+2) {
			case '-':
				p.npos += 3
				return DHEREDOC
			case '<':
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
		case '(':
			p.npos += 2
			return CMDOUT
		}
		p.npos++
		return GTR
	}
}

func (p *parser) doDqToken(b byte) Token {
	switch b {
	case '"':
		return DQUOTE
	case '`':
		return BQUOTE
	default: // '$'
		switch {
		case p.readOnly('{'):
			return DOLLBR
		case p.readOnly('('):
			if p.readOnly('(') {
				return DOLLDP
			}
			return DOLLPR
		}
		return DOLLAR
	}
}

func (p *parser) doParamToken(b byte) Token {
	switch b {
	case '}':
		return RBRACE
	case ':':
		switch {
		case p.readOnly('+'):
			return CADD
		case p.readOnly('-'):
			return CSUB
		case p.readOnly('?'):
			return CQUEST
		case p.readOnly('='):
			return CASSIGN
		}
		return COLON
	case '+':
		return ADD
	case '-':
		return SUB
	case '?':
		return QUEST
	case '=':
		return ASSIGN
	case '%':
		if p.readOnly('%') {
			return DREM
		}
		return REM
	case '#':
		if p.readOnly('#') {
			return DHASH
		}
		return HASH
	case '[':
		return LBRACK
	default: // '/'
		if p.readOnly('/') {
			return DQUO
		}
		return QUO
	}
}

func (p *parser) doArithmToken(b byte) Token {
	switch b {
	case '!':
		if p.readOnly('=') {
			return NEQ
		}
		return NOT
	case '=':
		if p.readOnly('=') {
			return EQL
		}
		return ASSIGN
	case '(':
		return LPAREN
	case ')':
		return RPAREN
	case '&':
		if p.readOnly('&') {
			return LAND
		}
		if p.readOnly('=') {
			return ANDASSGN
		}
		return AND
	case '|':
		if p.readOnly('|') {
			return LOR
		}
		if p.readOnly('=') {
			return ORASSGN
		}
		return OR
	case '<':
		switch {
		case p.readOnly('<'):
			if p.readOnly('=') {
				return SHLASSGN
			}
			return SHL
		case p.readOnly('='):
			return LEQ
		}
		return LSS
	case '>':
		switch {
		case p.readOnly('>'):
			if p.readOnly('=') {
				return SHRASSGN
			}
			return SHR
		case p.readOnly('='):
			return GEQ
		}
		return GTR
	case '+':
		if p.readOnly('+') {
			return INC
		}
		if p.readOnly('=') {
			return ADDASSGN
		}
		return ADD
	case '-':
		if p.readOnly('-') {
			return DEC
		}
		if p.readOnly('=') {
			return SUBASSGN
		}
		return SUB
	case '%':
		if p.readOnly('=') {
			return REMASSGN
		}
		return REM
	case '*':
		if p.readOnly('*') {
			return POW
		}
		if p.readOnly('=') {
			return MULASSGN
		}
		return MUL
	case '/':
		if p.readOnly('=') {
			return QUOASSGN
		}
		return QUO
	case '^':
		if p.readOnly('=') {
			return XORASSGN
		}
		return XOR
	case ',':
		return COMMA
	case '?':
		return QUEST
	default: // ':'
		return COLON
	}
}
