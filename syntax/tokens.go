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

	And     // &
	AndExpr // &&
	OrExpr  // ||
	Or      // |
	pipeAll // |& - bash

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

	dblSemicolon // ;;
	semiFall     // ;& - bash
	dblSemiFall  // ;;& - bash

	Mul // *
	Not // !
	Inc // ++
	Dec // --
	Pow // **
	Eql // ==
	Neq // !=
	Leq // <=
	Geq // >=

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

	Gtr      // >
	Shr      // >>
	Lss      // <
	rdrInOut // <>
	dplIn    // <&
	dplOut   // >&
	clbOut   // >|
	Shl      // <<
	dashHdoc // <<-
	wordHdoc // <<< - bash
	rdrAll   // &> - bash
	appAll   // &>> - bash

	cmdIn  // <( - bash
	cmdOut // >( - bash

	Add      // +
	ColAdd   // :+
	Sub      // -
	ColSub   // :-
	Quest    // ?
	ColQuest // :?
	Assgn    // =
	ColAssgn // :=
	Rem      // %
	dblRem   // %%
	Hash     // #
	dblHash  // ##
	Xor      // ^
	dblXor   // ^^ - bash
	Comma    // ,
	dblComma // ,, - bash
	Quo      // /
	dblQuo   // //
	Colon    // :

	tsNot     // !
	tsExists  // -e
	tsRegFile // -f
	tsDirect  // -d
	tsCharSp  // -c
	tsBlckSp  // -b
	tsNmPipe  // -p
	tsSocket  // -S
	tsSmbLink // -L
	tsGIDSet  // -g
	tsUIDSet  // -u
	tsRead    // -r
	tsWrite   // -w
	tsExec    // -x
	tsNoEmpty // -s
	tsFdTerm  // -t
	tsEmpStr  // -z
	tsNempStr // -n
	tsOptSet  // -o
	tsVarSet  // -v
	tsRefVar  // -R

	tsReMatch // =~
	tsNewer   // -nt
	tsOlder   // -ot
	tsDevIno  // -ef
	tsEql     // -eq
	tsNeq     // -ne
	tsLeq     // -le
	tsGeq     // -ge
	tsLss     // -lt
	tsGtr     // -gt

	globQuest // ?(
	globMul   // *(
	globAdd   // +(
	globAt    // @(
	globNot   // !(
)

type RedirOperator Token

const (
	RdrOut = RedirOperator(Gtr) + iota
	AppOut
	RdrIn
	RdrInOut
	DplIn
	DplOut
	ClbOut
	Hdoc
	DashHdoc
	WordHdoc
	RdrAll
	AppAll
)

type ProcOperator Token

const (
	CmdIn = ProcOperator(cmdIn) + iota
	CmdOut
)

type GlobOperator Token

const (
	GlobQuest = GlobOperator(globQuest) + iota
	GlobMul
	GlobAdd
	GlobAt
	GlobNot
)

type BinCmdOperator Token

const (
	AndStmt = BinCmdOperator(AndExpr) + iota
	OrStmt
	Pipe
	PipeAll
)

type CaseOperator Token

const (
	DblSemicolon = CaseOperator(dblSemicolon) + iota
	SemiFall
	DblSemiFall
)

type ParExpOperator Token

const (
	SubstAdd = ParExpOperator(Add) + iota
	SubstColAdd
	SubstSub
	SubstColSub
	SubstQuest
	SubstColQuest
	SubstAssgn
	SubstColAssgn
	RemSmallSuffix
	RemLargeSuffix
	RemSmallPrefix
	RemLargePrefix
	UpperFirst
	UpperAll
	LowerFirst
	LowerAll
)

type UnTestOperator Token

const (
	TsNot = UnTestOperator(tsNot) + iota
	TsExists
	TsRegFile
	TsDirect
	TsCharSp
	TsBlckSp
	TsNmPipe
	TsSocket
	TsSmbLink
	TsGIDSet
	TsUIDSet
	TsRead
	TsWrite
	TsExec
	TsNoEmpty
	TsFdTerm
	TsEmpStr
	TsNempStr
	TsOptSet
	TsVarSet
	TsRefVar
)

type BinTestOperator Token

const (
	TsReMatch = BinTestOperator(tsReMatch) + iota
	TsNewer
	TsOlder
	TsDevIno
	TsEql
	TsNeq
	TsLeq
	TsGeq
	TsLss
	TsGtr
	AndTest  = BinTestOperator(AndExpr)
	OrTest   = BinTestOperator(OrExpr)
	TsAssgn  = BinTestOperator(Assgn)
	TsEqual  = BinTestOperator(Eql)
	TsNequal = BinTestOperator(Neq)
	TsBefore = BinTestOperator(Lss)
	TsAfter  = BinTestOperator(Gtr)
)

func (o RedirOperator) String() string   { return Token(o).String() }
func (o ProcOperator) String() string    { return Token(o).String() }
func (o GlobOperator) String() string    { return Token(o).String() }
func (o BinCmdOperator) String() string  { return Token(o).String() }
func (o CaseOperator) String() string    { return Token(o).String() }
func (o ParExpOperator) String() string  { return Token(o).String() }
func (o UnTestOperator) String() string  { return Token(o).String() }
func (o BinTestOperator) String() string { return Token(o).String() }

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

	And:     "&",
	AndExpr: "&&",
	OrExpr:  "||",
	Or:      "|",
	pipeAll: "|&",

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

	dblSemicolon: ";;",
	semiFall:     ";&",
	dblSemiFall:  ";;&",

	Gtr:      ">",
	Shr:      ">>",
	Lss:      "<",
	rdrInOut: "<>",
	dplIn:    "<&",
	dplOut:   ">&",
	clbOut:   ">|",
	Shl:      "<<",
	dashHdoc: "<<-",
	wordHdoc: "<<<",
	rdrAll:   "&>",
	appAll:   "&>>",

	cmdIn:  "<(",
	cmdOut: ">(",

	Add:      "+",
	ColAdd:   ":+",
	Sub:      "-",
	ColSub:   ":-",
	Quest:    "?",
	ColQuest: ":?",
	Assgn:    "=",
	ColAssgn: ":=",
	Rem:      "%",
	dblRem:   "%%",
	Hash:     "#",
	dblHash:  "##",
	Xor:      "^",
	dblXor:   "^^",
	Comma:    ",",
	dblComma: ",,",
	Quo:      "/",
	dblQuo:   "//",
	Colon:    ":",

	Mul: "*",
	Not: "!",
	Inc: "++",
	Dec: "--",
	Pow: "**",
	Eql: "==",
	Neq: "!=",
	Leq: "<=",
	Geq: ">=",

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

	tsNot:     "!",
	tsExists:  "-e",
	tsRegFile: "-f",
	tsDirect:  "-d",
	tsCharSp:  "-c",
	tsBlckSp:  "-b",
	tsNmPipe:  "-p",
	tsSocket:  "-S",
	tsSmbLink: "-L",
	tsGIDSet:  "-g",
	tsUIDSet:  "-u",
	tsRead:    "-r",
	tsWrite:   "-w",
	tsExec:    "-x",
	tsNoEmpty: "-s",
	tsFdTerm:  "-t",
	tsEmpStr:  "-z",
	tsNempStr: "-n",
	tsOptSet:  "-o",
	tsVarSet:  "-v",
	tsRefVar:  "-R",

	tsReMatch: "=~",
	tsNewer:   "-nt",
	tsOlder:   "-ot",
	tsDevIno:  "-ef",
	tsEql:     "-eq",
	tsNeq:     "-ne",
	tsLeq:     "-le",
	tsGeq:     "-ge",
	tsLss:     "-lt",
	tsGtr:     "-gt",

	globQuest: "?(",
	globMul:   "*(",
	globAdd:   "+(",
	globAt:    "@(",
	globNot:   "!(",
}

func (t Token) String() string { return tokNames[t] }
