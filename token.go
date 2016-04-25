// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

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
	return string(rune(t))
}

func doToken(r rune, readOnly func(byte) bool) Token {
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
		return Token(r)
	}
}
