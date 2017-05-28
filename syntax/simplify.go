// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import "bytes"

// Simplify simplifies a given program.
//
// This function is EXPERIMENTAL; it may change or disappear at any
// point until this notice is removed.
func Simplify(f *File) {
	Walk(f, simpleVisit)
}

func simpleVisit(node Node) bool {
	switch x := node.(type) {
	case *Assign:
		if x.Index != nil {
			x.Index = removeParensArithm(x.Index)
			x.Index = inlineSimpleParams(x.Index)
		}
	case *ParamExp:
		if x.Index != nil {
			x.Index = removeParensArithm(x.Index)
			x.Index = inlineSimpleParams(x.Index)
		}
		if x.Slice == nil {
			break
		}
		if x.Slice.Offset != nil {
			x.Slice.Offset = removeParensArithm(x.Slice.Offset)
			x.Slice.Offset = inlineSimpleParams(x.Slice.Offset)
		}
		if x.Slice.Length != nil {
			x.Slice.Length = removeParensArithm(x.Slice.Length)
			x.Slice.Length = inlineSimpleParams(x.Slice.Length)
		}
	case *ArithmExp:
		x.X = removeParensArithm(x.X)
		x.X = inlineSimpleParams(x.X)
	case *ArithmCmd:
		x.X = removeParensArithm(x.X)
		x.X = inlineSimpleParams(x.X)
	case *ParenArithm:
		x.X = removeParensArithm(x.X)
		x.X = inlineSimpleParams(x.X)
	case *BinaryArithm:
		x.X = inlineSimpleParams(x.X)
		x.Y = inlineSimpleParams(x.Y)
	case *CmdSubst:
		x.Stmts = inlineSubshell(x.Stmts)
	case *Subshell:
		x.Stmts = inlineSubshell(x.Stmts)
	case *Word:
		x.Parts = simplifyWord(x.Parts)
	case *TestClause:
		x.X = removeParensTest(x.X)
		x.X = removeNegateTest(x.X)
	case *ParenTest:
		x.X = removeParensTest(x.X)
		x.X = removeNegateTest(x.X)
	case *BinaryTest:
		x.X = unquoteParams(x.X)
		x.X = removeNegateTest(x.X)
		switch x.Op {
		case TsMatch, TsNoMatch:
			// unquoting enables globbing
		default:
			x.Y = unquoteParams(x.Y)
		}
		x.Y = removeNegateTest(x.Y)
	case *UnaryTest:
		x.X = unquoteParams(x.X)
	}
	return true
}

func simplifyWord(wps []WordPart) []WordPart {
parts:
	for i, wp := range wps {
		dq, _ := wp.(*DblQuoted)
		if dq == nil || len(dq.Parts) != 1 {
			break
		}
		lit, _ := dq.Parts[0].(*Lit)
		if lit == nil {
			break
		}
		var buf bytes.Buffer
		escaped := false
		for _, r := range lit.Value {
			switch r {
			case '\\':
				escaped = !escaped
				if escaped {
					continue
				}
			case '\'':
				continue parts
			case '$', '"', '`':
				escaped = false
			default:
				if escaped {
					continue parts
				}
				escaped = false
			}
			buf.WriteRune(r)
		}
		newVal := buf.String()
		if newVal == lit.Value {
			break
		}
		wps[i] = &SglQuoted{
			Position: dq.Position,
			Dollar:   dq.Dollar,
			Value:    newVal,
		}
	}
	return wps
}

func removeParensArithm(x ArithmExpr) ArithmExpr {
	for {
		par, _ := x.(*ParenArithm)
		if par == nil {
			return x
		}
		x = par.X
	}
}

func inlineSimpleParams(x ArithmExpr) ArithmExpr {
	w, _ := x.(*Word)
	if w == nil || len(w.Parts) != 1 {
		return x
	}
	pe, _ := w.Parts[0].(*ParamExp)
	if pe == nil || !ValidName(pe.Param.Value) {
		return x
	}
	if pe.Indirect || pe.Length || pe.Width || pe.Key != nil ||
		pe.Slice != nil || pe.Repl != nil || pe.Exp != nil {
		return x
	}
	if pe.Index != nil {
		pe.Short = true
		return w
	}
	return &Word{Parts: []WordPart{pe.Param}}
}

func inlineSubshell(stmts []*Stmt) []*Stmt {
	for len(stmts) == 1 {
		s := stmts[0]
		if s.Negated || s.Background || s.Coprocess ||
			len(s.Assigns) > 0 || len(s.Redirs) > 0 {
			break
		}
		sub, _ := s.Cmd.(*Subshell)
		if sub == nil {
			break
		}
		stmts = sub.Stmts
	}
	return stmts
}

func unquoteParams(x TestExpr) TestExpr {
	w, _ := x.(*Word)
	if w == nil || len(w.Parts) != 1 {
		return x
	}
	dq, _ := w.Parts[0].(*DblQuoted)
	if dq == nil || len(dq.Parts) != 1 {
		return x
	}
	if _, ok := dq.Parts[0].(*ParamExp); !ok {
		return x
	}
	w.Parts = dq.Parts
	return w
}

func removeParensTest(x TestExpr) TestExpr {
	for {
		par, _ := x.(*ParenTest)
		if par == nil {
			return x
		}
		x = par.X
	}
}

func removeNegateTest(x TestExpr) TestExpr {
	u, _ := x.(*UnaryTest)
	if u == nil || u.Op != TsNot {
		return x
	}
	switch y := u.X.(type) {
	case *UnaryTest:
		switch y.Op {
		case TsEmpStr:
			y.Op = TsNempStr
			return y
		case TsNempStr:
			y.Op = TsEmpStr
			return y
		case TsNot:
			return y.X
		}
	case *BinaryTest:
		switch y.Op {
		case TsMatch:
			y.Op = TsNoMatch
			return y
		case TsNoMatch:
			y.Op = TsMatch
			return y
		}
	}
	return x
}
