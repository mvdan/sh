// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import "fmt"

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
	Column int // column number, starting at 1
}

// Position describes an arbitrary position in a source file. Offsets,
// including column numbers, are in bytes.
type Position struct {
	Filename string
	Line     int // line number, starting at 1
	Column   int // column number, starting at 1
}

func (p Position) String() string {
	if p.Filename == "" {
		return fmt.Sprintf("%d:%d", p.Line, p.Column)
	}
	return fmt.Sprintf("%s:%d:%d", p.Filename, p.Line, p.Column)
}

func init() {
	for tok, str := range reservedWords {
		tokNames[tok] = str
	}
	for _, list := range [...][]tokEntry{regList, arithmList, paramList} {
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
	}
	reservedWords = map[Token]string{
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
	}

	regList = []tokEntry{
		{"'", SQUOTE},
		{`"`, DQUOTE},
		{"`", BQUOTE},

		{"&", AND},
		{"&&", LAND},
		{"|", OR},
		{"||", LOR},

		{"$", DOLLAR},
		{"$'", DOLLSQ},
		{`$"`, DOLLDQ},
		{`${`, DOLLBR},
		{`$(`, DOLLPR},
		{`$((`, DOLLDP},
		{"(", LPAREN},
		{")", RPAREN},
		{";", SEMICOLON},
		{";;", DSEMICOLON},

		{"<", LSS},
		{">", GTR},
		{"<<", SHL},
		{">>", SHR},
		{"|&", PIPEALL},
		{"<>", RDRINOUT},
		{"<&", DPLIN},
		{">&", DPLOUT},
		{"<<-", DHEREDOC},
		{"<<<", WHEREDOC},
		{"<(", CMDIN},
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
		if p.readOnly(t.str) {
			return t.tok
		}
	}
	return ILLEGAL
}
func (p *parser) doRegToken() Token    { return p.doToken(regList) }
func (p *parser) doParamToken() Token  { return p.doToken(paramList) }
func (p *parser) doArithmToken() Token { return p.doToken(arithmList) }
