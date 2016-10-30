// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

// Token is the set of lexical tokens and reserved words.
type Token int

// The list of all possible tokens and reserved words.
const (
	ILLEGAL Token = iota

	SQUOTE // '
	DQUOTE // "
	BQUOTE // `

	AND  // &
	LAND // &&
	OR   // |
	LOR  // ||

	ASSIGN  // =
	DOLLAR  // $
	DOLLSQ  // $' - bash
	DOLLDQ  // $" - bash
	DOLLBR  // ${
	DOLLBK  // $[
	DOLLPR  // $(
	DOLLDP  // $((
	DLBRCK  // [[
	LBRACE  // {
	LPAREN  // (
	DLPAREN // (( - bash

	RBRACE     // }
	RBRACK     // ]
	DRBRCK     // ]]
	RPAREN     // )
	DRPAREN    // ))
	SEMICOLON  // ;
	DSEMICOLON // ;;
	SEMIFALL   // ;& - bash
	DSEMIFALL  // ;;& - bash
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

	PIPEALL  // |& - bash
	RDRINOUT // <>
	DPLIN    // <&
	DPLOUT   // >&
	CLBOUT   // >|
	DHEREDOC // <<-
	WHEREDOC // <<< - bash
	CMDIN    // <( - bash
	CMDOUT   // >( - bash
	RDRALL   // &> - bash
	APPALL   // &>> - bash

	CADD    // :+
	CSUB    // :-
	QUEST   // ?
	CQUEST  // :?
	CASSIGN // :=
	DREM    // %%
	HASH    // #
	DHASH   // ##
	LBRACK  // [
	DQUO    // //
	DXOR    // ^^ - bash
	DCOMMA  // ,, - bash

	// All of the below are bash-only.
	TEXISTS  // -e
	TREGFILE // -f
	TDIRECT  // -d
	TCHARSP  // -c
	TBLCKSP  // -b
	TNMPIPE  // -p
	TSOCKET  // -S
	TSMBLINK // -L
	TSGIDSET // -g
	TSUIDSET // -u
	TREAD    // -r
	TWRITE   // -w
	TEXEC    // -x
	TNOEMPTY // -s
	TFDTERM  // -t
	TEMPSTR  // -z
	TNEMPSTR // -n
	TOPTSET  // -o
	TVARSET  // -v
	TNRFVAR  // -R

	TREMATCH // =~
	TNEWER   // -nt
	TOLDER   // -ot
	TDEVIND  // -ef
	TEQL     // -eq
	TNEQ     // -ne
	TLEQ     // -le
	TGEQ     // -ge
	TLSS     // -lt
	TGTR     // -gt

	GQUEST // ?(
	GMUL   // *(
	GADD   // +(
	GAT    // @(
	GNOT   // !(
)

// Pos is the internal representation of a position within a source
// file.
type Pos int

var DefaultPos Pos = 0

// Position describes a position within a source file including the line
// and column location. A Position is valid if the line number is > 0.
type Position struct {
	Offset int // byte offset, starting at 0
	Line   int // line number, starting at 1
	Column int // column number, starting at 1 (in bytes)
}

var tokNames = map[Token]string{
	ILLEGAL: "ILLEGAL",

	SQUOTE: "'",
	DQUOTE: `"`,
	BQUOTE: "`",

	AND:  "&",
	LAND: "&&",
	OR:   "|",
	LOR:  "||",

	DOLLAR:  "$",
	DOLLSQ:  "$'",
	DOLLDQ:  `$"`,
	DOLLBR:  "${",
	DOLLBK:  "$[",
	DOLLPR:  "$(",
	DOLLDP:  "$((",
	DLBRCK:  "[[",
	LBRACE:  "{",
	LPAREN:  "(",
	DLPAREN: "((",

	RBRACE:     "}",
	RBRACK:     "]",
	DRBRCK:     "]]",
	RPAREN:     ")",
	DRPAREN:    "))",
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
	CLBOUT:   ">|",
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
	QUO:     "/",
	DQUO:    "//",
	DXOR:    "^^",
	DCOMMA:  ",,",

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

	TEXISTS:  "-e",
	TREGFILE: "-f",
	TDIRECT:  "-d",
	TCHARSP:  "-c",
	TBLCKSP:  "-b",
	TNMPIPE:  "-p",
	TSOCKET:  "-S",
	TSMBLINK: "-L",
	TSGIDSET: "-g",
	TSUIDSET: "-u",
	TREAD:    "-r",
	TWRITE:   "-w",
	TEXEC:    "-x",
	TNOEMPTY: "-s",
	TFDTERM:  "-t",
	TEMPSTR:  "-z",
	TNEMPSTR: "-n",
	TOPTSET:  "-o",
	TVARSET:  "-v",
	TNRFVAR:  "-R",

	TREMATCH: "=~",
	TNEWER:   "-nt",
	TOLDER:   "-ot",
	TDEVIND:  "-ef",
	TEQL:     "-eq",
	TNEQ:     "-ne",
	TLEQ:     "-le",
	TGEQ:     "-ge",
	TLSS:     "-lt",
	TGTR:     "-gt",

	GQUEST: "?(",
	GMUL:   "*(",
	GADD:   "+(",
	GAT:    "@(",
	GNOT:   "!(",
}

func (t Token) String() string { return tokNames[t] }
