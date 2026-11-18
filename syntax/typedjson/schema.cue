// Copyright (c) 2026, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

// Package typedjson describes the typed JSON format of the Go package
// mvdan.cc/sh/v3/syntax/typedjson, and can be imported in CUE as
// "github.com/mvdan/sh/syntax/typedjson@v3".
// Each definition matches the syntax type of the same name,
// documented at https://pkg.go.dev/mvdan.cc/sh/v3/syntax.
//
// Fields holding zero values are omitted when encoding.
// The Type field is required where a node's type cannot be inferred,
// and the derived Pos and End fields are ignored when decoding.
package typedjson

// Node is any syntax node, such as the root node.
#Node: {Type!: _} & (#File | #Comment | #Stmt | #Assign | #Redirect | #Word |
	#Lit | #SglQuoted | #DblQuoted | #ParamExp | #CmdSubst | #CallExpr |
	#ArithmExp | #ProcSubst | #ExtGlob | #BraceExp |
	#ArithmCmd | #BinaryCmd | #IfClause | #ForClause | #RepeatClause |
	#WhileClause | #CaseClause | #Block | #TryClause | #Subshell | #FuncDecl |
	#TestClause | #DeclClause | #LetClause | #TimeClause | #CoprocClause |
	#TestDecl |
	#UnaryArithm | #BinaryArithm | #ParenArithm | #FlagsArithm | #CallArithm |
	#UnaryTest | #BinaryTest | #ParenTest |
	#WordIter | #CStyleLoop | #CaseItem | #ArrayExpr | #ArrayElem)

#Command: {Type!: _} & (#CallExpr | #IfClause | #WhileClause | #ForClause |
	#RepeatClause | #CaseClause | #Block | #TryClause | #Subshell | #BinaryCmd |
	#FuncDecl | #ArithmCmd | #TestClause | #DeclClause | #LetClause |
	#TimeClause | #CoprocClause | #TestDecl)

#Loop: {Type!: _} & (#WordIter | #CStyleLoop)

#WordPart: {Type!: _} & (#Lit | #SglQuoted | #DblQuoted | #ParamExp |
	#CmdSubst | #ArithmExp | #ProcSubst | #ExtGlob | #BraceExp)

#ArithmExpr: {Type!: _} & (#BinaryArithm | #UnaryArithm | #ParenArithm |
	#FlagsArithm | #CallArithm | #Word)

#TestExpr: {Type!: _} & (#BinaryTest | #UnaryTest | #ParenTest | #Word)

// Pos is a position in the source. Offset counts bytes from 0,
// while Line and Col count from 1, or are 0 when too large to track.
#Pos: {
	Offset!: uint32
	Line!:   uint32
	Col!:    uint32
}

// _node holds the derived fields of every node.
_node: {
	Pos?: #Pos
	End?: #Pos
}

// OptState is either unset, on, or off.
#OptState: 0 | 1 | 2

#File: {
	_node
	Type?:  "File"
	Name?:  string
	Stmts?: [...#Stmt]
	Last?:  [...#Comment]
}

#Comment: {
	_node
	Type?: "Comment"
	Hash?: #Pos
	Text?: string
}

#Stmt: {
	_node
	Type?:       "Stmt"
	Comments?:   [...#Comment]
	Cmd?:        #Command
	Position?:   #Pos
	Semicolon?:  #Pos
	Negated?:    bool
	Background?: bool
	Coprocess?:  bool
	Disown?:     bool
	Redirs?:     [...#Redirect]
}

#Assign: {
	_node
	Type?:   "Assign"
	Append?: bool
	Naked?:  bool
	Name?:   #Lit
	Index?:  #ArithmExpr
	Value?:  #Word
	Array?:  #ArrayExpr
}

#Redirect: {
	_node
	Type?:     "Redirect"
	OpPos?:    #Pos
	ClosePos?: #Pos
	Op?:       #RedirOperator
	N?:        #Lit
	Word?:     #Word
	Hdoc?:     #Word
}

#CallExpr: {
	_node
	Type?:    "CallExpr"
	Assigns?: [...#Assign]
	Args?:    [...#Word]
}

#Subshell: {
	_node
	Type?:   "Subshell"
	Lparen?: #Pos
	Rparen?: #Pos
	Stmts?:  [...#Stmt]
	Last?:   [...#Comment]
}

#Block: {
	_node
	Type?:   "Block"
	Lbrace?: #Pos
	Rbrace?: #Pos
	Stmts?:  [...#Stmt]
	Last?:   [...#Comment]
}

#TryClause: {
	_node
	Type?:      "TryClause"
	Try?:       #Block
	AlwaysPos?: #Pos
	Always?:    #Block
}

#IfClause: {
	_node
	Type?:     "IfClause"
	Position?: #Pos
	ThenPos?:  #Pos
	FiPos?:    #Pos
	Cond?:     [...#Stmt]
	CondLast?: [...#Comment]
	Then?:     [...#Stmt]
	ThenLast?: [...#Comment]
	Else?:     #IfClause
	Last?:     [...#Comment]
}

#WhileClause: {
	_node
	Type?:     "WhileClause"
	WhilePos?: #Pos
	DoPos?:    #Pos
	DonePos?:  #Pos
	Until?:    bool
	Braces?:   bool
	Short?:    bool
	Cond?:     [...#Stmt]
	CondLast?: [...#Comment]
	Do?:       [...#Stmt]
	DoLast?:   [...#Comment]
}

#ForClause: {
	_node
	Type?:    "ForClause"
	ForPos?:  #Pos
	DoPos?:   #Pos
	DonePos?: #Pos
	Select?:  bool
	Foreach?: bool
	Braces?:  bool
	Short?:   bool
	Loop?:    #Loop
	Do?:      [...#Stmt]
	DoLast?:  [...#Comment]
}

#RepeatClause: {
	_node
	Type?:      "RepeatClause"
	RepeatPos?: #Pos
	DoPos?:     #Pos
	DonePos?:   #Pos
	Braces?:    bool
	Short?:     bool
	Count?:     #Word
	Do?:        [...#Stmt]
	DoLast?:    [...#Comment]
}

#WordIter: {
	_node
	Type?:   "WordIter"
	Name?:   #Lit
	Names?:  [...#Lit]
	InPos?:  #Pos
	Lparen?: #Pos
	Rparen?: #Pos
	Items?:  [...#Word]
}

#CStyleLoop: {
	_node
	Type?:   "CStyleLoop"
	Lparen?: #Pos
	Rparen?: #Pos
	Init?:   #ArithmExpr
	Cond?:   #ArithmExpr
	Post?:   #ArithmExpr
}

#BinaryCmd: {
	_node
	Type?:  "BinaryCmd"
	OpPos?: #Pos
	Op?:    #BinCmdOperator
	X?:     #Stmt
	Y?:     #Stmt
}

#FuncDecl: {
	_node
	Type?:     "FuncDecl"
	Position?: #Pos
	RsrvWord?: bool
	Parens?:   bool
	Name?:     #Lit
	Names?:    [...#Lit]
	Body?:     #Stmt
	Args?:     [...#Word]
}

#Word: {
	_node
	Type?:  "Word"
	Parts?: [...#WordPart]
}

#Lit: {
	_node
	Type?:     "Lit"
	ValuePos?: #Pos
	ValueEnd?: #Pos
	Value?:    string
}

#SglQuoted: {
	_node
	Type?:   "SglQuoted"
	Left?:   #Pos
	Right?:  #Pos
	Dollar?: bool
	Value?:  string
}

#DblQuoted: {
	_node
	Type?:   "DblQuoted"
	Left?:   #Pos
	Right?:  #Pos
	Dollar?: bool
	Parts?:  [...#WordPart]
}

#CmdSubst: {
	_node
	Type?:       "CmdSubst"
	Left?:       #Pos
	Right?:      #Pos
	Stmts?:      [...#Stmt]
	Last?:       [...#Comment]
	Backquotes?: bool
	TempFile?:   bool
	ReplyVar?:   bool
}

#ParamExp: {
	_node
	Type?:        "ParamExp"
	Dollar?:      #Pos
	Rbrace?:      #Pos
	Short?:       bool
	Flags?:       #Lit
	Excl?:        bool
	Length?:      bool
	Width?:       bool
	IsSet?:       bool
	Split?:       #OptState
	GlobSubst?:   #OptState
	RcExpand?:    #OptState
	Param?:       #Lit
	NestedParam?: #WordPart
	Index?:       #ArithmExpr
	Subscripts?:  [...#ArithmExpr]
	Modifiers?:   [...#Lit]
	Slice?:       #Slice
	Repl?:        #Replace
	Names?:       #ParNamesOperator
	Exp?:         #Expansion
}

#Slice: {
	Offset?: #ArithmExpr
	Length?: #ArithmExpr
}

#Replace: {
	All?:  bool
	Orig?: #Word
	With?: #Word
}

#Expansion: {
	Op?:   #ParExpOperator
	Word?: #Word
}

#ArithmExp: {
	_node
	Type?:     "ArithmExp"
	Left?:     #Pos
	Right?:    #Pos
	Bracket?:  bool
	Unsigned?: bool
	X?:        #ArithmExpr
}

#ArithmCmd: {
	_node
	Type?:     "ArithmCmd"
	Left?:     #Pos
	Right?:    #Pos
	Unsigned?: bool
	X?:        #ArithmExpr
}

#BinaryArithm: {
	_node
	Type?:  "BinaryArithm"
	OpPos?: #Pos
	Op?:    #BinAritOperator
	X?:     #ArithmExpr
	Y?:     #ArithmExpr
}

#UnaryArithm: {
	_node
	Type?:  "UnaryArithm"
	OpPos?: #Pos
	Op?:    #UnAritOperator
	Post?:  bool
	X?:     #ArithmExpr
}

#ParenArithm: {
	_node
	Type?:   "ParenArithm"
	Lparen?: #Pos
	Rparen?: #Pos
	X?:      #ArithmExpr
}

#FlagsArithm: {
	_node
	Type?:  "FlagsArithm"
	Flags?: #Lit
	X?:     #ArithmExpr
}

#CallArithm: {
	_node
	Type?:   "CallArithm"
	Name?:   #Lit
	Lparen?: #Pos
	Rparen?: #Pos
	Args?:   [...#ArithmExpr]
}

#CaseClause: {
	_node
	Type?:   "CaseClause"
	Case?:   #Pos
	In?:     #Pos
	Esac?:   #Pos
	Braces?: bool
	Word?:   #Word
	Items?:  [...#CaseItem]
	Last?:   [...#Comment]
}

#CaseItem: {
	_node
	Type?:     "CaseItem"
	Op?:       #CaseOperator
	OpPos?:    #Pos
	Lparen?:   #Pos
	Comments?: [...#Comment]
	Patterns?: [...#Word]
	Stmts?:    [...#Stmt]
	Last?:     [...#Comment]
}

#TestClause: {
	_node
	Type?:  "TestClause"
	Left?:  #Pos
	Right?: #Pos
	X?:     #TestExpr
}

#BinaryTest: {
	_node
	Type?:  "BinaryTest"
	OpPos?: #Pos
	Op?:    #BinTestOperator
	X?:     #TestExpr
	Y?:     #TestExpr
}

#UnaryTest: {
	_node
	Type?:  "UnaryTest"
	OpPos?: #Pos
	Op?:    #UnTestOperator
	X?:     #TestExpr
}

#ParenTest: {
	_node
	Type?:   "ParenTest"
	Lparen?: #Pos
	Rparen?: #Pos
	X?:      #TestExpr
}

#DeclClause: {
	_node
	Type?:    "DeclClause"
	Variant?: #Lit
	Args?:    [...#Assign]
	Assigns?: [...#Assign]
}

#ArrayExpr: {
	_node
	Type?:   "ArrayExpr"
	Lparen?: #Pos
	Rparen?: #Pos
	Elems?:  [...#ArrayElem]
	Last?:   [...#Comment]
}

#ArrayElem: {
	_node
	Type?:     "ArrayElem"
	Index?:    #ArithmExpr
	Value?:    #Word
	Comments?: [...#Comment]
}

#ExtGlob: {
	_node
	Type?:    "ExtGlob"
	OpPos?:   #Pos
	Op?:      #GlobOperator
	Pattern?: #Lit
}

#ProcSubst: {
	_node
	Type?:   "ProcSubst"
	OpPos?:  #Pos
	Rparen?: #Pos
	Op?:     #ProcOperator
	Stmts?:  [...#Stmt]
	Last?:   [...#Comment]
}

#TimeClause: {
	_node
	Type?:        "TimeClause"
	Time?:        #Pos
	PosixFormat?: bool
	Stmt?:        #Stmt
}

#CoprocClause: {
	_node
	Type?:   "CoprocClause"
	Coproc?: #Pos
	Name?:   #Word
	Stmt?:   #Stmt
}

#LetClause: {
	_node
	Type?:  "LetClause"
	Let?:   #Pos
	Exprs?: [...#ArithmExpr]
}

#BraceExp: {
	_node
	Type?:     "BraceExp"
	Lbrace?:   #Pos
	Rbrace?:   #Pos
	Sequence?: bool
	Elems?:    [...#Word]
}

#TestDecl: {
	_node
	Type?:        "TestDecl"
	Position?:    #Pos
	Description?: #Word
	Body?:        #Stmt
}

#RedirOperator: ">" | ">>" | "<" | "<>" | "<&" | ">&" | ">|" | ">>|" | "<<" |
	"<<-" | "<<<" | "&>" | "&>|" | "&>>" | "&>>|"

#ProcOperator: "<(" | "=(" | ">("

#GlobOperator: "?(" | "*(" | "+(" | "@(" | "!("

#BinCmdOperator: "&&" | "||" | "|" | "|&"

#CaseOperator: ";;" | ";&" | ";;&" | ";|"

#ParNamesOperator: "*" | "@"

#ParExpOperator: "+" | ":+" | "-" | ":-" | "?" | ":?" | "=" | ":=" | "%" |
	"%%" | "#" | "##" | ":#" | ":|" | ":*" | "^" | "^^" | "," | ",," | "@"

#UnAritOperator: "!" | "~" | "++" | "--" | "+" | "-"

#BinAritOperator: "+" | "-" | "*" | "/" | "%" | "**" | "==" | ">" | "<" |
	"!=" | "<=" | ">=" | "&" | "|" | "^" | ">>" | "<<" | "&&" | "||" | "^^" |
	"," | "?" | ":" | "=" | "+=" | "-=" | "*=" | "/=" | "%=" | "&=" | "|=" |
	"^=" | "<<=" | ">>=" | "&&=" | "||=" | "^^=" | "**="

#UnTestOperator: "-e" | "-f" | "-d" | "-c" | "-b" | "-p" | "-S" | "-L" |
	"-k" | "-g" | "-u" | "-G" | "-O" | "-N" | "-r" | "-w" | "-x" | "-s" |
	"-t" | "-z" | "-n" | "-o" | "-v" | "-R" | "!" | "("

#BinTestOperator: "=~" | "-nt" | "-ot" | "-ef" | "-eq" | "-ne" | "-le" |
	"-ge" | "-lt" | "-gt" | "&&" | "||" | "=" | "==" | "!=" | "<" | ">"
