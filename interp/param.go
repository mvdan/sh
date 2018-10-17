// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"mvdan.cc/sh/syntax"
)

func anyOfLit(v interface{}, vals ...string) string {
	word, _ := v.(*syntax.Word)
	if word == nil || len(word.Parts) != 1 {
		return ""
	}
	lit, ok := word.Parts[0].(*syntax.Lit)
	if !ok {
		return ""
	}
	for _, val := range vals {
		if lit.Value == val {
			return val
		}
	}
	return ""
}

type UnsetParameterError struct {
	Expr    *syntax.ParamExp
	Message string
}

func (u UnsetParameterError) Error() string {
	return u.Message
}

func (e *expandContext) paramExp(ctx context.Context, pe *syntax.ParamExp) string {
	oldParam := e.curParam
	e.curParam = pe
	defer func() { e.curParam = oldParam }()

	name := pe.Param.Value
	index := pe.Index
	switch name {
	case "@", "*":
		index = &syntax.Word{Parts: []syntax.WordPart{
			&syntax.Lit{Value: name},
		}}
	}
	vr := e.env.Get(name)
	set := vr != Variable{}
	str := e.varStr(vr, 0)
	if index != nil {
		str = e.varInd(ctx, vr, index, 0)
	}
	slicePos := func(expr syntax.ArithmExpr) int {
		p := e.arithm(ctx, expr)
		if p < 0 {
			p = len(str) + p
			if p < 0 {
				p = len(str)
			}
		} else if p > len(str) {
			p = len(str)
		}
		return p
	}
	elems := []string{str}
	if anyOfLit(index, "@", "*") != "" {
		switch x := vr.Value.(type) {
		case nil:
			elems = nil
		case IndexArray:
			elems = x
		}
	}
	switch {
	case pe.Length:
		n := len(elems)
		if anyOfLit(index, "@", "*") == "" {
			n = utf8.RuneCountInString(str)
		}
		str = strconv.Itoa(n)
	case pe.Excl:
		var strs []string
		if pe.Names != 0 {
			strs = e.namesByPrefix(pe.Param.Value)
		} else if vr.NameRef {
			strs = append(strs, string(vr.Value.(StringVal)))
		} else if x, ok := vr.Value.(IndexArray); ok {
			for i, e := range x {
				if e != "" {
					strs = append(strs, strconv.Itoa(i))
				}
			}
		} else if x, ok := vr.Value.(AssocArray); ok {
			for k := range x {
				strs = append(strs, k)
			}
		} else if str != "" {
			vr = e.env.Get(str)
			strs = append(strs, e.varStr(vr, 0))
		}
		sort.Strings(strs)
		str = strings.Join(strs, " ")
	case pe.Slice != nil:
		if pe.Slice.Offset != nil {
			offset := slicePos(pe.Slice.Offset)
			str = str[offset:]
		}
		if pe.Slice.Length != nil {
			length := slicePos(pe.Slice.Length)
			str = str[:length]
		}
	case pe.Repl != nil:
		orig := e.lonePattern(ctx, pe.Repl.Orig)
		with := e.loneWord(ctx, pe.Repl.With)
		n := 1
		if pe.Repl.All {
			n = -1
		}
		locs := findAllIndex(orig, str, n)
		buf := e.strBuilder()
		last := 0
		for _, loc := range locs {
			buf.WriteString(str[last:loc[0]])
			buf.WriteString(with)
			last = loc[1]
		}
		buf.WriteString(str[last:])
		str = buf.String()
	case pe.Exp != nil:
		arg := e.loneWord(ctx, pe.Exp.Word)
		switch op := pe.Exp.Op; op {
		case syntax.SubstColPlus:
			if str == "" {
				break
			}
			fallthrough
		case syntax.SubstPlus:
			if set {
				str = arg
			}
		case syntax.SubstMinus:
			if set {
				break
			}
			fallthrough
		case syntax.SubstColMinus:
			if str == "" {
				str = arg
			}
		case syntax.SubstQuest:
			if set {
				break
			}
			fallthrough
		case syntax.SubstColQuest:
			if str == "" {
				e.err(UnsetParameterError{
					Expr:    pe,
					Message: arg,
				})
			}
		case syntax.SubstAssgn:
			if set {
				break
			}
			fallthrough
		case syntax.SubstColAssgn:
			if str == "" {
				e.envSet(name, arg)
				str = arg
			}
		case syntax.RemSmallPrefix, syntax.RemLargePrefix,
			syntax.RemSmallSuffix, syntax.RemLargeSuffix:
			suffix := op == syntax.RemSmallSuffix ||
				op == syntax.RemLargeSuffix
			large := op == syntax.RemLargePrefix ||
				op == syntax.RemLargeSuffix
			for i, elem := range elems {
				elems[i] = removePattern(elem, arg, suffix, large)
			}
			str = strings.Join(elems, " ")
		case syntax.UpperFirst, syntax.UpperAll,
			syntax.LowerFirst, syntax.LowerAll:

			caseFunc := unicode.ToLower
			if op == syntax.UpperFirst || op == syntax.UpperAll {
				caseFunc = unicode.ToUpper
			}
			all := op == syntax.UpperAll || op == syntax.LowerAll

			// empty string means '?'; nothing to do there
			expr, err := syntax.TranslatePattern(arg, false)
			if err != nil {
				return str
			}
			rx := regexp.MustCompile(expr)

			for i, elem := range elems {
				rs := []rune(elem)
				for ri, r := range rs {
					if rx.MatchString(string(r)) {
						rs[ri] = caseFunc(r)
						if !all {
							break
						}
					}
				}
				elems[i] = string(rs)
			}
			str = strings.Join(elems, " ")
		case syntax.OtherParamOps:
			switch arg {
			case "Q":
				str = strconv.Quote(str)
			case "E":
				tail := str
				var rns []rune
				for tail != "" {
					var rn rune
					rn, _, tail, _ = strconv.UnquoteChar(tail, 0)
					rns = append(rns, rn)
				}
				str = string(rns)
			case "P", "A", "a":
				panic(fmt.Sprintf("unhandled @%s param expansion", arg))
			default:
				panic(fmt.Sprintf("unexpected @%s param expansion", arg))
			}
		}
	}
	return str
}

func removePattern(str, pattern string, fromEnd, greedy bool) string {
	expr, err := syntax.TranslatePattern(pattern, greedy)
	if err != nil {
		return str
	}
	switch {
	case fromEnd && !greedy:
		// use .* to get the right-most (shortest) match
		expr = ".*(" + expr + ")$"
	case fromEnd:
		// simple suffix
		expr = "(" + expr + ")$"
	default:
		// simple prefix
		expr = "^(" + expr + ")"
	}
	// no need to check error as TranslatePattern returns one
	rx := regexp.MustCompile(expr)
	if loc := rx.FindStringSubmatchIndex(str); loc != nil {
		// remove the original pattern (the submatch)
		str = str[:loc[2]] + str[loc[3]:]
	}
	return str
}
