// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

type Token int

const (
	ILLEGAL Token = -iota
	EOF
	COMMENT
	LIT

	IF
	THEN
	ELIF
	ELSE
	FI
	WHILE
	FOR
	IN
	DO
	DONE

	AND  // &
	LAND // &&
	OR   // |
	LOR  // ||

	EXP    // $
	LPAREN // (
	LBRACE // {

	RPAREN     // )
	RBRACE     // }
	SEMICOLON  // ;
	DSEMICOLON // ;;

	LSS // <
	GTR // >
	SHR // >>
)

var tokNames = map[Token]string{
	ILLEGAL: `ILLEGAL`,
	EOF:     `EOF`,
	COMMENT: `comment`,
	LIT:     `word`, // TODO: fix inconsistency

	IF:    "if",
	THEN:  "then",
	ELIF:  "elif",
	ELSE:  "else",
	FI:    "fi",
	WHILE: "while",
	FOR:   "for",
	IN:    "in",
	DO:    "do",
	DONE:  "done",

	AND:  "&",
	LAND: "&&",
	OR:   "|",
	LOR:  "||",

	EXP:    ")",
	LPAREN: "(",
	LBRACE: "{",

	RPAREN:     ")",
	RBRACE:     "}",
	SEMICOLON:  ";",
	DSEMICOLON: ";;",

	LSS: "<",
	GTR: ">",
	SHR: ">>",
}

func (t Token) String() string {
	if s, e := tokNames[t]; e {
		return s
	}
	return string(t)
}

func doToken(r rune, readOnly func(rune) bool) Token {
	switch r {
	case '&':
		if readOnly('&') {
			return LAND
		}
		return AND
	case '|':
		if readOnly('|') {
			return LOR
		}
		return OR
	case '(':
		return LPAREN
	case '{':
		return LBRACE
	case ')':
		return RPAREN
	case '}':
		return RBRACE
	case '$':
		return EXP
	case ';':
		if readOnly(';') {
			return DSEMICOLON
		}
		return SEMICOLON
	case '<':
		return LSS
	case '>':
		if readOnly('>') {
			return SHR
		}
		return GTR
	default:
		return Token(r)
	}
}
