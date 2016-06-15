// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

// Token is the set of lexical tokens.
type Token int

// The list of all possible tokens.
const (
	ILLEGAL Token = -iota
	STOPPED
	EOF
	LIT
	COMMENT

	// POSIX Shell reserved words
	IF
	THEN
	ELIF
	ELSE
	FI
	WHILE
	FOR
	IN
	UNTIL
	DO
	DONE
	CASE
	ESAC

	SQUOTE // '
	DQUOTE // "
	BQUOTE // `

	NOT    // !
	LBRACE // {
	RBRACE // }

	// Bash reserved words
	FUNCTION
	DECLARE
	LOCAL
	EVAL
	LET

	// Rest of tokens
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
	LPAREN // (

	RPAREN     // )
	SEMICOLON  // ;
	DSEMICOLON // ;;
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
	INC   // ++
	DEC   // --
	POW   // **
	COMMA // ,
	NEQ   // !=
	LEQ   // <=
	GEQ   // >=

	ADD_ASSIGN // +=
	SUB_ASSIGN // -=
	MUL_ASSIGN // *=
	QUO_ASSIGN // /=
	REM_ASSIGN // %=
	AND_ASSIGN // &=
	OR_ASSIGN  // |=
	XOR_ASSIGN // ^=
	SHL_ASSIGN // <<=
	SHR_ASSIGN // >>=

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

func init() {
	for _, list := range [...][]tokEntry{arithmList, paramList} {
		for _, t := range list {
			tokNames[t.tok] = t.str
		}
	}
}

type tokEntry struct {
	str string
	tok Token
}

var (
	tokNames = map[Token]string{
		ILLEGAL: `ILLEGAL`,
		STOPPED: `STOPPED`,
		EOF:     `EOF`,
		LIT:     `literal`,
		COMMENT: `comment`,

		DLPAREN: "((",
		DRPAREN: "))",

		IF:    "if",
		THEN:  "then",
		ELIF:  "elif",
		ELSE:  "else",
		FI:    "fi",
		WHILE: "while",
		FOR:   "for",
		IN:    "in",
		UNTIL: "until",
		DO:    "do",
		DONE:  "done",
		CASE:  "case",
		ESAC:  "esac",

		NOT:    "!",
		LBRACE: "{",
		RBRACE: "}",

		FUNCTION: "function",
		DECLARE:  "declare",
		LOCAL:    "local",
		EVAL:     "eval",
		LET:      "let",

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
		LPAREN:     "(",
		RPAREN:     ")",
		SEMICOLON:  ";",
		DSEMICOLON: ";;",

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
	}

	paramList = []tokEntry{
		{":", COLON},
		{"+", ADD},
		{":+", CADD},
		{"-", SUB},
		{":-", CSUB},
		{"?", QUEST},
		{":?", CQUEST},
		{"=", ASSIGN},
		{":=", CASSIGN},
		{"%", REM},
		{"%%", DREM},
		{"#", HASH},
		{"##", DHASH},
		{"[", LBRACK},
		{"]", RBRACK},
		{"/", QUO},
		{"//", DQUO},
	}
	arithmList = []tokEntry{
		{"!", NOT},
		{"=", ASSIGN},
		{"(", LPAREN},
		{")", RPAREN},
		{"&", AND},
		{"&&", LAND},
		{"|", OR},
		{"||", LOR},
		{"<", LSS},
		{">", GTR},
		{"<<", SHL},
		{">>", SHR},

		{"+", ADD},
		{"-", SUB},
		{"%", REM},
		{"*", MUL},
		{"/", QUO},
		{"^", XOR},
		{"++", INC},
		{"--", DEC},
		{"**", POW},
		{",", COMMA},
		{"!=", NEQ},
		{"<=", LEQ},
		{">=", GEQ},
		{"?", QUEST},
		{":", COLON},

		{"+=", ADD_ASSIGN},
		{"-=", SUB_ASSIGN},
		{"*=", MUL_ASSIGN},
		{"/=", QUO_ASSIGN},
		{"%=", REM_ASSIGN},
		{"&=", AND_ASSIGN},
		{"|=", OR_ASSIGN},
		{"^=", XOR_ASSIGN},
		{"<<=", SHL_ASSIGN},
		{">>=", SHR_ASSIGN},
	}
)

func (t Token) String() string { return tokNames[t] }

func (p *parser) doToken(tokList []tokEntry) Token {
	// In reverse, to not treat e.g. && as & two times
	for i := len(tokList) - 1; i >= 0; i-- {
		t := tokList[i]
		if p.readOnlyStr(t.str) {
			return t.tok
		}
	}
	return ILLEGAL
}

// TODO: decouple from parser. Passing readOnly as a func argument
// doesn't seem to work well as it means an extra allocation (?).
func (p *parser) doRegToken() Token {
	switch {
	case p.readOnly('\''):
		return SQUOTE
	case p.readOnly('"'):
		return DQUOTE
	case p.readOnly('`'):
		return BQUOTE
	case p.readOnly('&'):
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
	case p.readOnly('|'):
		switch {
		case p.readOnly('|'):
			return LOR
		case p.readOnly('&'):
			return PIPEALL
		}
		return OR
	case p.readOnly('$'):
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
	case p.readOnly('('):
		return LPAREN
	case p.readOnly(')'):
		return RPAREN
	case p.readOnly(';'):
		if p.readOnly(';') {
			return DSEMICOLON
		}
		return SEMICOLON
	case p.readOnly('<'):
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
	case p.readOnly('>'):
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
func (p *parser) doParamToken() Token  { return p.doToken(paramList) }
func (p *parser) doArithmToken() Token { return p.doToken(arithmList) }
