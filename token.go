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

	AND  // &
	LAND // &&
	OR   // |
	LOR  // ||

	BANG    // !
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
	}

	tokList = [...]struct {
		str string
		tok Token
	}{
		{"&", AND},
		{"&&", LAND},
		{"|", OR},
		{"||", LOR},

		{"!", BANG},
		{"$", EXP},
		{"(", LPAREN},
		{"{", LBRACE},
		{"((", DLPAREN},

		{")", RPAREN},
		{"}", RBRACE},
		{"))", DRPAREN},
		{";", SEMICOLON},
		{";;", DSEMICOLON},

		{"<", RDRIN},
		{">", RDROUT},
		{"<>", OPRDWR},
		{"<&", DPLIN},
		{">&", DPLOUT},
		{">>", APPEND},
		{"<<", HEREDOC},
		{"<<-", DHEREDOC},
	}
)

func (t Token) String() string {
	if s, e := tokNames[t]; e {
		return s
	}
	return string(t)
}

func doToken(readOnly func(string) bool, readByte func() (byte, error)) (Token, error) {
	// In reverse, to not treat e.g. && as & two times
	for i := len(tokList) - 1; i >= 0; i-- {
		t := tokList[i]
		if readOnly(t.str) {
			return t.tok, nil
		}
	}
	b, err := readByte()
	return Token(b), err
}
