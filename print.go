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

	compactArithm bool
}

var (
	contiguousRight = map[Token]bool{
		DOLLPR:  true,
		LPAREN:  true,
		DLPAREN: true,
		BQUOTE:  true,
		CMDIN:   true,
		DOLLDP:  true,
	}
	contiguousLeft = map[Token]bool{
		SEMICOLON: true,
		RPAREN:    true,
		DRPAREN:   true,
		COMMA:     true,
	}
)

func (p *printer) space(b byte) {
	if p.err != nil {
		return
	}
	_, p.err = p.w.Write([]byte{b})
	p.contiguous = false
}

func (p *printer) nonSpaced(a ...interface{}) {
	for _, v := range a {
		if p.err != nil {
			break
		}
		switch x := v.(type) {
		case string:
			if len(x) > 0 {
				last := x[len(x)-1]
				p.contiguous = !space[last]
			}
			_, p.err = fmt.Fprint(p.w, x)
		case Token:
			p.contiguous = !contiguousRight[x]
			_, p.err = fmt.Fprint(p.w, x)
		default:
			p.node(v)
		}
	}
}

func (p *printer) spaced(a ...interface{}) {
	for _, v := range a {
		if v == nil {
			continue
		}
		if t, ok := v.(Token); ok && contiguousLeft[t] {
		} else if p.contiguous {
			p.space(' ')
		}
		p.nonSpaced(v)
	}
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
		p.spaced(x.Node)
		for _, r := range x.Redirs {
			p.spaced(r.N)
			p.nonSpaced(r.Op, r.Word)
			p.needNewline = r.Op == SHL || r.Op == DHEREDOC
		}
		if x.Background {
			p.spaced(AND)
		}
	case Assign:
		if x.Name != nil {
			p.spaced(x.Name)
			if x.Append {
				p.nonSpaced(ADD_ASSIGN)
			} else {
				p.nonSpaced(ASSIGN)
			}
		}
		p.nonSpaced(x.Value)
	case Command:
		p.wordJoin(x.Args)
	case Subshell:
		p.spaced(LPAREN)
		if len(x.Stmts) == 0 {
			// avoid conflict with ()
			p.space(' ')
		}
		p.stmtJoin(x.Stmts)
		p.nonSpaced(RPAREN)
	case Block:
		p.spaced(LBRACE)
		p.stmtList(x.Stmts)
		p.spaced(RBRACE)
	case IfStmt:
		p.spaced(IF, x.Cond, SEMICOLON, THEN)
		p.stmtList(x.ThenStmts)
		for _, el := range x.Elifs {
			p.spaced(ELIF, el.Cond, SEMICOLON, THEN)
			p.stmtList(el.ThenStmts)
		}
		if len(x.ElseStmts) > 0 {
			p.spaced(ELSE)
			p.stmtList(x.ElseStmts)
		}
		p.spaced(FI)
	case StmtCond:
		p.stmtJoin(x.Stmts)
	case CStyleCond:
		p.spaced(DLPAREN, x.Cond, DRPAREN)
	case WhileStmt:
		p.spaced(WHILE, x.Cond, SEMICOLON, DO)
		p.stmtList(x.DoStmts)
		p.spaced(DONE)
	case UntilStmt:
		p.spaced(UNTIL, x.Cond, SEMICOLON, DO)
		p.stmtList(x.DoStmts)
		p.spaced(DONE)
	case ForStmt:
		p.spaced(FOR, x.Cond, SEMICOLON, DO)
		p.stmtList(x.DoStmts)
		p.spaced(DONE)
	case WordIter:
		p.spaced(x.Name)
		if len(x.List) > 0 {
			p.spaced(IN)
			p.wordJoin(x.List)
		}
	case CStyleLoop:
		p.spaced(DLPAREN, x.Init, SEMICOLON, x.Cond,
			SEMICOLON, x.Post, DRPAREN)
	case UnaryExpr:
		if x.Post {
			p.nonSpaced(x.X, x.Op)
		} else {
			p.nonSpaced(x.Op)
			p.contiguous = false
			p.nonSpaced(x.X)
		}
	case BinaryExpr:
		if p.compactArithm {
			p.nonSpaced(x.X, x.Op, x.Y)
		} else {
			p.spaced(x.X, x.Op, x.Y)
		}
	case FuncDecl:
		if x.BashStyle {
			p.spaced(FUNCTION)
		}
		p.spaced(x.Name)
		p.nonSpaced(LPAREN, RPAREN)
		p.spaced(x.Body)
	case Word:
		for _, n := range x.Parts {
			p.nonSpaced(n)
		}
	case Lit:
		p.nonSpaced(x.Value)
	case SglQuoted:
		p.nonSpaced(SQUOTE, x.Value, SQUOTE)
	case Quoted:
		p.nonSpaced(x.Quote)
		for _, n := range x.Parts {
			p.nonSpaced(n)
		}
		p.nonSpaced(quotedStop(x.Quote))
	case CmdSubst:
		if x.Backquotes {
			p.nonSpaced(BQUOTE)
		} else {
			p.nonSpaced(DOLLPR)
		}
		p.stmtJoin(x.Stmts)
		if x.Backquotes {
			p.nonSpaced(BQUOTE)
		} else {
			p.nonSpaced(RPAREN)
		}
	case ParamExp:
		if x.Short {
			p.nonSpaced(DOLLAR, x.Param)
			return
		}
		p.nonSpaced(DOLLBR)
		if x.Length {
			p.nonSpaced(HASH)
		}
		p.nonSpaced(x.Param)
		if x.Ind != nil {
			p.nonSpaced(LBRACK, x.Ind.Word, RBRACK)
		}
		if x.Repl != nil {
			if x.Repl.All {
				p.nonSpaced(QUO)
			}
			p.nonSpaced(QUO, x.Repl.Orig, QUO, x.Repl.With)
		} else if x.Exp != nil {
			p.nonSpaced(x.Exp.Op, x.Exp.Word)
		}
		p.nonSpaced(RBRACE)
	case ArithmExpr:
		p.nonSpaced(DOLLDP, x.X, DRPAREN)
	case ParenExpr:
		p.nonSpaced(LPAREN)
		oldCompact := p.compactArithm
		p.compactArithm = false
		p.nonSpaced(x.X)
		p.compactArithm = oldCompact
		p.nonSpaced(RPAREN)
	case CaseStmt:
		p.spaced(CASE, x.Word, IN)
		for i, pl := range x.List {
			if i > 0 {
				p.nonSpaced(DSEMICOLON)
			}
			for i, w := range pl.Patterns {
				if i > 0 {
					p.spaced(OR)
				}
				p.spaced(w)
			}
			p.nonSpaced(RPAREN)
			p.stmtJoin(pl.Stmts)
		}
		p.spaced(SEMICOLON, ESAC)
	case DeclStmt:
		if x.Local {
			p.spaced(LOCAL)
		} else {
			p.spaced(DECLARE)
		}
		for _, w := range x.Opts {
			p.spaced(w)
		}
		for _, a := range x.Assigns {
			p.spaced(a)
		}
	case ArrayExpr:
		p.nonSpaced(LPAREN)
		p.wordJoin(x.List)
		p.nonSpaced(RPAREN)
	case CmdInput:
		// avoid conflict with <<
		p.spaced(CMDIN)
		p.stmtJoin(x.Stmts)
		p.nonSpaced(RPAREN)
	case EvalStmt:
		p.spaced(EVAL, x.Stmt)
	case LetStmt:
		p.spaced(LET)
		p.compactArithm = true
		for _, n := range x.Exprs {
			p.spaced(n)
		}
		p.compactArithm = false
	}
}

func (p *printer) wordJoin(ws []Word) {
	for _, w := range ws {
		p.spaced(w)
	}
}

func (p *printer) stmtJoin(stmts []Stmt) {
	for i, s := range stmts {
		if p.needNewline {
			p.space('\n')
		} else if i > 0 {
			p.nonSpaced(SEMICOLON)
		}
		p.node(s)
	}
}

func (p *printer) stmtList(stmts []Stmt) {
	if len(stmts) == 0 {
		p.nonSpaced(SEMICOLON)
		return
	}
	p.stmtJoin(stmts)
	if p.needNewline {
		p.space('\n')
	} else {
		p.nonSpaced(SEMICOLON)
	}
}
