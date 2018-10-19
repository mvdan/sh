// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"

	"mvdan.cc/sh/expand"
	"mvdan.cc/sh/syntax"
)

type overlayEnviron struct {
	// TODO: drop the pointer from the receivers
	parent expand.Environ
	values map[string]expand.Variable
}

func (o *overlayEnviron) Get(name string) expand.Variable {
	if vr, ok := o.values[name]; ok {
		return vr
	}
	if o.parent == nil {
		return expand.Variable{}
	}
	return o.parent.Get(name)
}

func (o *overlayEnviron) Set(name string, vr expand.Variable) {
	if o.values == nil {
		o.values = make(map[string]expand.Variable)
	}
	o.values[name] = vr
	// TODO: parent too?
}

func (o *overlayEnviron) Delete(name string) {
	delete(o.values, name)
	// TODO: parent too?
}

func (o *overlayEnviron) Each(f func(name string, vr expand.Variable) bool) {
	for name, vr := range o.values {
		if !f(name, vr) {
			return
		}
	}
	if o.parent != nil {

		o.parent.Each(f)
	}
}

func execEnv(env expand.Environ) []string {
	list := make([]string, 32)
	env.Each(func(name string, vr expand.Variable) bool {
		list = append(list, name+"="+vr.String())
		return true
	})
	return list
}

func (r *Runner) lookupVar(name string) expand.Variable {
	if name == "" {
		panic("variable name must not be empty")
	}
	var value interface{}
	switch name {
	case "#":
		value = strconv.Itoa(len(r.Params))
	case "@", "*":
		value = r.Params
	case "?":
		value = strconv.Itoa(r.exit)
	case "$":
		value = strconv.Itoa(os.Getpid())
	case "PPID":
		value = strconv.Itoa(os.Getppid())
	case "DIRSTACK":
		value = r.dirStack
	case "0":
		if r.filename != "" {
			value = r.filename
		} else {
			value = "gosh"
		}
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		i := int(name[0] - '1')
		if i < len(r.Params) {
			value = r.Params[i]
		} else {
			value = ""
		}
	}
	if value != nil {
		return expand.Variable{Value: value}
	}
	if value, e := r.cmdVars[name]; e {
		return expand.Variable{Value: value}
	}
	if vr, e := r.funcVars[name]; e {
		return vr
	}
	if vr, e := r.Vars[name]; e {
		return vr
	}
	if vr := r.Env.Get(name); vr != (expand.Variable{}) {
		return vr
	}
	if runtime.GOOS == "windows" {
		upper := strings.ToUpper(name)
		if vr := r.Env.Get(upper); vr != (expand.Variable{}) {
			return vr
		}
	}
	if r.opts[optNoUnset] {
		r.errf("%s: unbound variable\n", name)
		r.setErr(ShellExitStatus(1))
	}
	return expand.Variable{}
}

func (r *Runner) envGet(name string) string {
	return r.lookupVar(name).String()
}

func (r *Runner) delVar(name string) {
	value := r.lookupVar(name)
	if value.ReadOnly {
		r.errf("%s: readonly variable\n", name)
		r.exit = 1
		return
	}
	r.Vars[name] = expand.Variable{} // to not query r.Env
	delete(r.funcVars, name)
	delete(r.cmdVars, name)
}

func (r *Runner) setVarString(ctx context.Context, name, value string) {
	r.setVar(ctx, name, nil, expand.Variable{Value: value})
}

func (r *Runner) setVarInternal(name string, vr expand.Variable) {
	if _, ok := vr.Value.(string); ok {
		if r.opts[optAllExport] {
			vr.Exported = true
		}
	} else {
		vr.Exported = false
	}
	if vr.Local {
		if r.funcVars == nil {
			r.funcVars = make(map[string]expand.Variable)
		}
		r.funcVars[name] = vr
	} else {
		r.Vars[name] = vr
	}
}

func (r *Runner) setVar(ctx context.Context, name string, index syntax.ArithmExpr, vr expand.Variable) {
	cur := r.lookupVar(name)
	if cur.ReadOnly {
		r.errf("%s: readonly variable\n", name)
		r.exit = 1
		return
	}
	if name2, var2 := cur.Resolve(r.Env); name2 != "" {
		name = name2
		cur = var2
		vr.NameRef = false
		cur.NameRef = false
	}
	_, isIndexArray := cur.Value.([]string)
	_, isAssocArray := cur.Value.(map[string]string)

	if _, ok := vr.Value.(string); ok && index == nil {
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
	valStr := vr.Value.(string)

	// if the existing variable is already an AssocArray, try our best
	// to convert the key to a string
	if isAssocArray {
		amap := cur.Value.(map[string]string)
		w, ok := index.(*syntax.Word)
		if !ok {
			return
		}
		k := r.ExpandLiteral(ctx, w)
		amap[k] = valStr
		cur.Value = amap
		r.setVarInternal(name, cur)
		return
	}
	var list []string
	switch x := cur.Value.(type) {
	case string:
		list = append(list, x)
	case []string:
		list = x
	case map[string]string: // done above
	}
	k := r.ExpandArithm(ctx, index)
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

func (r *Runner) assignVal(ctx context.Context, as *syntax.Assign, valType string) interface{} {
	prev := r.lookupVar(as.Name.Value)
	if as.Naked {
		return prev.Value
	}
	if as.Value != nil {
		s := r.ExpandLiteral(ctx, as.Value)
		if !as.Append || prev == (expand.Variable{}) {
			return s
		}
		switch x := prev.Value.(type) {
		case string:
			return x + s
		case []string:
			if len(x) == 0 {
				x = append(x, "")
			}
			x[0] += s
			return x
		case map[string]string:
			// TODO
		}
		return s
	}
	if as.Array == nil {
		// don't return nil, as that's an unset variable
		return ""
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
		amap := make(map[string]string, len(elems))
		for _, elem := range elems {
			k := r.ExpandLiteral(ctx, elem.Index.(*syntax.Word))
			amap[k] = r.ExpandLiteral(ctx, elem.Value)
		}
		if !as.Append || prev == (expand.Variable{}) {
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
		k := r.ExpandArithm(ctx, elem.Index)
		indexes[i] = k
		if k > maxIndex {
			maxIndex = k
		}
	}
	strs := make([]string, maxIndex+1)
	for i, elem := range elems {
		strs[indexes[i]] = r.ExpandLiteral(ctx, elem.Value)
	}
	if !as.Append || prev == (expand.Variable{}) {
		return strs
	}
	switch x := prev.Value.(type) {
	case string:
		return append([]string{x}, strs...)
	case []string:
		return append(x, strs...)
	case map[string]string:
		// TODO
	}
	return strs
}
