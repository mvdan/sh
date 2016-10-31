// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

// Token is the set of lexical tokens and reserved words.
type Token int

// The list of all possible tokens and reserved words.
const (
	illegalTok Token = iota
	_EOF
	_Lit
	_LitWord

	sglQuote // '
	dblQuote // "
	bckQuote // `

	And   // &
	AndIf // &&
	Or    // |
	OrIf  // ||

	dollar       // $
	dollSglQuote // $' - bash
	dollDblQuote // $" - bash
	dollBrace    // ${
	dollBrack    // $[
	dollParen    // $(
	dollDblParen // $((
	leftBrack    // [
	leftParen    // (
	dblLeftParen // (( - bash

	rightBrace    // }
	rightBrack    // ]
	rightParen    // )
	dblRightParen // ))
	semicolon     // ;

	DblSemicolon // ;;
	SemiFall     // ;& - bash
	DblSemiFall  // ;;& - bash

	LSS // <
	GTR // >
	SHL // <<
	SHR // >>

	ADD    // +
	SUB    // -
	REM    // %
	MUL    // *
	QUO    // /
	XOR    // ^
	NOT    // !
	INC    // ++
	DEC    // --
	POW    // **
	COMMA  // ,
	ASSIGN // =
	EQL    // ==
	NEQ    // !=
	LEQ    // <=
	GEQ    // >=

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

	COLON   // :
	CADD    // :+
	CSUB    // :-
	QUEST   // ?
	CQUEST  // :?
	CASSIGN // :=
	DREM    // %%
	HASH    // #
	DHASH   // ##
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

var defaultPos Pos

const maxPos = Pos(^uint(0) >> 1)

// Position describes a position within a source file including the line
// and column location. A Position is valid if the line number is > 0.
type Position struct {
	Offset int // byte offset, starting at 0
	Line   int // line number, starting at 1
	Column int // column number, starting at 1 (in bytes)
}

var tokNames = map[Token]string{
	illegalTok: "illegal",
	_EOF:       "EOF",
	_Lit:       "Lit",
	_LitWord:   "LitWord",

	sglQuote: "'",
	dblQuote: `"`,
	bckQuote: "`",

	And:   "&",
	AndIf: "&&",
	Or:    "|",
	OrIf:  "||",

	dollar:       "$",
	dollSglQuote: "$'",
	dollDblQuote: `$"`,
	dollBrace:    "${",
	dollBrack:    "$[",
	dollParen:    "$(",
	dollDblParen: "$((",
	leftBrack:    "[",
	leftParen:    "(",
	dblLeftParen: "((",

	rightBrace:    "}",
	rightBrack:    "]",
	rightParen:    ")",
	dblRightParen: "))",
	semicolon:     ";",

	DblSemicolon: ";;",
	SemiFall:     ";&",
	DblSemiFall:  ";;&",

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
	CASSIGN: ":=",
	REM:     "%",
	DREM:    "%%",
	HASH:    "#",
	DHASH:   "##",
	QUO:     "/",
	DQUO:    "//",
	DXOR:    "^^",
	DCOMMA:  ",,",

	MUL:    "*",
	XOR:    "^",
	NOT:    "!",
	INC:    "++",
	DEC:    "--",
	POW:    "**",
	COMMA:  ",",
	ASSIGN: "=",
	EQL:    "==",
	NEQ:    "!=",
	LEQ:    "<=",
	GEQ:    ">=",

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
