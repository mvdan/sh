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

	Lss // <
	Gtr // >
	Shl // <<
	Shr // >>

	Add   // +
	Sub   // -
	Rem   // %
	Mul   // *
	Quo   // /
	Xor   // ^
	Not   // !
	Inc   // ++
	Dec   // --
	Pow   // **
	Comma // ,
	Assgn // =
	Eql   // ==
	Neq   // !=
	Leq   // <=
	Geq   // >=

	AddAssgn // +=
	SubAssgn // -=
	MulAssgn // *=
	QuoAssgn // /=
	RemAssgn // %=
	AndAssgn // &=
	OrAssgn  // |=
	XorAssgn // ^=
	ShlAssgn // <<=
	ShrAssgn // >>=

	PipeAll  // |& - bash
	RdrInOut // <>
	DplIn    // <&
	DplOut   // >&
	ClbOut   // >|
	DashHdoc // <<-
	WordHdoc // <<< - bash
	CmdIn    // <( - bash
	CmdOut   // >( - bash
	RdrAll   // &> - bash
	AppAll   // &>> - bash

	Colon    // :
	ColAdd   // :+
	ColSub   // :-
	Quest    // ?
	ColQuest // :?
	ColAssgn // :=
	DblRem   // %%
	Hash     // #
	DblHash  // ##
	DblQuo   // //
	DblXor   // ^^ - bash
	DblComma // ,, - bash

	// All of the below are bash-only.
	TsExists  // -e
	TsRegFile // -f
	TsDirect  // -d
	TsCharSp  // -c
	TsBlckSp  // -b
	TsNmPipe  // -p
	TsSocket  // -S
	TsSmbLink // -L
	TsGIDSet  // -g
	TsUIDSet  // -u
	TsRead    // -r
	TsWrite   // -w
	TsExec    // -x
	TsNoEmpty // -s
	TsFdTerm  // -t
	TsEmpStr  // -z
	TsNempStr // -n
	TsOptSet  // -o
	TsVarSet  // -v
	TsRefVar  // -R

	TsReMatch // =~
	TsNewer   // -nt
	TsOlder   // -ot
	TsDevIno  // -ef
	TsEql     // -eq
	TsNeq     // -ne
	TsLeq     // -le
	TsGeq     // -ge
	TsLss     // -lt
	TsGtr     // -gt

	GlobQuest // ?(
	GlobMul   // *(
	GlobAdd   // +(
	GlobAt    // @(
	GlobNot   // !(
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

	Lss:      "<",
	Gtr:      ">",
	Shl:      "<<",
	Shr:      ">>",
	PipeAll:  "|&",
	RdrInOut: "<>",
	DplIn:    "<&",
	DplOut:   ">&",
	ClbOut:   ">|",
	DashHdoc: "<<-",
	WordHdoc: "<<<",
	CmdIn:    "<(",
	CmdOut:   ">(",
	RdrAll:   "&>",
	AppAll:   "&>>",

	Colon:    ":",
	Add:      "+",
	ColAdd:   ":+",
	Sub:      "-",
	ColSub:   ":-",
	Quest:    "?",
	ColQuest: ":?",
	ColAssgn: ":=",
	Rem:      "%",
	DblRem:   "%%",
	Hash:     "#",
	DblHash:  "##",
	Quo:      "/",
	DblQuo:   "//",
	DblXor:   "^^",
	DblComma: ",,",

	Mul:   "*",
	Xor:   "^",
	Not:   "!",
	Inc:   "++",
	Dec:   "--",
	Pow:   "**",
	Comma: ",",
	Assgn: "=",
	Eql:   "==",
	Neq:   "!=",
	Leq:   "<=",
	Geq:   ">=",

	AddAssgn: "+=",
	SubAssgn: "-=",
	MulAssgn: "*=",
	QuoAssgn: "/=",
	RemAssgn: "%=",
	AndAssgn: "&=",
	OrAssgn:  "|=",
	XorAssgn: "^=",
	ShlAssgn: "<<=",
	ShrAssgn: ">>=",

	TsExists:  "-e",
	TsRegFile: "-f",
	TsDirect:  "-d",
	TsCharSp:  "-c",
	TsBlckSp:  "-b",
	TsNmPipe:  "-p",
	TsSocket:  "-S",
	TsSmbLink: "-L",
	TsGIDSet:  "-g",
	TsUIDSet:  "-u",
	TsRead:    "-r",
	TsWrite:   "-w",
	TsExec:    "-x",
	TsNoEmpty: "-s",
	TsFdTerm:  "-t",
	TsEmpStr:  "-z",
	TsNempStr: "-n",
	TsOptSet:  "-o",
	TsVarSet:  "-v",
	TsRefVar:  "-R",

	TsReMatch: "=~",
	TsNewer:   "-nt",
	TsOlder:   "-ot",
	TsDevIno:  "-ef",
	TsEql:     "-eq",
	TsNeq:     "-ne",
	TsLeq:     "-le",
	TsGeq:     "-ge",
	TsLss:     "-lt",
	TsGtr:     "-gt",

	GlobQuest: "?(",
	GlobMul:   "*(",
	GlobAdd:   "+(",
	GlobAt:    "@(",
	GlobNot:   "!(",
}

func (t Token) String() string { return tokNames[t] }
