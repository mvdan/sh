// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"mvdan.cc/sh/syntax"
)

type Environ interface {
	Get(name string) Variable
	Set(name string, vr Variable)
	Delete(name string)
	Each(func(name string, vr Variable) bool)
	Sub() Environ
}

type mapEnviron struct {
	parent Environ
	values map[string]Variable
}

func (m *mapEnviron) Get(name string) Variable {
	if vr, ok := m.values[name]; ok {
		return vr
	}
	if m.parent == nil {
		return Variable{}
	}
	return m.parent.Get(name)
}

func (m *mapEnviron) Set(name string, vr Variable) {
	if m.values == nil {
		m.values = make(map[string]Variable)
	}
	m.values[name] = vr
	// TODO: parent too?
}

func (m *mapEnviron) Delete(name string) {
	delete(m.values, name)
	// TODO: parent too?
}

func (m *mapEnviron) Each(f func(name string, vr Variable) bool) {
	for name, vr := range m.values {
		if !f(name, vr) {
			return
		}
	}
	if m.parent != nil {
		m.parent.Each(f)
	}
}

func (m *mapEnviron) Sub() Environ {
	return &mapEnviron{parent: m}
}

func execEnv(env Environ) []string {
	list := make([]string, 32)
	env.Each(func(name string, vr Variable) bool {
		list = append(list, name+"="+vr.Value.String())
		return true
	})
	return list
}

func EnvFromList(list []string) (Environ, error) {
	m := mapEnviron{
		values: make(map[string]Variable, len(list)),
	}
	for _, kv := range list {
		i := strings.IndexByte(kv, '=')
		if i < 0 {
			return nil, fmt.Errorf("env not in the form key=value: %q", kv)
		}
		name, value := kv[:i], kv[i+1:]
		if runtime.GOOS == "windows" {
			name = strings.ToUpper(name)
		}
		m.values[name] = Variable{Value: StringVal(value)}
	}
	return &m, nil
}

type FuncEnviron func(string) string

func (f FuncEnviron) Get(name string) Variable {
	value := f(name)
	if value == "" {
		return Variable{}
	}
	return Variable{Value: StringVal(value)}
}

func (f FuncEnviron) Set(name string, vr Variable)             { panic("FuncEnviron is read-only") }
func (f FuncEnviron) Delete(name string)                       { panic("FuncEnviron is read-only") }
func (f FuncEnviron) Each(func(name string, vr Variable) bool) {}
func (f FuncEnviron) Sub() Environ                             { return f }

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

func (r *Runner) lookupVar(name string) Variable {
	if name == "" {
		panic("variable name must not be empty")
	}
	var value VarValue
	switch name {
	case "#":
		value = StringVal(strconv.Itoa(len(r.Params)))
	case "@", "*":
		value = IndexArray(r.Params)
	case "?":
		value = StringVal(strconv.Itoa(r.exit))
	case "$":
		value = StringVal(strconv.Itoa(os.Getpid()))
	case "PPID":
		value = StringVal(strconv.Itoa(os.Getppid()))
	case "LINENO":
		line := uint64(r.curParam.Pos().Line())
		value = StringVal(strconv.FormatUint(line, 10))
	case "DIRSTACK":
		value = IndexArray(r.dirStack)
	case "0":
		if r.filename != "" {
			value = StringVal(r.filename)
		} else {
			value = StringVal("gosh")
		}
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		i := int(name[0] - '1')
		if i < len(r.Params) {
			value = StringVal(r.Params[i])
		} else {
			value = StringVal("")
		}
	}
	if value != nil {
		return Variable{Value: value}
	}
	if value, e := r.cmdVars[name]; e {
		return Variable{Value: StringVal(value)}
	}
	if vr, e := r.funcVars[name]; e {
		return vr
	}
	if vr, e := r.Vars[name]; e {
		return vr
	}
	if vr := r.Env.Get(name); vr != (Variable{}) {
		return vr
	}
	if runtime.GOOS == "windows" {
		upper := strings.ToUpper(name)
		if vr := r.Env.Get(upper); vr != (Variable{}) {
			return vr
		}
	}
	if r.opts[optNoUnset] {
		r.errf("%s: unbound variable\n", name)
		r.setErr(ShellExitStatus(1))
	}
	return Variable{}
}

func (r *Runner) getVar(name string) string {
	value := r.lookupVar(name)
	return r.varStr(value, 0)
}

func (r *Runner) delVar(name string) {
	value := r.lookupVar(name)
	if value.ReadOnly {
		r.errf("%s: readonly variable\n", name)
		r.exit = 1
		return
	}
	delete(r.Vars, name)
	delete(r.funcVars, name)
	delete(r.cmdVars, name)
	r.Env.Delete(name)
}

// maxNameRefDepth defines the maximum number of times to follow
// references when expanding a variable. Otherwise, simple name
// reference loops could crash the interpreter quite easily.
const maxNameRefDepth = 100

func (r *Runner) varStr(vr Variable, depth int) string {
	if vr.Value == nil || depth > maxNameRefDepth {
		return ""
	}
	if vr.NameRef {
		vr = r.lookupVar(string(vr.Value.(StringVal)))
		return r.varStr(vr, depth+1)
	}
	return vr.Value.String()
}

func (r *Runner) varInd(ctx context.Context, vr Variable, e syntax.ArithmExpr, depth int) string {
	if depth > maxNameRefDepth {
		return ""
	}
	switch x := vr.Value.(type) {
	case StringVal:
		if vr.NameRef {
			vr = r.lookupVar(string(x))
			return r.varInd(ctx, vr, e, depth+1)
		}
		if r.arithm(ctx, e) == 0 {
			return string(x)
		}
	case IndexArray:
		switch anyOfLit(e, "@", "*") {
		case "@":
			return strings.Join(x, " ")
		case "*":
			return strings.Join(x, r.ifsJoin)
		}
		i := r.arithm(ctx, e)
		if len(x) > 0 {
			return x[i]
		}
	case AssocArray:
		if lit := anyOfLit(e, "@", "*"); lit != "" {
			var strs IndexArray
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				strs = append(strs, x[k])
			}
			if lit == "*" {
				return strings.Join(strs, r.ifsJoin)
			}
			return strings.Join(strs, " ")
		}
		return x[r.loneWord(ctx, e.(*syntax.Word))]
	}
	return ""
}

func (r *Runner) setVarString(ctx context.Context, name, value string) {
	r.setVar(ctx, name, nil, Variable{Value: StringVal(value)})
}

func (r *Runner) setVarInternal(name string, vr Variable) {
	if _, ok := vr.Value.(StringVal); ok {
		if r.opts[optAllExport] {
			vr.Exported = true
		}
	} else {
		vr.Exported = false
	}
	if vr.Local {
		if r.funcVars == nil {
			r.funcVars = make(map[string]Variable)
		}
		r.funcVars[name] = vr
	} else {
		r.Vars[name] = vr
	}
	if name == "IFS" {
		r.ifsUpdated()
	}
}

func (r *Runner) setVar(ctx context.Context, name string, index syntax.ArithmExpr, vr Variable) {
	cur := r.lookupVar(name)
	if cur.ReadOnly {
		r.errf("%s: readonly variable\n", name)
		r.exit = 1
		return
	}
	_, isIndexArray := cur.Value.(IndexArray)
	_, isAssocArray := cur.Value.(AssocArray)

	if _, ok := vr.Value.(StringVal); ok && index == nil {
		// When assigning a string to an array, fall back to the
		// zero value for the index.
		if isIndexArray {
			index = &syntax.Word{Parts: []syntax.WordPart{
				&syntax.Lit{Value: "0"},
			}}
		} else if isAssocArray {
			index = &syntax.Word{Parts: []syntax.WordPart{
				&syntax.DblQuoted{},
			}}
		}
	}
	if index == nil {
		r.setVarInternal(name, vr)
		return
	}

	// from the syntax package, we know that value must be a string if index
	// is non-nil; nested arrays are forbidden.
	valStr := string(vr.Value.(StringVal))

	// if the existing variable is already an AssocArray, try our best
	// to convert the key to a string
	if isAssocArray {
		amap := cur.Value.(AssocArray)
		w, ok := index.(*syntax.Word)
		if !ok {
			return
		}
		k := r.loneWord(ctx, w)
		amap[k] = valStr
		cur.Value = amap
		r.setVarInternal(name, cur)
		return
	}
	var list IndexArray
	switch x := cur.Value.(type) {
	case StringVal:
		list = append(list, string(x))
	case IndexArray:
		list = x
	case AssocArray: // done above
	}
	k := r.arithm(ctx, index)
	for len(list) < k+1 {
		list = append(list, "")
	}
	list[k] = valStr
	cur.Value = list
	r.setVarInternal(name, cur)
}

func (r *Runner) setFunc(name string, body *syntax.Stmt) {
	if r.Funcs == nil {
		r.Funcs = make(map[string]*syntax.Stmt, 4)
	}
	r.Funcs[name] = body
}

func stringIndex(index syntax.ArithmExpr) bool {
	w, ok := index.(*syntax.Word)
	if !ok || len(w.Parts) != 1 {
		return false
	}
	switch w.Parts[0].(type) {
	case *syntax.DblQuoted, *syntax.SglQuoted:
		return true
	}
	return false
}

func (r *Runner) assignVal(ctx context.Context, as *syntax.Assign, valType string) VarValue {
	prev := r.lookupVar(as.Name.Value)
	if as.Naked {
		return prev.Value
	}
	if as.Value != nil {
		s := r.loneWord(ctx, as.Value)
		if !as.Append || prev == (Variable{}) {
			return StringVal(s)
		}
		switch x := prev.Value.(type) {
		case StringVal:
			return x + StringVal(s)
		case IndexArray:
			if len(x) == 0 {
				x = append(x, "")
			}
			x[0] += s
			return x
		case AssocArray:
			// TODO
		}
		return StringVal(s)
	}
	if as.Array == nil {
		// don't return nil, as that's an unset variable
		return StringVal("")
	}
	elems := as.Array.Elems
	if valType == "" {
		if len(elems) == 0 || !stringIndex(elems[0].Index) {
			valType = "-a" // indexed
		} else {
			valType = "-A" // associative
		}
	}
	if valType == "-A" {
		// associative array
		amap := AssocArray(make(map[string]string, len(elems)))
		for _, elem := range elems {
			k := r.loneWord(ctx, elem.Index.(*syntax.Word))
			amap[k] = r.loneWord(ctx, elem.Value)
		}
		if !as.Append || prev == (Variable{}) {
			return amap
		}
		// TODO
		return amap
	}
	// indexed array
	maxIndex := len(elems) - 1
	indexes := make([]int, len(elems))
	for i, elem := range elems {
		if elem.Index == nil {
			indexes[i] = i
			continue
		}
		k := r.arithm(ctx, elem.Index)
		indexes[i] = k
		if k > maxIndex {
			maxIndex = k
		}
	}
	strs := make([]string, maxIndex+1)
	for i, elem := range elems {
		strs[indexes[i]] = r.loneWord(ctx, elem.Value)
	}
	if !as.Append || prev == (Variable{}) {
		return IndexArray(strs)
	}
	switch x := prev.Value.(type) {
	case StringVal:
		prevList := IndexArray([]string{string(x)})
		return append(prevList, strs...)
	case IndexArray:
		return append(x, strs...)
	case AssocArray:
		// TODO
	}
	return IndexArray(strs)
}

func (r *Runner) ifsUpdated() {
	runes := r.getVar("IFS")
	r.ifsJoin = ""
	if len(runes) > 0 {
		r.ifsJoin = runes[:1]
	}
	r.ifsRune = func(r rune) bool {
		for _, r2 := range runes {
			if r == r2 {
				return true
			}
		}
		return false
	}
}

func (r *Runner) namesByPrefix(prefix string) []string {
	var names []string
	r.Env.Each(func(name string, vr Variable) bool {
		if strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
		return true
	})
	for name := range r.Vars {
		if strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	return names
}
