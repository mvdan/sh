// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"fmt"
	"io"
)

func Fprint(w io.Writer, v interface{}) error {
	p := printer{w: w}
	p.node(v)
	return p.err
}

type printer struct {
	w   io.Writer
	err error

	contiguous  bool
	needNewline bool
}

var contiguousTokens = map[Token]bool{
	DOLLPR: true,
	LPAREN: true,
	BQUOTE: true,
	CMDIN:  true,
}

func (p *printer) pr(a ...interface{}) {
	for _, v := range a {
		if p.err != nil {
			break
		}
		switch x := v.(type) {
		case string:
			if len(x) > 0 {
				b := x[len(x)-1]
				p.contiguous = !space[b]
			}
			_, p.err = fmt.Fprint(p.w, x)
		case Token:
			p.contiguous = !contiguousTokens[x]
			_, p.err = fmt.Fprint(p.w, x)
		default:
			p.node(v)
		}
	}
}

func (p *printer) spaced(a ...interface{}) {
	if p.contiguous {
		p.pr(" ")
	}
	p.pr(a...)
}

func (p *printer) node(v interface{}) {
	switch x := v.(type) {
	case File:
		p.stmtJoin(x.Stmts)
	case Stmt:
		if x.Negated {
			p.spaced(NOT)
		}
		for _, a := range x.Assigns {
			p.spaced(a)
		}
		if x.Node != nil {
			p.spaced(x.Node)
		}
		for _, r := range x.Redirs {
			p.spaced(r.N, r.Op)
			p.pr(r.Word)
			p.needNewline = r.Op == SHL || r.Op == DHEREDOC
		}
		if x.Background {
			p.spaced(AND)
		}
	case Assign:
		if x.Name != nil {
			p.pr(x.Name)
			if x.Append {
				p.pr(ADD_ASSIGN)
			} else {
				p.pr(ASSIGN)
			}
		}
		p.pr(x.Value)
	case Command:
		p.wordJoin(x.Args, " ")
	case Subshell:
		p.pr(LPAREN)
		if len(x.Stmts) == 0 {
			// avoid conflict with ()
			p.pr(" ")
		}
		p.stmtJoin(x.Stmts)
		p.pr(RPAREN)
	case Block:
		p.pr(LBRACE)
		p.stmtList(x.Stmts)
		p.pr(RBRACE)
	case IfStmt:
		p.pr(IF)
		p.semicolonIfNil(x.Cond)
		p.pr(THEN)
		p.stmtList(x.ThenStmts)
		for _, el := range x.Elifs {
			p.pr(ELIF)
			p.semicolonIfNil(el.Cond)
			p.pr(THEN)
			p.stmtList(el.ThenStmts)
		}
		if len(x.ElseStmts) > 0 {
			p.pr(ELSE)
			p.stmtList(x.ElseStmts)
		}
		p.pr(FI)
	case StmtCond:
		p.stmtList(x.Stmts)
	case CStyleCond:
		p.pr(" ", DLPAREN, x.Cond, DRPAREN, SEMICOLON, " ")
	case WhileStmt:
		p.pr(WHILE)
		p.semicolonIfNil(x.Cond)
		p.pr(DO)
		p.stmtList(x.DoStmts)
		p.pr(DONE)
	case UntilStmt:
		p.pr(UNTIL)
		p.semicolonIfNil(x.Cond)
		p.pr(DO)
		p.stmtList(x.DoStmts)
		p.pr(DONE)
	case ForStmt:
		p.pr(FOR, " ", x.Cond, SEMICOLON, " ", DO)
		p.stmtList(x.DoStmts)
		p.pr(DONE)
	case WordIter:
		p.pr(x.Name)
		if len(x.List) > 0 {
			p.pr(" ", IN, " ")
			p.wordJoin(x.List, " ")
		}
	case CStyleLoop:
		p.pr(DLPAREN, x.Init, SEMICOLON, " ", x.Cond,
			SEMICOLON, " ", x.Post, DRPAREN)
	case UnaryExpr:
		if !x.Post {
			p.pr(x.Op)
		}
		p.pr(x.X)
		if x.Post {
			p.pr(x.Op)
		}
	case BinaryExpr:
		p.pr(x.X)
		if x.Op != COMMA {
			p.pr(" ")
		}
		p.pr(x.Op, " ", x.Y)
	case FuncDecl:
		if x.BashStyle {
			p.pr(FUNCTION, " ")
		}
		p.pr(x.Name, LPAREN, RPAREN, " ", x.Body)
	case Word:
		p.nodeJoin(x.Parts, "")
	case Lit:
		p.pr(x.Value)
	case SglQuoted:
		p.pr(SQUOTE, x.Value, SQUOTE)
	case Quoted:
		stop := x.Quote
		if stop == DOLLSQ {
			stop = SQUOTE
		} else if stop == DOLLDQ {
			stop = DQUOTE
		}
		p.pr(x.Quote)
		p.nodeJoin(x.Parts, "")
		p.pr(stop)
	case CmdSubst:
		if x.Backquotes {
			p.pr(BQUOTE)
		} else {
			p.pr(DOLLPR)
		}
		p.stmtJoin(x.Stmts)
		if x.Backquotes {
			p.pr(BQUOTE)
		} else {
			p.pr(RPAREN)
		}
	case ParamExp:
		if x.Short {
			p.pr(DOLLAR, x.Param)
			return
		}
		p.pr(DOLLBR)
		if x.Length {
			p.pr(HASH)
		}
		p.pr(x.Param)
		if x.Ind != nil {
			p.pr(LBRACK, x.Ind.Word, RBRACK)
		}
		if x.Repl != nil {
			if x.Repl.All {
				p.pr(QUO)
			}
			p.pr(QUO, x.Repl.Orig, QUO, x.Repl.With)
		} else if x.Exp != nil {
			p.pr(x.Exp.Op, x.Exp.Word)
		}
		p.pr(RBRACE)
	case ArithmExpr:
		p.pr(DOLLDP, x.X, DRPAREN)
	case ParenExpr:
		p.pr(LPAREN, x.X, RPAREN)
	case CaseStmt:
		p.pr(CASE, " ", x.Word, " ", IN)
		for i, pl := range x.List {
			if i > 0 {
				p.pr(DSEMICOLON)
			}
			p.pr(" ")
			p.wordJoin(pl.Patterns, " "+OR.String()+" ")
			p.pr(RPAREN, " ")
			p.stmtJoin(pl.Stmts)
		}
		p.pr(SEMICOLON, " ", ESAC)
	case DeclStmt:
		if x.Local {
			p.pr(LOCAL)
		} else {
			p.pr(DECLARE)
		}
		for _, w := range x.Opts {
			p.pr(" ", w)
		}
		for _, a := range x.Assigns {
			p.pr(" ", a)
		}
	case ArrayExpr:
		p.pr(LPAREN)
		p.wordJoin(x.List, " ")
		p.pr(RPAREN)
	case CmdInput:
		if p.contiguous {
			// avoid conflict with <<
			p.pr(" ")
		}
		p.pr(CMDIN)
		p.stmtJoin(x.Stmts)
		p.pr(RPAREN)
	case EvalStmt:
		p.pr(EVAL, " ", x.Stmt)
	case LetStmt:
		p.pr(LET, " ")
		p.nodeJoin(x.Exprs, " ")
	}
}

func (p *printer) nodeJoin(ns []Node, sep string) {
	for i, n := range ns {
		if i > 0 {
			p.pr(sep)
		}
		p.node(n)
	}
}

func (p *printer) wordJoin(ws []Word, sep string) {
	for i, w := range ws {
		if i > 0 {
			p.pr(sep)
		}
		p.node(w)
	}
}

func (p *printer) stmtJoin(stmts []Stmt) {
	for i, s := range stmts {
		if p.needNewline {
			p.pr("\n")
		} else if i > 0 {
			p.pr(SEMICOLON, " ")
		}
		p.node(s)
	}
}

func (p *printer) stmtList(stmts []Stmt) {
	if len(stmts) == 0 {
		p.pr(SEMICOLON, " ")
		return
	}
	p.pr(" ")
	p.stmtJoin(stmts)
	if p.needNewline {
		p.pr("\n")
	} else {
		p.pr(SEMICOLON, " ")
	}
}

func (p *printer) semicolonIfNil(v interface{}) {
	if v == nil {
		p.pr(SEMICOLON, " ")
		return
	}
	p.node(v)
}
