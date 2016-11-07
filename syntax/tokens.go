// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

type token int

//go:generate stringer -type token

// The list of all possible tokens and reserved words.
const (
	illegalTok token = iota
	_EOF
	_Lit
	_LitWord

	sglQuote   // '
	dblSlashte // "
	bckQuote   // `

	and     // &
	andAnd  // &&
	orOr    // ||
	or      // |
	pipeAll // |&

	dollar       // $
	dollSglQuote // $'
	dollDblQuote // $"
	dollBrace    // ${
	dollBrack    // $[
	dollParen    // $(
	dollDblParen // $((
	leftBrack    // [
	leftParen    // (
	dblLeftParen // ((

	rightBrace    // }
	rightBrack    // ]
	rightParen    // )
	dblRightParen // ))
	semicolon     // ;

	dblSemicolon // ;;
	semiFall     // ;&
	dblSemiFall  // ;;&

	exclMark // !
	addAdd   // ++
	subSub   // --
	star     // *
	power    // **
	equal    // ==
	nequal   // !=
	lequal   // <=
	gequal   // >=

	addAssgn // +=
	subAssgn // -=
	mulAssgn // *=
	quoAssgn // /=
	remAssgn // %=
	andAssgn // &=
	orAssgn  // |=
	xorAssgn // ^=
	shlAssgn // <<=
	shrAssgn // >>=

	rdrOut   // >
	appOut   // >>
	rdrIn    // <
	rdrInOut // <>
	dplIn    // <&
	dplOut   // >&
	clbOut   // >|
	hdoc     // <<
	dashHdoc // <<-
	wordHdoc // <<<
	rdrAll   // &>
	appAll   // &>>

	cmdIn  // <(
	cmdOut // >(

	plus     // +
	colPlus  // :+
	minus    // -
	colMinus // :-
	quest    // ?
	colQuest // :?
	assgn    // =
	colAssgn // :=
	perc     // %
	dblPerc  // %%
	hash     // #
	dblHash  // ##
	caret    // ^
	dblCaret // ^^
	comma    // ,
	dblComma // ,,
	slash    // /
	dblSlash // //
	colon    // :

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
	globStar  // *(
	globPlus  // +(
	globAt    // @(
	globExcl  // !(
)

type RedirOperator token

const (
	RdrOut = RedirOperator(rdrOut) + iota
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

type ProcOperator token

const (
	CmdIn = ProcOperator(cmdIn) + iota
	CmdOut
)

type GlobOperator token

const (
	GlobQuest = GlobOperator(globQuest) + iota
	GlobStar
	GlobPlus
	GlobAt
	GlobExcl
)

type BinCmdOperator token

const (
	AndStmt = BinCmdOperator(andAnd) + iota
	OrStmt
	Pipe
	PipeAll
)

type CaseOperator token

const (
	DblSemicolon = CaseOperator(dblSemicolon) + iota
	SemiFall
	DblSemiFall
)

type ParExpOperator token

const (
	SubstPlus = ParExpOperator(plus) + iota
	SubstColPlus
	SubstMinus
	SubstColMinus
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

type UnAritOperator token

const (
	Not = UnAritOperator(exclMark) + iota
	Inc
	Dec
	Plus  = UnAritOperator(plus)
	Minus = UnAritOperator(minus)
)

type BinAritOperator token

const (
	Add = BinAritOperator(plus)
	Sub = BinAritOperator(minus)
	Mul = BinAritOperator(star)
	Quo = BinAritOperator(slash)
	Rem = BinAritOperator(perc)
	Pow = BinAritOperator(power)
	Eql = BinAritOperator(equal)
	Gtr = BinAritOperator(rdrOut)
	Lss = BinAritOperator(rdrIn)
	Neq = BinAritOperator(nequal)
	Leq = BinAritOperator(lequal)
	Geq = BinAritOperator(gequal)
	And = BinAritOperator(and)
	Or  = BinAritOperator(or)
	Xor = BinAritOperator(caret)
	Shr = BinAritOperator(appOut)
	Shl = BinAritOperator(hdoc)

	AndArit = BinAritOperator(andAnd)
	OrArit  = BinAritOperator(orOr)
	Comma   = BinAritOperator(comma)
	Quest   = BinAritOperator(quest)
	Colon   = BinAritOperator(colon)

	Assgn    = BinAritOperator(assgn)
	AddAssgn = BinAritOperator(addAssgn)
	SubAssgn = BinAritOperator(subAssgn)
	MulAssgn = BinAritOperator(mulAssgn)
	QuoAssgn = BinAritOperator(quoAssgn)
	RemAssgn = BinAritOperator(remAssgn)
	AndAssgn = BinAritOperator(andAssgn)
	OrAssgn  = BinAritOperator(orAssgn)
	XorAssgn = BinAritOperator(xorAssgn)
	ShlAssgn = BinAritOperator(shlAssgn)
	ShrAssgn = BinAritOperator(shrAssgn)
)

type UnTestOperator token

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

type BinTestOperator token

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
	AndTest  = BinTestOperator(andAnd)
	OrTest   = BinTestOperator(orOr)
	TsAssgn  = BinTestOperator(assgn)
	TsEqual  = BinTestOperator(equal)
	TsNequal = BinTestOperator(nequal)
	TsBefore = BinTestOperator(rdrIn)
	TsAfter  = BinTestOperator(rdrOut)
)

func (o RedirOperator) String() string   { return token(o).String() }
func (o ProcOperator) String() string    { return token(o).String() }
func (o GlobOperator) String() string    { return token(o).String() }
func (o BinCmdOperator) String() string  { return token(o).String() }
func (o CaseOperator) String() string    { return token(o).String() }
func (o ParExpOperator) String() string  { return token(o).String() }
func (o UnAritOperator) String() string  { return token(o).String() }
func (o BinAritOperator) String() string { return token(o).String() }
func (o UnTestOperator) String() string  { return token(o).String() }
func (o BinTestOperator) String() string { return token(o).String() }

// Pos is the internal representation of a position within a source
// file.
type Pos int

const maxPos = Pos(^uint(0) >> 1)

// Position describes a position within a source file including the line
// and column location. A Position is valid if the line number is > 0.
type Position struct {
	Offset int // byte offset, starting at 0
	Line   int // line number, starting at 1
	Column int // column number, starting at 1 (in bytes)
}
