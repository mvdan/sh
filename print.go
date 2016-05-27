// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"bytes"
	"fmt"
	"strings"
)

func stringerJoin(strs []fmt.Stringer, sep string) string {
	var b bytes.Buffer
	for i, s := range strs {
		if i > 0 {
			fmt.Fprint(&b, sep)
		}
		fmt.Fprint(&b, s)
	}
	return b.String()
}

func nodeJoin(ns []Node, sep string) string {
	var b bytes.Buffer
	for i, n := range ns {
		if i > 0 {
			fmt.Fprint(&b, sep)
		}
		fmt.Fprint(&b, n)
	}
	return b.String()
}

func stmtJoinWithEnd(stmts []Stmt, end bool) string {
	var b bytes.Buffer
	newline := false
	for i, s := range stmts {
		if newline {
			newline = false
			fmt.Fprintln(&b)
		} else if i > 0 {
			fmt.Fprint(&b, "; ")
		}
		fmt.Fprint(&b, s)
		newline = s.newlineAfter()
	}
	if newline && end {
		fmt.Fprintln(&b)
	}
	return b.String()
}

func stmtJoin(stmts []Stmt) string {
	return stmtJoinWithEnd(stmts, true)
}

func stmtList(stmts []Stmt) string {
	if len(stmts) == 0 {
		return fmt.Sprint(SEMICOLON, " ")
	}
	s := stmtJoin(stmts)
	if len(s) > 0 && s[len(s)-1] == '\n' {
		return " " + s
	}
	return fmt.Sprintf(" %s%s ", s, SEMICOLON)
}

func semicolonIfNil(s fmt.Stringer) string {
	if s == nil {
		return fmt.Sprint(SEMICOLON, " ")
	}
	return s.String()
}

func wordJoin(words []Word, sep string) string {
	ns := make([]Node, len(words))
	for i, w := range words {
		ns[i] = w
	}
	return nodeJoin(ns, sep)
}

func (f File) String() string { return stmtJoinWithEnd(f.Stmts, false) }

func (s Stmt) String() string {
	var strs []fmt.Stringer
	if s.Negated {
		strs = append(strs, NOT)
	}
	for _, a := range s.Assigns {
		strs = append(strs, a)
	}
	if s.Node != nil {
		strs = append(strs, s.Node)
	}
	for _, r := range s.Redirs {
		strs = append(strs, r)
	}
	if s.Background {
		strs = append(strs, AND)
	}
	return stringerJoin(strs, " ")
}

func (a Assign) String() string {
	if a.Name == nil {
		return a.Value.String()
	}
	if a.Append {
		return fmt.Sprint(a.Name, "+=", a.Value)
	}
	return fmt.Sprint(a.Name, "=", a.Value)
}

func (r Redirect) String() string {
	if strings.HasPrefix(r.Word.String(), "<") {
		return fmt.Sprint(r.N, r.Op.String(), " ", r.Word)
	}
	return fmt.Sprint(r.N, r.Op.String(), r.Word)
}

func (c Command) String() string { return wordJoin(c.Args, " ") }

func (s Subshell) String() string {
	if len(s.Stmts) == 0 {
		// A space in between to avoid confusion with ()
		return fmt.Sprint(LPAREN, RPAREN)
	}
	return fmt.Sprint(LPAREN, stmtJoin(s.Stmts), RPAREN)
}

func (b Block) String() string {
	return fmt.Sprint(LBRACE, stmtList(b.Stmts), RBRACE)
}

func (s IfStmt) String() string {
	var b bytes.Buffer
	fmt.Fprint(&b, IF, semicolonIfNil(s.Cond), THEN, stmtList(s.ThenStmts))
	for _, elif := range s.Elifs {
		fmt.Fprint(&b, elif)
	}
	if len(s.ElseStmts) > 0 {
		fmt.Fprint(&b, ELSE, stmtList(s.ElseStmts))
	}
	fmt.Fprint(&b, FI)
	return b.String()
}

func (s StmtCond) String() string { return stmtList(s.Stmts) }

func (c CStyleCond) String() string {
	return fmt.Sprintf(" ((%s)); ", c.Cond)
}

func (e Elif) String() string {
	return fmt.Sprint(ELIF, semicolonIfNil(e.Cond), THEN, stmtList(e.ThenStmts))
}

func (w WhileStmt) String() string {
	return fmt.Sprint(WHILE, semicolonIfNil(w.Cond), DO, stmtList(w.DoStmts), DONE)
}

func (u UntilStmt) String() string {
	return fmt.Sprint(UNTIL, semicolonIfNil(u.Cond), DO, stmtList(u.DoStmts), DONE)
}

func (f ForStmt) String() string {
	return fmt.Sprint(FOR, " ", f.Cond, "; ", DO, stmtList(f.DoStmts), DONE)
}

func (w WordIter) String() string {
	if len(w.List) < 1 {
		return w.Name.String()
	}
	return fmt.Sprint(w.Name, IN, " ", wordJoin(w.List, " "))
}

func (c CStyleLoop) String() string {
	return fmt.Sprintf("((%s; %s; %s))", c.Init, c.Cond, c.Post)
}

func (u UnaryExpr) String() string {
	if u.Post {
		return fmt.Sprint(u.X, "", u.Op)
	}
	return fmt.Sprint(u.Op, "", u.X)
}

func (b BinaryExpr) String() string {
	if b.Op == COMMA {
		return fmt.Sprint(b.X, "", b.Op, b.Y)
	}
	return fmt.Sprint(b.X, b.Op, b.Y)
}

func (f FuncDecl) String() string {
	if f.BashStyle {
		return fmt.Sprint(FUNCTION, f.Name, "() ", f.Body)
	}
	return fmt.Sprint(f.Name, "() ", f.Body)
}

func (w Word) String() string { return nodeJoin(w.Parts, "") }

func (l Lit) String() string { return l.Value }

func (q SglQuoted) String() string { return `'` + q.Value + `'` }

func (q Quoted) String() string {
	stop := q.Quote
	if stop == DOLLSQ {
		stop = SQUOTE
	} else if stop == DOLLDQ {
		stop = DQUOTE
	}
	return fmt.Sprint(q.Quote, nodeJoin(q.Parts, ""), stop)
}

func (c CmdSubst) String() string {
	if c.Backquotes {
		return "`" + stmtJoin(c.Stmts) + "`"
	}
	return fmt.Sprint(DOLLAR, "", LPAREN, stmtJoin(c.Stmts), RPAREN)
}

func (p ParamExp) String() string {
	if p.Short {
		return fmt.Sprint(DOLLAR, "", p.Param)
	}
	var b bytes.Buffer
	fmt.Fprint(&b, "${")
	if p.Length {
		fmt.Fprint(&b, HASH)
	}
	fmt.Fprint(&b, p.Param)
	if p.Ind != nil {
		fmt.Fprint(&b, p.Ind)
	}
	if p.Repl != nil {
		fmt.Fprint(&b, p.Repl)
	}
	if p.Exp != nil {
		fmt.Fprint(&b, p.Exp)
	}
	fmt.Fprint(&b, "}")
	return b.String()
}

func (i Index) String() string { return fmt.Sprintf("[%s]", i.Word) }

func (r Replace) String() string {
	if r.All {
		return fmt.Sprintf("//%s/%s", r.Orig, r.With)
	}
	return fmt.Sprintf("/%s/%s", r.Orig, r.With)
}

func (e Expansion) String() string { return fmt.Sprint(e.Op.String(), e.Word) }

func (a ArithmExpr) String() string {
	if a.X == nil {
		return "$(())"
	}
	return fmt.Sprintf("$((%s))", a.X)
}

func (p ParenExpr) String() string { return fmt.Sprintf("(%s)", p.X) }

func (c CaseStmt) String() string {
	var b bytes.Buffer
	fmt.Fprint(&b, CASE, c.Word, IN)
	for i, plist := range c.List {
		if i > 0 {
			fmt.Fprint(&b, ";;")
		}
		fmt.Fprint(&b, plist)
	}
	fmt.Fprint(&b, "; ", ESAC)
	return b.String()
}

func (p PatternList) String() string {
	return fmt.Sprintf(" %s) %s", wordJoin(p.Patterns, " | "), stmtJoin(p.Stmts))
}

func (d DeclStmt) String() string {
	var strs []fmt.Stringer
	if d.Local {
		strs = append(strs, LOCAL)
	} else {
		strs = append(strs, DECLARE)
	}
	for _, w := range d.Opts {
		strs = append(strs, w)
	}
	for _, a := range d.Assigns {
		strs = append(strs, a)
	}
	return stringerJoin(strs, " ")
}

func (a ArrayExpr) String() string {
	return fmt.Sprint(LPAREN, wordJoin(a.List, " "), RPAREN)
}

func (c CmdInput) String() string {
	return fmt.Sprint(LSS, "", LPAREN, stmtJoin(c.Stmts), RPAREN)
}

func (e EvalStmt) String() string { return fmt.Sprint(EVAL, e.Stmt) }

func (l LetStmt) String() string {
	return fmt.Sprint(LET, " ", nodeJoin(l.Exprs, " "))
}
