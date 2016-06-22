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
	COMMENT

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
type Pos struct {
	Line   int // line number, starting at 1
	Column int // column number (in bytes), starting at 1
}

func posGreater(p1, p2 Pos) bool {
	if p1.Line == p2.Line {
		return p1.Column > p2.Column
	}
	return p1.Line > p2.Line
}

func posMax(p1, p2 Pos) Pos {
	if posGreater(p2, p1) {
		return p2
	}
	return p1
}

var (
	tokNames = map[Token]string{
		ILLEGAL: `ILLEGAL`,
		STOPPED: `STOPPED`,
		EOF:     `EOF`,
		LIT:     `literal`,
		LITWORD: `literal`,
		COMMENT: `comment`,

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

// TODO: decouple from parser. Passing readOnly as a func argument
// doesn't seem to work well as it means an extra allocation (?).
func (p *parser) doRegToken(b byte) Token {
	switch b {
	case '\'':
		return SQUOTE
	case '"':
		return DQUOTE
	case '`':
		return BQUOTE
	case '&':
		switch {
		case p.readOnly('&'):
			return LAND
		case p.readOnly('>'):
			if p.readOnly('>') {
				return APPALL
			}
			return RDRALL
		}
		return AND
	case '|':
		switch {
		case p.readOnly('|'):
			return LOR
		case p.readOnly('&'):
			return PIPEALL
		}
		return OR
	case '$':
		switch {
		case p.readOnly('\''):
			return DOLLSQ
		case p.readOnly('"'):
			return DOLLDQ
		case p.readOnly('{'):
			return DOLLBR
		case p.readOnly('('):
			if p.readOnly('(') {
				return DOLLDP
			}
			return DOLLPR
		}
		return DOLLAR
	case '(':
		return LPAREN
	case ')':
		return RPAREN
	case ';':
		if p.readOnly(';') {
			if p.readOnly('&') {
				return DSEMIFALL
			}
			return DSEMICOLON
		}
		if p.readOnly('&') {
			return SEMIFALL
		}
		return SEMICOLON
	case '<':
		switch {
		case p.readOnly('<'):
			if p.readOnly('-') {
				return DHEREDOC
			}
			if p.readOnly('<') {
				return WHEREDOC
			}
			return SHL
		case p.readOnly('>'):
			return RDRINOUT
		case p.readOnly('&'):
			return DPLIN
		case p.readOnly('('):
			return CMDIN
		}
		return LSS
	case '>':
		switch {
		case p.readOnly('>'):
			return SHR
		case p.readOnly('&'):
			return DPLOUT
		case p.readOnly('('):
			return CMDOUT
		}
		return GTR
	}
	return ILLEGAL
}

func (p *parser) doParamToken(b byte) Token {
	switch b {
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
	case '/':
		if p.readOnly('/') {
			return DQUO
		}
		return QUO
	}
	return ILLEGAL
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
	case ':':
		return COLON
	}
	return ILLEGAL
}
