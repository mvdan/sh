// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

type token int

const (
	ILLEGAL token = -iota
	EOF
	COMMENT
	WORD

	IF
	THEN
	ELIF
	ELSE
	FI
	WHILE
	DO
	DONE

	AND  // &
	LAND // &&
	OR   // |
	LOR  // ||

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

var tokNames = map[token]string{
	ILLEGAL: `ILLEGAL`,
	EOF:     `EOF`,
	COMMENT: `comment`,
	WORD:    `word`,

	IF:    "if",
	THEN:  "then",
	ELIF:  "elif",
	ELSE:  "else",
	FI:    "fi",
	WHILE: "while",
	DO:    "do",
	DONE:  "done",

	AND:  "&",
	LAND: "&&",
	OR:   "|",
	LOR:  "||",

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

func (t token) String() string {
	if s, e := tokNames[t]; e {
		return s
	}
	return string(t)
}
