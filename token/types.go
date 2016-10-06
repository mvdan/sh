// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package token

// Token is the set of lexical tokens.
type Token int

// The list of all possible tokens and reserved words.
const (
	ILLEGAL Token = iota
	STOPPED
	EOF
	LIT
	LITWORD

	// Rest of tokens
	SQUOTE // '
	DQUOTE // "
	BQUOTE // `

	AND  // &
	LAND // &&
	OR   // |
	LOR  // ||

	ASSIGN // =
	DOLLAR // $
	DOLLSQ // $'
	DOLLDQ // $"
	DOLLBR // ${
	DOLLBK // $[
	DOLLPR // $(
	DOLLDP // $((
	DLBRCK // [[
	LET    // let
	LBRACE // {
	LPAREN // (

	RBRACE     // }
	RBRACK     // ]
	RPAREN     // )
	DRBRCK     // ]]
	SEMICOLON  // ;
	DSEMICOLON // ;;
	SEMIFALL   // ;&
	DSEMIFALL  // ;;&
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

	PIPEALL  // |&
	RDRINOUT // <>
	DPLIN    // <&
	DPLOUT   // >&
	DHEREDOC // <<-
	WHEREDOC // <<<
	CMDIN    // <(
	CMDOUT   // >(
	RDRALL   // &>
	APPALL   // &>>

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

	DLPAREN // ((
	DRPAREN // ))

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
)

// Pos is the internal representation of a position within a source
// file.
type Pos int

// Position describes a position within a source file including the line
// and column location. A Position is valid if the line number is > 0.
type Position struct {
	Offset int // offset, starting at 0
	Line   int // line number, starting at 1
	Column int // column number, starting at 1 (byte count)
}

var (
	tokNames = map[Token]string{
		ILLEGAL: `ILLEGAL`,
		STOPPED: `STOPPED`,
		EOF:     `EOF`,
		LIT:     `lit`,
		LITWORD: `litword`,

		DLPAREN: "((",
		DRPAREN: "))",

		SQUOTE: `'`,
		DQUOTE: `"`,
		BQUOTE: "`",

		AND:  "&",
		LAND: "&&",
		OR:   "|",
		LOR:  "||",

		DOLLAR:     "$",
		DOLLSQ:     "$'",
		DOLLDQ:     `$"`,
		DOLLBR:     `${`,
		DOLLBK:     `$[`,
		DOLLPR:     `$(`,
		DOLLDP:     `$((`,
		DLBRCK:     "[[",
		LET:        "let",
		LBRACE:     "{",
		LPAREN:     "(",
		RBRACE:     "}",
		RBRACK:     "]",
		RPAREN:     ")",
		DRBRCK:     "]]",
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
	}
)

func (t Token) String() string { return tokNames[t] }
