// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"fmt"
	"io"
	"strings"
)

func Fprint(w io.Writer, n Node) error {
	p := printer{
		w:       w,
		curLine: 1,
		level:   -1,
	}
	p.node(n)
	p.space('\n')
	return p.err
}

type printer struct {
	w   io.Writer
	err error

	contiguous bool

	curLine int
	level   int

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
		SEMICOLON:  true,
		DSEMICOLON: true,
		RPAREN:     true,
		DRPAREN:    true,
		COMMA:      true,
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
			_, p.err = io.WriteString(p.w, x)
			p.curLine += strings.Count(x, "\n")
		case Token:
			p.contiguous = !contiguousRight[x]
			_, p.err = fmt.Fprint(p.w, x)
		case Node:
			p.node(x)
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

func (p *printer) indent(n int) {
	if p.err != nil {
		return
	}
	_, p.err = io.WriteString(p.w, strings.Repeat("\t", n))
}

func (p *printer) separate(pos Pos, fallback bool) {
	if p.curLine == 0 {
		return
	}
	if pos.Line > p.curLine {
		p.space('\n')
		if pos.Line > p.curLine+1 {
			// preserve single empty lines
			p.space('\n')
		}
		p.indent(p.level)
		p.curLine = pos.Line
	} else if fallback {
		p.nonSpaced(SEMICOLON)
	}
}

func (p *printer) sepSemicolon(v interface{}, pos Pos) {
	p.separate(pos, true)
	p.spaced(v)
}

func (p *printer) sepNewline(v interface{}, pos Pos) {
	p.separate(pos, false)
	p.spaced(v)
}

func (p *printer) node(n Node) {
	switch x := n.(type) {
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
		p.sepNewline(RPAREN, x.Rparen)
	case Block:
		p.spaced(LBRACE)
		p.stmtJoin(x.Stmts)
		p.sepSemicolon(RBRACE, x.Rbrace)
	case IfStmt:
		p.spaced(IF, x.Cond, SEMICOLON, THEN)
		p.stmtJoin(x.ThenStmts)
		for _, el := range x.Elifs {
			p.sepSemicolon(ELIF, el.Elif)
			p.spaced(el.Cond, SEMICOLON, THEN)
			p.stmtJoin(el.ThenStmts)
		}
		if len(x.ElseStmts) > 0 {
			p.sepSemicolon(ELSE, x.Else)
			p.stmtJoin(x.ElseStmts)
		}
		p.sepSemicolon(FI, x.Fi)
	case StmtCond:
		p.stmtJoin(x.Stmts)
	case CStyleCond:
		p.spaced(DLPAREN, x.Cond, DRPAREN)
	case WhileStmt:
		p.spaced(WHILE, x.Cond, SEMICOLON, DO)
		p.stmtJoin(x.DoStmts)
		p.sepSemicolon(DONE, x.Done)
	case UntilStmt:
		p.spaced(UNTIL, x.Cond, SEMICOLON, DO)
		p.stmtJoin(x.DoStmts)
		p.sepSemicolon(DONE, x.Done)
	case ForStmt:
		p.spaced(FOR, x.Cond, SEMICOLON, DO)
		p.stmtJoin(x.DoStmts)
		p.sepSemicolon(DONE, x.Done)
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
		for _, pl := range x.List {
			p.separate(wordFirstPos(pl.Patterns), false)
			for i, w := range pl.Patterns {
				if i > 0 {
					p.spaced(OR)
				}
				p.spaced(w)
			}
			p.nonSpaced(RPAREN)
			p.stmtJoin(pl.Stmts)
			p.level++
			p.sepNewline(DSEMICOLON, pl.Dsemi)
			p.level--
		}
		if len(x.List) == 0 {
			p.sepSemicolon(ESAC, x.Esac)
		} else {
			p.sepNewline(ESAC, x.Esac)
		}
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
	p.level++
	for i, s := range stmts {
		p.separate(s.Pos(), i > 0)
		p.node(s)
	}
	p.level--
}
