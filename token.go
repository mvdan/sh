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

	NOT    // !
	LBRACE // {
	RBRACE // }

	// Bash reserved words
	FUNCTION

	// Rest of tokens
	AND  // &
	LAND // &&
	OR   // |
	LOR  // ||

	ASSIGN // =
	DOLLAR // $
	LPAREN // (

	RPAREN     // )
	SEMICOLON  // ;
	DSEMICOLON // ;;

	RDRIN    // <
	RDROUT   // >
	RDRINOUT // <>
	DPLIN    // <&
	DPLOUT   // >&
	APPEND   // >>
	HEREDOC  // <<
	DHEREDOC // <<-
	WHEREDOC // <<<

	ADD // +
	SUB // -
	REM // %
	MUL // *
	QUO // /
	INC // ++
	DEC // --

	CADD    // :+
	CSUB    // :-
	QUEST   // ?
	CQUEST  // :?
	CASSIGN // :=
	DREM    // %%
	HASH    // #
	DHASH   // ##

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
	for _, t := range tokList {
		tokNames[t.tok] = t.str
	}
}

var (
	tokNames = map[Token]string{
		ILLEGAL: `ILLEGAL`,
		STOPPED: `STOPPED`,
		EOF:     `EOF`,
		LIT:     `literal`,
		COMMENT: `comment`,

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

		DLPAREN: "((",
		DRPAREN: "))",
	}

	tokList = [...]struct {
		str string
		tok Token
	}{
		{"&", AND},
		{"&&", LAND},
		{"|", OR},
		{"||", LOR},

		{"=", ASSIGN},
		{"$", DOLLAR},
		{"(", LPAREN},

		{")", RPAREN},
		{";", SEMICOLON},
		{";;", DSEMICOLON},

		{"<", RDRIN},
		{">", RDROUT},
		{"<>", RDRINOUT},
		{"<&", DPLIN},
		{">&", DPLOUT},
		{">>", APPEND},
		{"<<", HEREDOC},
		{"<<-", DHEREDOC},
		{"<<<", WHEREDOC},

		{"+", ADD},
		{"-", SUB},
		{"%", REM},
		{"*", MUL},
		{"/", QUO},
		{"++", INC},
		{"--", DEC},

		{":+", CADD},
		{":-", CSUB},
		{"?", QUEST},
		{":?", CQUEST},
		{":=", CASSIGN},
		{"%%", DREM},
		{"#", HASH},
		{"##", DHASH},
	}
)

func (t Token) String() string {
	if s, e := tokNames[t]; e {
		return s
	}
	return string(t)
}

func (p *parser) doToken(b byte) Token {
	// In reverse, to not treat e.g. && as & two times
	for i := len(tokList) - 1; i >= 0; i-- {
		t := tokList[i]
		if p.readOnly(t.str) {
			return t.tok
		}
	}
	p.consumeByte()
	return Token(b)
}
