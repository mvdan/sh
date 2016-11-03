// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import "fmt"

// Visitor holds a Visit method which is invoked for each node
// encountered by Walk. If the result visitor w is not nil, Walk visits
// each of the children of node with the visitor w, followed by a call
// of w.Visit(nil).
type Visitor interface {
	Visit(node Node) (w Visitor)
}

func walkStmts(v Visitor, stmts []*Stmt) {
	for _, s := range stmts {
		Walk(v, s)
	}
}

func walkWords(v Visitor, words []Word) {
	for i := range words {
		Walk(v, &words[i])
	}
}

// Walk traverses an AST in depth-first order: It starts by calling
// v.Visit(node); node must not be nil. If the visitor w returned by
// v.Visit(node) is not nil, Walk is invoked recursively with visitor w
// for each of the non-nil children of node, followed by a call of
// w.Visit(nil).
func Walk(v Visitor, node Node) {
	if v = v.Visit(node); v == nil {
		return
	}

	switch x := node.(type) {
	case *File:
		walkStmts(v, x.Stmts)
	case *Stmt:
		if x.Cmd != nil {
			Walk(v, x.Cmd)
		}
		for _, a := range x.Assigns {
			Walk(v, a)
		}
		for _, r := range x.Redirs {
			Walk(v, r)
		}
	case *Assign:
		if x.Name != nil {
			Walk(v, x.Name)
		}
		Walk(v, &x.Value)
	case *Redirect:
		if x.N != nil {
			Walk(v, x.N)
		}
		Walk(v, &x.Word)
		if len(x.Hdoc.Parts) > 0 {
			Walk(v, &x.Hdoc)
		}
	case *CallExpr:
		walkWords(v, x.Args)
	case *Subshell:
		walkStmts(v, x.Stmts)
	case *Block:
		walkStmts(v, x.Stmts)
	case *IfClause:
		walkStmts(v, x.CondStmts)
		walkStmts(v, x.ThenStmts)
		for _, elif := range x.Elifs {
			walkStmts(v, elif.CondStmts)
			walkStmts(v, elif.ThenStmts)
		}
		walkStmts(v, x.ElseStmts)
	case *WhileClause:
		walkStmts(v, x.CondStmts)
		walkStmts(v, x.DoStmts)
	case *UntilClause:
		walkStmts(v, x.CondStmts)
		walkStmts(v, x.DoStmts)
	case *ForClause:
		Walk(v, x.Loop)
		walkStmts(v, x.DoStmts)
	case *WordIter:
		Walk(v, &x.Name)
		walkWords(v, x.List)
	case *CStyleLoop:
		if x.Init != nil {
			Walk(v, x.Init)
		}
		if x.Cond != nil {
			Walk(v, x.Cond)
		}
		if x.Post != nil {
			Walk(v, x.Post)
		}
	case *BinaryCmd:
		Walk(v, x.X)
		Walk(v, x.Y)
	case *FuncDecl:
		Walk(v, &x.Name)
		Walk(v, x.Body)
	case *Word:
		for _, wp := range x.Parts {
			Walk(v, wp)
		}
	case *Lit:
	case *SglQuoted:
	case *DblQuoted:
		for _, wp := range x.Parts {
			Walk(v, wp)
		}
	case *CmdSubst:
		walkStmts(v, x.Stmts)
	case *ParamExp:
		Walk(v, &x.Param)
		if x.Ind != nil {
			Walk(v, &x.Ind.Word)
		}
		if x.Repl != nil {
			Walk(v, &x.Repl.Orig)
			Walk(v, &x.Repl.With)
		}
		if x.Exp != nil {
			Walk(v, &x.Exp.Word)
		}
	case *ArithmExp:
		if x.X != nil {
			Walk(v, x.X)
		}
	case *ArithmCmd:
		if x.X != nil {
			Walk(v, x.X)
		}
	case *BinaryArithm:
		Walk(v, x.X)
		Walk(v, x.Y)
	case *BinaryTest:
		Walk(v, x.X)
		Walk(v, x.Y)
	case *UnaryArithm:
		Walk(v, x.X)
	case *UnaryTest:
		Walk(v, x.X)
	case *ParenArithm:
		Walk(v, x.X)
	case *ParenTest:
		Walk(v, x.X)
	case *CaseClause:
		Walk(v, &x.Word)
		for _, pl := range x.List {
			walkWords(v, pl.Patterns)
			walkStmts(v, pl.Stmts)
		}
	case *TestClause:
		Walk(v, x.X)
	case *DeclClause:
		walkWords(v, x.Opts)
		for _, a := range x.Assigns {
			Walk(v, a)
		}
	case *ArrayExpr:
		walkWords(v, x.List)
	case *ExtGlob:
		Walk(v, &x.Pattern)
	case *ProcSubst:
		walkStmts(v, x.Stmts)
	case *EvalClause:
		if x.Stmt != nil {
			Walk(v, x.Stmt)
		}
	case *CoprocClause:
		if x.Name != nil {
			Walk(v, x.Name)
		}
		Walk(v, x.Stmt)
	case *LetClause:
		for _, expr := range x.Exprs {
			Walk(v, expr)
		}
	default:
		panic(fmt.Sprintf("ast.Walk: unexpected node type %T", x))
	}

	v.Visit(nil)
}
