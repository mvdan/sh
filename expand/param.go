// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package expand

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

type Environ interface {
	Get(name string) Variable
	Set(name string, vr Variable)
	Delete(name string)
	Each(func(name string, vr Variable) bool)
	Sub() Environ
}

type Variable struct {
	Local    bool
	Exported bool
	ReadOnly bool
	NameRef  bool
	Value    VarValue
}

// VarValue is one of:
//
//     nil (unset variable)
//     StringVal
//     IndexArray
//     AssocArray
type VarValue interface {
	String() string
}

type StringVal string

func (s StringVal) String() string {
	return string(s)
}

type IndexArray []string

func (i IndexArray) String() string {
	if len(i) == 0 {
		return ""
	}
	return i[0]
}

type AssocArray map[string]string

func (a AssocArray) String() string {
	// nothing to do
	return ""
}

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

func (e *ExpandContext) paramExp(ctx context.Context, pe *syntax.ParamExp) string {
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
	var vr Variable
	switch name {
	case "LINENO":
		// This is the only parameter expansion that the environment
		// interface cannot satisfy.
		line := uint64(e.curParam.Pos().Line())
		vr.Value = StringVal(strconv.FormatUint(line, 10))
	default:
		vr = e.Env.Get(name)
	}
	set := vr != Variable{}
	str := e.varStr(vr, 0)
	if index != nil {
		str = e.varInd(ctx, vr, index, 0)
	}
	slicePos := func(expr syntax.ArithmExpr) int {
		p := e.Arithm(ctx, expr)
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
			vr = e.Env.Get(str)
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
		orig := e.Pattern(ctx, pe.Repl.Orig)
		with := e.Literal(ctx, pe.Repl.With)
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
		arg := e.Literal(ctx, pe.Exp.Word)
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

func (e *ExpandContext) varStr(vr Variable, depth int) string {
	if vr.Value == nil || depth > maxNameRefDepth {
		return ""
	}
	if vr.NameRef {
		vr = e.Env.Get(string(vr.Value.(StringVal)))
		return e.varStr(vr, depth+1)
	}
	return vr.Value.String()
}

// maxNameRefDepth defines the maximum number of times to follow
// references when expanding a variable. Otherwise, simple name
// reference loops could crash the interpreter quite easily.
const maxNameRefDepth = 100

func (e *ExpandContext) varInd(ctx context.Context, vr Variable, idx syntax.ArithmExpr, depth int) string {
	if depth > maxNameRefDepth {
		return ""
	}
	switch x := vr.Value.(type) {
	case StringVal:
		if vr.NameRef {
			vr = e.Env.Get(string(x))
			return e.varInd(ctx, vr, idx, depth+1)
		}
		if e.Arithm(ctx, idx) == 0 {
			return string(x)
		}
	case IndexArray:
		switch anyOfLit(idx, "@", "*") {
		case "@":
			return strings.Join(x, " ")
		case "*":
			return e.ifsJoin([]string(x))
		}
		i := e.Arithm(ctx, idx)
		if len(x) > 0 {
			return x[i]
		}
	case AssocArray:
		if lit := anyOfLit(idx, "@", "*"); lit != "" {
			var strs []string
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				strs = append(strs, x[k])
			}
			if lit == "*" {
				return e.ifsJoin(strs)
			}
			return strings.Join(strs, " ")
		}
		return x[e.Literal(ctx, idx.(*syntax.Word))]
	}
	return ""
}

func (e *ExpandContext) namesByPrefix(prefix string) []string {
	var names []string
	e.Env.Each(func(name string, vr Variable) bool {
		if strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
		return true
	})
	return names
}
