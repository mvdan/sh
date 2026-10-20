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

// listIndex reports whether an index is "@" or "*", expanding a list.
func listIndex(index syntax.ArithmExpr) bool {
	switch nodeLit(index) {
	case "@", "*":
		return true
	}
	return false
}

// mayExpandList reports whether an expansion may produce a list, such as
// $@, ${a[*]}, or an indirect expansion like ${!n} which refers to one.
func mayExpandList(pe *syntax.ParamExp) bool {
	if pe.Param == nil {
		return false
	}
	switch pe.Param.Value {
	case "@", "*":
		return true
	}
	return pe.Excl || listIndex(pe.Index)
}

// joinElems joins the elements of a list expansion: with the first IFS
// character for the "*" forms, and with a space otherwise.
func (e *expander) joinElems(elems []string, star bool) (string, error) {
	if star {
		return e.ifsJoin(elems)
	}
	return joinLimited(elems, " ")
}

// joinsNull reports whether [expander.joinElems] would give an empty string.
func (e *expander) joinsNull(elems []string, star bool) bool {
	if len(elems) > 1 && (!star || e.ifs != "") {
		return false // joined by a separator
	}
	return !slices.ContainsFunc(elems, func(elem string) bool { return elem != "" })
}

// testResult is the outcome of an operator like ${x-y}; see [testParamOp].
type testResult uint8

const (
	testKeep testResult = iota // the parameter's own value is used
	testArg                    // the operator's word is used instead
	testFail                   // the expansion fails with the operator's word
)

// testParamOp applies one of the operators which test whether a parameter
// is set or null, such as ${x-y}, ${x:+y}, or ${x:?y}.
func testParamOp(op syntax.ParExpOperator, set, null bool) testResult {
	switch op {
	case syntax.AlternateUnset:
		if set {
			return testArg
		}
	case syntax.AlternateUnsetOrNull:
		if !null {
			return testArg
		}
	case syntax.DefaultUnset:
		if !set {
			return testArg
		}
	case syntax.DefaultUnsetOrNull:
		if null {
			return testArg
		}
	case syntax.ErrorUnset:
		if !set {
			return testFail
		}
	case syntax.ErrorUnsetOrNull:
		if null {
			return testFail
		}
	}
	return testKeep
}

// param describes the parameter which an expansion refers to.
type param struct {
	name  string
	index syntax.ArithmExpr // [i], or a "@" or "*" word for $@ and $*
	vr    Variable          // with any nameref followed
	orig  Variable          // before following any nameref, for ${var@a}
}

// indirectExp reports whether an expansion like ${!name} refers to the
// parameter named by the value of name. ${!prefix*} lists names and
// ${!name[@]} lists keys instead; see [namesExp].
func indirectExp(pe *syntax.ParamExp) bool {
	return pe.Excl && pe.Names == 0 && !listIndex(pe.Index)
}

// namesExp reports whether an expansion lists names like ${!prefix*}, or
// keys like ${!name[@]}.
func namesExp(pe *syntax.ParamExp) bool {
	return pe.Excl && (pe.Names != 0 || listIndex(pe.Index))
}

// resolveParam finds the parameter which an expansion refers to. For an
// indirect expansion like ${!n}, that is the parameter named by the value
// of n, which may be a list like @ or a subscripted name like a[1], so
// that any operators apply to that parameter.
func (e *expander) resolveParam(pe *syntax.ParamExp) (param, error) {
	if pe.Param == nil || len(pe.Subscripts) > 0 { // e.g. zsh's ${} or ${a[1][2]}
		return param{}, fmt.Errorf("unsupported")
	}
	p := e.lookupParam(pe, pe.Param.Value, pe.Index)
	if !indirectExp(pe) {
		return p, nil
	}
	if p.orig.Kind == NameRef {
		// ${!ref} on a nameref expands to the name it refers to.
		p.vr = Variable{Set: true, Kind: String, Str: p.orig.Str}
		p.index = nil
		return p, nil
	}
	str, set, err := e.varInd(p.vr, p.index)
	if err != nil {
		return p, err
	}
	if !set || str == "" {
		if specialParam(pe.Param.Value) {
			// Like Bash, ${!@} or ${!1} with no parameters expand to nothing.
			return param{}, nil
		}
		if !set {
			return p, internal.Fatalf("%s: invalid indirect expansion", pe.Param.Value)
		}
	}
	name, sub := str, ""
	if i := strings.IndexByte(str, '['); i > 0 && strings.HasSuffix(str, "]") {
		name, sub = str[:i], str[i+1:len(str)-1]
	}
	if !syntax.ValidName(name) && !specialParam(name) {
		return p, internal.Fatalf("%s: invalid variable name", str)
	}
	var index syntax.ArithmExpr
	switch {
	case sub == "":
	case sub == "@":
		index = indexAt
	case sub == "*":
		index = indexStar
	default:
		if _, vr := e.Env.Get(name).Resolve(e.Env); vr.Kind == Associative {
			// The key of an associative array is taken as-is.
			index = &syntax.Word{Parts: []syntax.WordPart{&syntax.SglQuoted{Value: sub}}}
			break
		}
		index, err = e.parseArithm(sub)
		if err != nil {
			return p, internal.Fatalf("%s: bad array subscript", str)
		}
	}
	return e.lookupParam(pe, name, index), nil
}

// specialParam reports whether name is a positional or special parameter,
// such as 3, @, or ?.
func specialParam(name string) bool {
	if name == "" {
		return false
	}
	if strings.Trim(name, "0123456789") == "" {
		return true
	}
	return len(name) == 1 && strings.ContainsRune("@*#?-$!", rune(name[0]))
}

// The index words which $@ and $* behave like, for [expander.lookupParam].
var (
	indexAt   = &syntax.Word{Parts: []syntax.WordPart{&syntax.Lit{Value: "@"}}}
	indexStar = &syntax.Word{Parts: []syntax.WordPart{&syntax.Lit{Value: "*"}}}
)

// lookupParam implements [expander.resolveParam] for a parameter name.
func (e *expander) lookupParam(pe *syntax.ParamExp, name string, index syntax.ArithmExpr) param {
	p := param{name: name, index: index}
	switch name {
	case "":
		return p
	case "@":
		p.index = indexAt
	case "*":
		p.index = indexStar
	case "LINENO":
		// This is the only parameter expansion that the environment
		// interface cannot satisfy.
		line := uint64(pe.Pos().Line())
		p.vr = Variable{Set: true, Kind: String, Str: strconv.FormatUint(line, 10)}
	}
	if p.vr.Kind == Unknown {
		p.vr = e.Env.Get(name)
	}
	p.orig = p.vr
	if n, v := p.vr.Resolve(e.Env); n != "" {
		p.name, p.vr = n, v
	}
	return p
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

// operandWord reports whether an expansion like ${x-word} or ${x:+word}
// results in the operator's word rather than the parameter's value,
// returning the word, which may be nil. A list like ${a[@]} is tested as a
// whole: it is set when it has any elements, and null when they join into
// an empty string. An element like ${a[i]} is replaced in p by its value,
// so that its subscript is not evaluated again.
func (e *expander) operandWord(pe *syntax.ParamExp, p *param) (*syntax.Word, bool, error) {
	if pe.Exp == nil || pe.Length || pe.Width || pe.IsSet || namesExp(pe) {
		return nil, false, nil
	}
	switch pe.Exp.Op {
	case syntax.AlternateUnset, syntax.AlternateUnsetOrNull,
		syntax.DefaultUnset, syntax.DefaultUnsetOrNull:
	default:
		return nil, false, nil
	}
	var set, null bool
	if elems, star, ok := e.listElems(pe, *p); ok {
		set = len(elems) > 0
		// Only the operators with a colon test for null.
		if op := pe.Exp.Op; op == syntax.AlternateUnsetOrNull || op == syntax.DefaultUnsetOrNull {
			null = e.joinsNull(elems, star)
		}
	} else {
		str, isSet, err := e.varInd(p.vr, p.index)
		if err != nil {
			return nil, false, err
		}
		set, null = isSet, str == ""
		p.vr, p.index = Variable{Set: isSet, Kind: String, Str: str}, nil
	}
	if testParamOp(pe.Exp.Op, set, null) != testArg {
		return nil, false, nil
	}
	return pe.Exp.Word, true, nil
}

// paramExp expands a parameter expansion, where quoted reports whether it is
// within double quotes or a here-document; see [expander.expArg].
func (e *expander) paramExp(pe *syntax.ParamExp, quoted bool) (string, error) {
	p, err := e.resolveParam(pe)
	if err != nil {
		return "", err
	}
	return e.paramExpResolved(pe, p, quoted)
}

// paramExpResolved implements [expander.paramExp] once the parameter has
// been resolved, so that a caller which already resolved it, such as
// [expander.wordFields], does not evaluate any subscript again.
func (e *expander) paramExpResolved(pe *syntax.ParamExp, p param, quoted bool) (string, error) {
	var err error
	name, index, vr, orig := p.name, p.index, p.vr, p.orig
	// "*" expansions like ${*}, ${arr[*]}, or ${!prefix*} join their
	// elements with the first IFS character; others use a space.
	star := nodeLit(index) == "*" || pe.Names == syntax.NamesPrefix
	join := func(elems []string) (string, error) { return e.joinElems(elems, star) }
	if e.NoUnset && !vr.IsSet() && !overridingUnset(pe) {
		return "", UnsetParameterError{
			Node:    pe,
			Message: "unbound variable",
		}
	}

	var sliceOffset, sliceLen int
	if pe.Slice != nil && vr.IsSet() {
		// Like Bash, the offset and length of an unset variable are
		// not evaluated.
		if pe.Slice.Offset != nil {
			sliceOffset, err = e.arithmExp(pe.Slice.Offset)
			if err != nil {
				return "", err
			}
		}
		if pe.Slice.Length != nil {
			sliceLen, err = e.arithmExp(pe.Slice.Length)
			if err != nil {
				return "", err
			}
		}
	}

	var str string
	var set bool
	elems, _, isList := e.listElems(pe, p)
	if isList {
		// Like Bash, a list is set when it has any elements,
		// so that ${a[@]-x} works on an empty array.
		set = len(elems) > 0
	} else {
		str, set, err = e.varInd(vr, index)
		if err != nil {
			return "", err
		}
		if !listIndex(index) || vr.Kind != Unknown {
			// Other than an unset "${x[@]}", which is an empty list,
			// the single value is the only element.
			elems = []string{str}
		}
	}

	switch {
	case pe.Length:
		n := len(elems)
		if !listIndex(index) {
			n = utf8.RuneCountInString(str)
		}
		return strconv.Itoa(n), nil
	case namesExp(pe):
		var strs []string
		switch {
		case pe.Names != 0:
			strs = e.namesByPrefix(pe.Param.Value)
		case vr.Kind == Indexed:
			strs = vr.indexedKeys()
		case vr.Kind == Associative:
			strs = vr.assocKeys()
		case vr.IsSet():
			strs = []string{"0"}
		}
		return join(strs)
	case pe.Width:
		return "", fmt.Errorf("unsupported")
	case pe.IsSet:
		return "", fmt.Errorf("unsupported")
	case pe.Slice != nil:
		if !isList {
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
			return string(rs), nil
		} // else, elems are already sliced
	case pe.Repl != nil:
		elems, err := e.replaceElems(pe.Repl, elems)
		if err != nil {
			return "", err
		}
		return join(elems)
	case pe.Exp != nil:
		arg, err := e.expArg(pe.Exp, quoted)
		if err != nil {
			return "", err
		}
		switch op := pe.Exp.Op; op {
		case syntax.AlternateUnset, syntax.AlternateUnsetOrNull,
			syntax.DefaultUnset, syntax.DefaultUnsetOrNull,
			syntax.ErrorUnset, syntax.ErrorUnsetOrNull:
			switch testParamOp(op, set, e.joinsNull(elems, star)) {
			case testArg:
				return arg, nil
			case testFail:
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
			if e.joinsNull(elems, star) {
				if err := e.assignElem(name, vr, index, arg); err != nil {
					return "", err
				}
				return arg, nil
			}
		case syntax.RemSmallPrefix, syntax.RemLargePrefix,
			syntax.RemSmallSuffix, syntax.RemLargeSuffix:
			return join(e.removePatternElems(op, arg, elems))
		case syntax.UpperFirst, syntax.UpperAll,
			syntax.LowerFirst, syntax.LowerAll:
			if elems, err = e.caseConvElems(op, arg, elems); err != nil {
				return "", err
			}
			return join(elems)
		case syntax.OtherParamOps:
			if isList {
				if str, err = join(elems); err != nil {
					return "", err
				}
			}
			if s, ok, err := transformOp(arg, str); err != nil {
				return "", err
			} else if ok {
				return s, nil
			}
			switch arg {
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
			case "K", "k":
				// TODO: implement, like @A but listing keys for assoc arrays.
			case "#": // mksh's hash of the value
				return "", fmt.Errorf("unsupported")
			default:
				panic(fmt.Sprintf("unexpected @%s param expansion", arg))
			}
			return str, nil
		}
	}
	if isList {
		// Only join the elements when the result uses them as a whole,
		// as they may be too large.
		return join(elems)
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
// each element, given the expanded operand of pe.Exp if any, leaving them
// unchanged for any whole-expansion operator.
func (e *expander) perElemOps(pe *syntax.ParamExp, arg string, elems []string) ([]string, error) {
	switch {
	case pe.Repl != nil:
		return e.replaceElems(pe.Repl, elems)
	case pe.Exp != nil:
		switch op := pe.Exp.Op; op {
		case syntax.RemSmallPrefix, syntax.RemLargePrefix,
			syntax.RemSmallSuffix, syntax.RemLargeSuffix:
			return e.removePatternElems(op, arg, elems), nil
		case syntax.UpperFirst, syntax.UpperAll,
			syntax.LowerFirst, syntax.LowerAll:
			return e.caseConvElems(op, arg, elems)
		case syntax.OtherParamOps:
			// The caller handles the operators which do not transform.
			return mapElems(elems, func(elem string) (string, error) {
				s, _, err := transformOp(arg, elem)
				return s, err
			})
		}
	}
	return elems, nil
}

// transformOp applies a ${var@op} operator which transforms the value,
// such as Q or U. ok is false for the operators which describe the
// variable instead, such as a or A.
func transformOp(op, str string) (_ string, ok bool, _ error) {
	switch op {
	case "Q":
		quoted, err := syntax.Quote(str, syntax.LangBash)
		return quoted, true, err
	case "E":
		tail := str
		var rns []rune
		for tail != "" {
			var rn rune
			rn, _, tail, _ = strconv.UnquoteChar(tail, 0)
			rns = append(rns, rn)
		}
		return string(rns), true, nil
	case "P":
		// TODO: implement prompt expansion (\u, \h, \w, etc.).
		return str, true, nil
	case "U":
		return strings.ToUpper(str), true, nil
	case "u":
		rs := []rune(str)
		if len(rs) > 0 {
			rs[0] = unicode.ToUpper(rs[0])
		}
		return string(rs), true, nil
	case "L":
		return strings.ToLower(str), true, nil
	}
	return str, false, nil
}

// expArg expands the operand of a parameter expansion operator, such as
// the word in ${x:-word} or the pattern in ${x#pattern}. Like in Bash, when
// quoted reports that the expansion is within double quotes or a
// here-document, an operand word follows the rules of double quotes, so
// that only a backslash before a special character is removed. A pattern
// keeps its quoted characters escaped.
func (e *expander) expArg(exp *syntax.Expansion, quoted bool) (string, error) {
	switch exp.Op {
	case syntax.RemSmallPrefix, syntax.RemLargePrefix,
		syntax.RemSmallSuffix, syntax.RemLargeSuffix,
		syntax.UpperFirst, syntax.UpperAll,
		syntax.LowerFirst, syntax.LowerAll:
		return e.pattern(exp.Word)
	}
	if !quoted || exp.Word == nil {
		return e.literal(exp.Word)
	}
	field, err := e.wordField(exp.Word.Parts, quoteDouble)
	if err != nil {
		return "", err
	}
	return e.fieldJoin(field)
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
	rx := compilePattern(orig)
	if rx == nil {
		return elems, nil
	}
	// Stop replacing once the replacements alone would not fit,
	// before mapElems checks the results in total.
	left := internal.MaxExpandSize
	return mapElems(elems, func(elem string) (string, error) {
		if !repl.All {
			if loc := rx.FindStringIndex(elem); loc != nil {
				elem = elem[:loc[0]] + with + elem[loc[1]:]
			}
			return elem, nil
		}
		elem = rx.ReplaceAllStringFunc(elem, func(string) string {
			if len(with) > left {
				left = -1 // stop for good
				return ""
			}
			left -= len(with)
			return with
		})
		if left < 0 {
			return "", errTooLarge
		}
		return elem, nil
	})
}

// removePatternElems applies a pattern removal operator to each element.
// Unlike [mapElems], it needs no size limit, as each result is a substring.
func (e *expander) removePatternElems(op syntax.ParExpOperator, arg string, elems []string) []string {
	suffix := op == syntax.RemSmallSuffix || op == syntax.RemLargeSuffix
	small := op == syntax.RemSmallPrefix || op == syntax.RemSmallSuffix
	out := make([]string, len(elems))
	for i, elem := range elems {
		out[i] = removePattern(elem, arg, suffix, small)
	}
	return out
}

// mapElems applies fn to each element, failing once the results would
// exceed [internal.MaxExpandSize] in total, as the elements may all share
// one large string which fn copies.
func mapElems(elems []string, fn func(string) (string, error)) ([]string, error) {
	out := make([]string, len(elems))
	size := 0
	for i, elem := range elems {
		s, err := fn(elem)
		if err != nil {
			return nil, err
		}
		if size += len(s); size > internal.MaxExpandSize {
			return nil, errTooLarge
		}
		out[i] = s
	}
	return out, nil
}

// caseConvElems applies a case conversion operator to each element.
func (e *expander) caseConvElems(op syntax.ParExpOperator, arg string, elems []string) ([]string, error) {
	caseFunc := unicode.ToLower
	if op == syntax.UpperFirst || op == syntax.UpperAll {
		caseFunc = unicode.ToUpper
	}
	all := op == syntax.UpperAll || op == syntax.LowerAll

	// empty string means '?'; nothing to do there
	rx := compilePattern(arg)
	if rx == nil {
		return elems, nil
	}
	return mapElems(elems, func(elem string) (string, error) {
		rs := []rune(elem)
		for ri, r := range rs {
			if rx.MatchString(string(r)) {
				rs[ri] = caseFunc(r)
			}
			if !all {
				break // only the first character is considered
			}
		}
		return string(rs), nil
	})
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
		if listIndex(idx) {
			return vr.Str, vr.IsSet(), nil
		}
		n, err := e.subscript(idx)
		if err != nil {
			return "", false, err
		}
		if n == 0 {
			return vr.Str, vr.IsSet(), nil
		}
	case Indexed:
		if listIndex(idx) {
			return strings.Join(vr.List, " "), vr.IsSet(), nil
		}
		i, err := e.subscript(idx)
		if err != nil {
			return "", false, err
		}
		i, ok := internal.IndexedAbs(vr.List, vr.Indexes, i)
		if !ok {
			return "", false, fmt.Errorf("negative array index")
		}
		if str, ok := vr.indexedVal(i); ok {
			return str, true, nil
		}
	case Associative:
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
	if specialParam(name) {
		return internal.Fatalf("$%s: cannot assign in this way", name)
	}
	wenv, ok := e.Env.(WriteEnviron)
	if !ok {
		return fmt.Errorf("environment is read-only")
	}
	arrayWise := listIndex(idx)
	if arrayWise {
		// ${a[@]=val} assigns the element at index zero.
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
			if i, err = e.subscript(idx); err != nil {
				return err
			}
			var ok bool
			if i, ok = internal.IndexedAbs(vr.List, vr.Indexes, i); !ok {
				return fmt.Errorf("negative array index")
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
