// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"fmt"
	"path"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mvdan/sh/syntax"
)

func (r *Runner) paramExp(pe *syntax.ParamExp) string {
	name := pe.Param.Value
	val := ""
	set := false
	switch name {
	case "#":
		val = strconv.Itoa(len(r.args))
	case "*", "@":
		val = strings.Join(r.args, " ")
	case "?":
		val = strconv.Itoa(r.exit)
	default:
		if n, err := strconv.Atoi(name); err == nil {
			if i := n - 1; i < len(r.args) {
				val, set = r.args[i], true
			}
		} else {
			val, set = r.lookupVar(name)
		}
	}
	switch {
	case pe.Length:
		val = strconv.Itoa(utf8.RuneCountInString(val))
	case pe.Excl:
		val, set = r.lookupVar(val)
	}
	if pe.Ind != nil {
		panic("unhandled param exp index")
	}
	slicePos := func(expr syntax.ArithmExpr) int {
		p := r.arithm(expr)
		if p < 0 {
			p = len(val) + p
			if p < 0 {
				p = len(val)
			}
		} else if p > len(val) {
			p = len(val)
		}
		return p
	}
	if pe.Slice != nil {
		if pe.Slice.Offset != nil {
			offset := slicePos(pe.Slice.Offset)
			val = val[offset:]
		}
		if pe.Slice.Length != nil {
			length := slicePos(pe.Slice.Length)
			val = val[:length]
		}
	}
	if pe.Repl != nil {
		orig := r.loneWord(pe.Repl.Orig)
		with := r.loneWord(pe.Repl.With)
		n := 1
		if pe.Repl.All {
			n = -1
		}
		val = strings.Replace(val, orig, with, n)
	}
	if pe.Exp != nil {
		arg := r.loneWord(pe.Exp.Word)
		switch pe.Exp.Op {
		case syntax.SubstColPlus:
			if val == "" {
				break
			}
			fallthrough
		case syntax.SubstPlus:
			if set {
				val = arg
			}
		case syntax.SubstMinus:
			if set {
				break
			}
			fallthrough
		case syntax.SubstColMinus:
			if val == "" {
				val = arg
			}
		case syntax.SubstQuest:
			if set {
				break
			}
			fallthrough
		case syntax.SubstColQuest:
			if val == "" {
				r.errf("%s", arg)
				r.exit = 1
				r.lastExit()
			}
		case syntax.SubstAssgn:
			if set {
				break
			}
			fallthrough
		case syntax.SubstColAssgn:
			if val == "" {
				r.setVar(name, arg)
				val = arg
			}
		case syntax.RemSmallPrefix:
			val = removePattern(val, arg, false, false)
		case syntax.RemLargePrefix:
			val = removePattern(val, arg, false, true)
		case syntax.RemSmallSuffix:
			val = removePattern(val, arg, true, false)
		case syntax.RemLargeSuffix:
			val = removePattern(val, arg, true, true)
		case syntax.UpperFirst:
			rs := []rune(val)
			if len(rs) > 0 {
				rs[0] = unicode.ToUpper(rs[0])
			}
			val = string(rs)
		case syntax.UpperAll:
			val = strings.ToUpper(val)
		case syntax.LowerFirst:
			rs := []rune(val)
			if len(rs) > 0 {
				rs[0] = unicode.ToLower(rs[0])
			}
			val = string(rs)
		case syntax.LowerAll:
			val = strings.ToLower(val)
		//case syntax.OtherParamOps:
		default:
			panic(fmt.Sprintf("unhandled param expansion op: %v", pe.Exp.Op))
		}
	}
	return val
}

func removePattern(val, pattern string, fromEnd, longest bool) string {
	// TODO: really slow to not re-implement path.Match.
	last := val
	s := val
	i := len(val)
	if fromEnd {
		i = 0
	}
	for {
		if m, _ := path.Match(pattern, s); m {
			last = val[i:]
			if fromEnd {
				last = val[:i]
			}
			if longest {
				return last
			}
		}
		if fromEnd {
			if i++; i >= len(val) {
				break
			}
			s = val[i:]
		} else {
			if i--; i < 1 {
				break
			}
			s = val[:i]
		}
	}
	return last
}
