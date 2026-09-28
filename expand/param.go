// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package expand

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"mvdan.cc/sh/v3/internal"
	"mvdan.cc/sh/v3/internal/arithm"
	"mvdan.cc/sh/v3/pattern"
	"mvdan.cc/sh/v3/syntax"
)

func nodeLit(node syntax.Node) string {
	if word, ok := node.(*syntax.Word); ok {
		return word.Lit()
	}
	return ""
}

// assocKey expands an associative array subscript into its key.
func (e *expander) assocKey(idx syntax.ArithmExpr) (string, error) {
	return e.literal(arithm.Word(idx))
}

// UnsetParameterError is returned when a parameter expansion encounters an
// unset variable and [Config.NoUnset] has been set.
type UnsetParameterError struct {
	Node    *syntax.ParamExp
	Message string
}

func (u UnsetParameterError) Error() string {
	return fmt.Sprintf("%s: %s", u.Node.Param.Value, u.Message)
}

func overridingUnset(pe *syntax.ParamExp) bool {
	if pe.Exp == nil {
		return false
	}
	switch pe.Exp.Op {
	case syntax.AlternateUnset, syntax.AlternateUnsetOrNull,
		syntax.DefaultUnset, syntax.DefaultUnsetOrNull,
		syntax.ErrorUnset, syntax.ErrorUnsetOrNull,
		syntax.AssignUnset, syntax.AssignUnsetOrNull:
		return true
	}
	return false
}

func (e *expander) paramExp(pe *syntax.ParamExp) (string, error) {
	oldParam := e.curParam
	e.curParam = pe
	defer func() { e.curParam = oldParam }()

	if pe.Param == nil { // e.g. zsh's ${}
		return "", fmt.Errorf("unsupported")
	}
	name := pe.Param.Value
	index := pe.Index
	switch name {
	case "@", "*":
		index = &syntax.Word{Parts: []syntax.WordPart{
			&syntax.Lit{Value: name},
		}}
	}
	// "*" expansions like ${*}, ${arr[*]}, or ${!prefix*} join their
	// elements with the first IFS character; others use a space.
	join := func(elems []string) string {
		if nodeLit(index) == "*" || pe.Names == syntax.NamesPrefix {
			return e.ifsJoin(elems)
		}
		return strings.Join(elems, " ")
	}
	var vr Variable
	switch name {
	case "LINENO":
		// This is the only parameter expansion that the environment
		// interface cannot satisfy.
		line := uint64(e.curParam.Pos().Line())
		vr = Variable{Set: true, Kind: String, Str: strconv.FormatUint(line, 10)}
	default:
		vr = e.Env.Get(name)
	}
	orig := vr
	if n, v := vr.Resolve(e.Env); n != "" {
		name, vr = n, v
	}
	if e.NoUnset && !vr.IsSet() && !overridingUnset(pe) {
		return "", UnsetParameterError{
			Node:    pe,
			Message: "unbound variable",
		}
	}

	var sliceOffset, sliceLen int
	if pe.Slice != nil {
		var err error
		if pe.Slice.Offset != nil {
			sliceOffset, err = e.arithm(pe.Slice.Offset)
			if err != nil {
				return "", err
			}
		}
		if pe.Slice.Length != nil {
			sliceLen, err = e.arithm(pe.Slice.Length)
			if err != nil {
				return "", err
			}
		}
	}

	var (
		str   string
		elems []string

		indexAllElements bool // true if var has been accessed with * or @ index
		callVarInd       = true
		set              = vr.IsSet()
	)

	switch nodeLit(index) {
	case "@", "*":
		switch vr.Kind {
		case Unknown:
			elems = nil
			indexAllElements = true
		case Indexed:
			indexAllElements = true
			callVarInd = false
			elems = e.sliceElems(pe, vr.List, vr.Indexes, name == "@" || name == "*")
			str = join(elems)
		}
	}
	if callVarInd {
		var err error
		str, set, err = e.varInd(vr, index)
		if err != nil {
			return "", err
		}
	}
	if !indexAllElements {
		elems = []string{str}
	}

	switch {
	case pe.Length:
		n := len(elems)
		switch nodeLit(index) {
		case "@", "*":
		default:
			n = utf8.RuneCountInString(str)
		}
		str = strconv.Itoa(n)
	case pe.Excl:
		var strs []string
		switch {
		case pe.Names != 0:
			strs = e.namesByPrefix(pe.Param.Value)
		case orig.Kind == NameRef:
			strs = append(strs, orig.Str)
		case pe.Index != nil && vr.Kind == Indexed:
			strs = vr.indexedKeys()
		case pe.Index != nil && vr.Kind == Associative:
			strs = slices.Sorted(maps.Keys(vr.Map))
		case !vr.IsSet():
			return "", fmt.Errorf("invalid indirect expansion")
		case str == "":
			return "", nil
		default:
			vr = e.Env.Get(str)
			strs = append(strs, vr.String())
		}
		str = join(strs)
	case pe.Width:
		return "", fmt.Errorf("unsupported")
	case pe.IsSet:
		return "", fmt.Errorf("unsupported")
	case pe.Slice != nil:
		if callVarInd {
			// The offset and length are in characters, not bytes.
			rs := []rune(str)
			slicePos := func(n int) int {
				if n < 0 {
					n = len(rs) + n
					if n < 0 {
						n = len(rs)
					}
				} else if n > len(rs) {
					n = len(rs)
				}
				return n
			}
			if pe.Slice.Offset != nil {
				rs = rs[slicePos(sliceOffset):]
			}
			if pe.Slice.Length != nil {
				rs = rs[:slicePos(sliceLen)]
			}
			str = string(rs)
		} // else, elems are already sliced
	case pe.Repl != nil:
		elems, err := e.replaceElems(pe.Repl, elems)
		if err != nil {
			return "", err
		}
		str = join(elems)
	case pe.Exp != nil:
		arg, err := e.literal(pe.Exp.Word)
		if err != nil {
			return "", err
		}
		switch op := pe.Exp.Op; op {
		case syntax.AlternateUnsetOrNull:
			if str == "" {
				break
			}
			fallthrough
		case syntax.AlternateUnset:
			if set {
				str = arg
			}
		case syntax.DefaultUnset:
			if set {
				break
			}
			fallthrough
		case syntax.DefaultUnsetOrNull:
			if str == "" {
				str = arg
			}
		case syntax.ErrorUnset:
			if set {
				break
			}
			fallthrough
		case syntax.ErrorUnsetOrNull:
			if str == "" {
				return "", UnsetParameterError{
					Node:    pe,
					Message: arg,
				}
			}
		case syntax.AssignUnset:
			if set {
				break
			}
			fallthrough
		case syntax.AssignUnsetOrNull:
			if str == "" {
				if err := e.assignElem(name, vr, index, arg); err != nil {
					return "", err
				}
				str = arg
			}
		case syntax.RemSmallPrefix, syntax.RemLargePrefix,
			syntax.RemSmallSuffix, syntax.RemLargeSuffix:
			str = join(e.removePatternElems(op, arg, elems))
		case syntax.UpperFirst, syntax.UpperAll,
			syntax.LowerFirst, syntax.LowerAll:
			str = join(e.caseConvElems(op, arg, elems))
		case syntax.OtherParamOps:
			switch arg {
			case "Q":
				str, err = syntax.Quote(str, syntax.LangBash)
				if err != nil {
					return "", err
				}
			case "E":
				tail := str
				var rns []rune
				for tail != "" {
					var rn rune
					rn, _, tail, _ = strconv.UnquoteChar(tail, 0)
					rns = append(rns, rn)
				}
				str = string(rns)
			case "a":
				// ${var@a} returns variable attribute flags.
				// We use orig (before nameref resolve) for the attributes.
				str = orig.Flags()
			case "A":
				// ${var@A} returns a declare statement that recreates the variable.
				// The value may be quoted differently than in Bash.
				flags := orig.Flags()
				quoted, err := syntax.Quote(str, syntax.LangBash)
				if err != nil {
					return "", err
				}
				switch {
				case !set && flags == "":
					str = ""
				case !set:
					str = fmt.Sprintf("declare -%s %s", flags, name)
				case flags == "":
					str = fmt.Sprintf("%s=%s", name, quoted)
				default:
					str = fmt.Sprintf("declare -%s %s=%s", flags, name, quoted)
				}
			case "P":
				// TODO: implement prompt expansion (\u, \h, \w, etc.).
			case "U":
				str = strings.ToUpper(str)
			case "u":
				rs := []rune(str)
				if len(rs) > 0 {
					rs[0] = unicode.ToUpper(rs[0])
					str = string(rs)
				}
			case "L":
				str = strings.ToLower(str)
			case "K", "k":
				// TODO: implement, like @A but listing keys for assoc arrays.
			case "#": // mksh's hash of the value
				return "", fmt.Errorf("unsupported")
			default:
				panic(fmt.Sprintf("unexpected @%s param expansion", arg))
			}
		}
	}
	return str, nil
}

func removePattern(str, pat string, fromEnd, shortest bool) string {
	var mode pattern.Mode
	if shortest {
		mode |= pattern.Shortest
	}
	expr, err := pattern.Regexp(pat, mode)
	if err != nil {
		return str
	}
	switch {
	case fromEnd && shortest:
		// use .* to get the right-most shortest match
		expr = ".*(" + expr + ")$"
	case fromEnd:
		// simple suffix
		expr = "(" + expr + ")$"
	default:
		// simple prefix
		expr = "^(" + expr + ")"
	}
	rx, err := regexp.Compile(expr)
	if err != nil {
		return str
	}
	if loc := rx.FindStringSubmatchIndex(str); loc != nil {
		// remove the original pattern (the submatch)
		str = str[:loc[2]] + str[loc[3]:]
	}
	return str
}

// The helpers below never modify elems in place, as it may alias a
// variable's list of elements.

// perElemOps applies pattern removal, replacement, or case conversion to
// each element, leaving them unchanged for any whole-expansion operator.
func (e *expander) perElemOps(pe *syntax.ParamExp, elems []string) ([]string, error) {
	switch {
	case pe.Repl != nil:
		return e.replaceElems(pe.Repl, elems)
	case pe.Exp != nil:
		arg, err := e.literal(pe.Exp.Word)
		if err != nil {
			return nil, err
		}
		switch op := pe.Exp.Op; op {
		case syntax.RemSmallPrefix, syntax.RemLargePrefix,
			syntax.RemSmallSuffix, syntax.RemLargeSuffix:
			return e.removePatternElems(op, arg, elems), nil
		case syntax.UpperFirst, syntax.UpperAll,
			syntax.LowerFirst, syntax.LowerAll:
			return e.caseConvElems(op, arg, elems), nil
		}
	}
	return elems, nil
}

// replaceElems applies a ${var/pattern/repl} replacement to each element.
func (e *expander) replaceElems(repl *syntax.Replace, elems []string) ([]string, error) {
	orig, err := e.pattern(repl.Orig)
	if err != nil {
		return nil, err
	}
	if orig == "" {
		return elems, nil // nothing to replace
	}
	with, err := e.literal(repl.With)
	if err != nil {
		return nil, err
	}
	n := 1
	if repl.All {
		n = -1
	}
	out := make([]string, len(elems))
	for i, elem := range elems {
		locs := findAllIndex(orig, elem, n)
		sb := e.strBuilder()
		last := 0
		for _, loc := range locs {
			sb.WriteString(elem[last:loc[0]])
			sb.WriteString(with)
			last = loc[1]
		}
		sb.WriteString(elem[last:])
		out[i] = sb.String()
	}
	return out, nil
}

// removePatternElems applies a pattern removal operator to each element.
func (e *expander) removePatternElems(op syntax.ParExpOperator, arg string, elems []string) []string {
	suffix := op == syntax.RemSmallSuffix || op == syntax.RemLargeSuffix
	small := op == syntax.RemSmallPrefix || op == syntax.RemSmallSuffix
	out := make([]string, len(elems))
	for i, elem := range elems {
		out[i] = removePattern(elem, arg, suffix, small)
	}
	return out
}

// caseConvElems applies a case conversion operator to each element.
func (e *expander) caseConvElems(op syntax.ParExpOperator, arg string, elems []string) []string {
	caseFunc := unicode.ToLower
	if op == syntax.UpperFirst || op == syntax.UpperAll {
		caseFunc = unicode.ToUpper
	}
	all := op == syntax.UpperAll || op == syntax.LowerAll

	// empty string means '?'; nothing to do there
	expr, err := pattern.Regexp(arg, 0)
	if err != nil {
		return elems
	}
	rx, err := regexp.Compile(expr)
	if err != nil {
		return elems
	}

	out := make([]string, len(elems))
	for i, elem := range elems {
		rs := []rune(elem)
		for ri, r := range rs {
			if rx.MatchString(string(r)) {
				rs[ri] = caseFunc(r)
			}
			if !all {
				break // only the first character is considered
			}
		}
		out[i] = string(rs)
	}
	return out
}

// varInd expands an indexed variable expansion like ${a[i]}, also reporting
// whether the resulting element is set, which may be false for missing array
// elements such as the holes in a sparse array.
func (e *expander) varInd(vr Variable, idx syntax.ArithmExpr) (string, bool, error) {
	if idx == nil {
		switch vr.Kind {
		case Indexed:
			// A bare $a is the element at index zero, which may be unset.
			str, ok := vr.indexedVal(0)
			return str, ok, nil
		case Associative:
			str, ok := vr.Map["0"]
			return str, ok, nil
		}
		return vr.String(), vr.IsSet(), nil
	}
	switch vr.Kind {
	case String:
		n, err := e.arithm(idx)
		if err != nil {
			return "", false, err
		}
		if n == 0 {
			return vr.Str, vr.IsSet(), nil
		}
	case Indexed:
		switch nodeLit(idx) {
		case "*", "@":
			return strings.Join(vr.List, " "), vr.IsSet(), nil
		}
		i, err := e.arithm(idx)
		if err != nil {
			return "", false, err
		}
		if i < 0 {
			// Negative indices count from one past the maximum index.
			if i += internal.IndexedMax(vr.List, vr.Indexes) + 1; i < 0 {
				return "", false, fmt.Errorf("negative array index")
			}
		}
		if str, ok := vr.indexedVal(i); ok {
			return str, true, nil
		}
	case Associative:
		switch lit := nodeLit(idx); lit {
		case "@", "*":
			strs := slices.Sorted(maps.Values(vr.Map))
			if lit == "*" {
				return e.ifsJoin(strs), vr.IsSet(), nil
			}
			return strings.Join(strs, " "), vr.IsSet(), nil
		}
		val, err := e.assocKey(idx)
		if err != nil {
			return "", false, err
		}
		str, ok := vr.Map[val]
		return str, ok, nil
	}
	return "", false, nil
}

// assignElem assigns a variable via an expansion like ${a=val} or
// ${a[i]=val}, setting a single element when the variable is an array or is
// indexed, like Bash. ${a[i]=val} on a whole scalar converts it to an array.
func (e *expander) assignElem(name string, vr Variable, idx syntax.ArithmExpr, val string) error {
	wenv, ok := e.Env.(WriteEnviron)
	if !ok {
		return fmt.Errorf("environment is read-only")
	}
	arrayWise := false
	switch nodeLit(idx) {
	case "@", "*":
		// ${a[@]=val} assigns the element at index zero.
		arrayWise = true
		idx = nil
	}
	if idx == nil && !arrayWise && vr.Kind != Indexed && vr.Kind != Associative {
		// A plain scalar assignment like ${x=val}.
		return e.envSet(name, val)
	}
	switch vr.Kind {
	case Associative:
		key := "0"
		if idx != nil {
			var err error
			if key, err = e.assocKey(idx); err != nil {
				return err
			}
		}
		vr.Map = maps.Clone(vr.Map)
		if vr.Map == nil {
			vr.Map = make(map[string]string, 1)
		}
		vr.Map[key] = val
	default: // assign a single indexed element
		i := 0
		if idx != nil {
			var err error
			if i, err = e.arithm(idx); err != nil {
				return err
			}
			if i < 0 {
				// Negative indices count from one past the maximum index.
				if i += internal.IndexedMax(vr.List, vr.Indexes) + 1; i < 0 {
					return fmt.Errorf("negative array index")
				}
			}
		}
		list, indexes := slices.Clone(vr.List), slices.Clone(vr.Indexes)
		if vr.Kind == String && vr.IsSet() {
			list, indexes = []string{vr.Str}, nil
		}
		list, indexes = internal.SetIndexedElem(list, indexes, i, val)
		vr.Kind, vr.Str, vr.List, vr.Indexes = Indexed, "", list, indexes
	}
	vr.Set = true
	return wenv.Set(name, vr)
}

func (e *expander) namesByPrefix(prefix string) []string {
	// Later occurrences of a name take priority, as documented by [Environ.Each].
	// Like Bash, only list variables with a value, and do not let a local
	// variable without a value hide an outer variable.
	names := make(map[string]bool)
	for name, vr := range e.Env.Each {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if vr.IsSet() {
			names[name] = true
		} else if !vr.Local {
			delete(names, name)
		}
	}
	return slices.Sorted(maps.Keys(names))
}
