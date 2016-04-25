// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import "fmt"

type Token int

const (
	ILLEGAL Token = -iota
	EOF
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
	CASE
	ESAC

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

	RDRIN    // <
	RDROUT   // >
	OPRDWR   // <>
	DPLIN    // <&
	DPLOUT   // >&
	APPEND   // >>
	HEREDOC  // <<
	DHEREDOC // <<-
)

// Pos is the internal representation of a position within a source
// file.
// TODO: replace struct with a byte offset
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

var tokNames = map[Token]string{
	ILLEGAL: `ILLEGAL`,
	EOF:     `EOF`,
	LIT:     `literal`,

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
	CASE:  "case",
	ESAC:  "esac",

	AND:  "&",
	LAND: "&&",
	OR:   "|",
	LOR:  "||",

	EXP:     "$",
	LPAREN:  "(",
	LBRACE:  "{",
	DLPAREN: "((",

	RPAREN:     ")",
	RBRACE:     "}",
	DRPAREN:    "))",
	SEMICOLON:  ";",
	DSEMICOLON: ";;",

	RDRIN:    "<",
	RDROUT:   ">",
	OPRDWR:   "<>",
	DPLIN:    "<&",
	DPLOUT:   ">&",
	APPEND:   ">>",
	HEREDOC:  "<<",
	DHEREDOC: "<<-",
}

func (t Token) String() string {
	if s, e := tokNames[t]; e {
		return s
	}
	return string(t)
}

func doToken(b byte, readOnly func(byte) bool) Token {
	switch b {
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
		if readOnly('<') {
			if readOnly('-') {
				return DHEREDOC
			}
			return HEREDOC
		}
		if readOnly('&') {
			return DPLIN
		}
		if readOnly('>') {
			return OPRDWR
		}
		return RDRIN
	case '>':
		if readOnly('>') {
			return APPEND
		}
		if readOnly('&') {
			return DPLOUT
		}
		return RDROUT
	default:
		return Token(b)
	}
}
