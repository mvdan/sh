// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"strconv"
	"unicode"
)

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

	EXP     // $
	LPAREN  // (
	LBRACE  // {
	DLPAREN // ((

	RPAREN     // )
	RBRACE     // }
	DRPAREN    // ))
	SEMICOLON  // ;
	DSEMICOLON // ;;

	RDRIN  // <
	RDROUT // >
	APPEND // >>
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

	EXP:     ")",
	LPAREN:  "(",
	LBRACE:  "{",
	DLPAREN: "((",

	RPAREN:     ")",
	RBRACE:     "}",
	DRPAREN:    "))",
	SEMICOLON:  ";",
	DSEMICOLON: ";;",

	RDRIN:  "<",
	RDROUT: ">",
	APPEND: ">>",
}

func (t Token) String() string {
	if s, e := tokNames[t]; e {
		return s
	}
	r := rune(t)
	if !unicode.IsPrint(r) {
		return strconv.QuoteRune(r)
	}
	return string(r)
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
		if readOnly('(') {
			return DLPAREN
		}
		return LPAREN
	case '{':
		return LBRACE
	case ')':
		if readOnly(')') {
			return DRPAREN
		}
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
		return RDRIN
	case '>':
		if readOnly('>') {
			return APPEND
		}
		return RDROUT
	default:
		return Token(r)
	}
}
