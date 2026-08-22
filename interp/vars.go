// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	cryptorand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	mathrand "math/rand/v2"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/internal"
	"mvdan.cc/sh/v3/syntax"
)

func newOverlayEnviron(parent expand.Environ, background bool) *overlayEnviron {
	oenv := &overlayEnviron{}
	if !background {
		oenv.parent = parent
	} else {
		// We could do better here if the parent is also an overlayEnviron;
		// measure with profiles or benchmarks before we choose to do so.
		for name, vr := range parent.Each {
			oenv.Set(name, vr)
		}
	}
	return oenv
}

// overlayEnviron is our main implementation of [expand.WriteEnviron].
type overlayEnviron struct {
	// parent is non-nil if [values] is an overlay over a parent environment
	// which we can safely reuse without data races, such as non-background subshells
	// or function calls.
	parent expand.Environ

	// values maps normalized variable names, per [overlayEnviron.normalize].
	values map[string]namedVariable

	// We need to know if the current scope is a function's scope, because
	// functions can modify global variables. When true, [parent] must not be nil.
	funcScope bool
}

// namedVariable records the original name of a variable for platforms
// where variable names are matched in a case-insensitive way.
type namedVariable struct {
	// TODO(v4): consider adding this field to [expand.Variable],
	// as a general way for a variable to report its original name.
	// This can be useful for GOOS=windows with case insensitive env vars,
	// as otherwise it's not possible to Environ.Get a var
	// and know what was its original name without looping over Environ.Each.
	Name string
	expand.Variable
}

func (o *overlayEnviron) normalize(name string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(name)
	}
	return name
}

func (o *overlayEnviron) Get(name string) expand.Variable {
	normalized := o.normalize(name)
	if vr, ok := o.values[normalized]; ok {
		return vr.Variable
	}
	if o.parent != nil {
		return o.parent.Get(name)
	}
	return expand.Variable{}
}

func (o *overlayEnviron) Set(name string, vr expand.Variable) error {
	normalized := o.normalize(name)
	prev, inOverlay := o.values[normalized]
	// Manipulation of a global var inside a function.
	if o.funcScope && !vr.Local && !prev.Local {
		// In a function, the parent environment is ours, so it's always read-write.
		return o.parent.(expand.WriteEnviron).Set(name, vr)
	}
	if !inOverlay && o.parent != nil {
		prev.Variable = o.parent.Get(name)
	}

	if o.values == nil {
		o.values = make(map[string]namedVariable)
	}
	if vr.Kind == expand.KeepValue {
		vr.Kind = prev.Kind
		vr.Str = prev.Str
		vr.List = prev.List
		vr.Indexes = prev.Indexes
		vr.Map = prev.Map
	} else if prev.ReadOnly {
		return errReadOnly
	}
	if !vr.IsSet() { // unsetting
		if prev.Local {
			vr.Local = true
			o.values[normalized] = namedVariable{name, vr}
			return nil
		}
		delete(o.values, normalized)
	}
	// modifying the entire variable
	vr.Local = prev.Local || vr.Local
	o.values[normalized] = namedVariable{name, vr}
	return nil
}

func (o *overlayEnviron) Each(f func(name string, vr expand.Variable) bool) {
	if o.parent != nil {
		o.parent.Each(f)
	}
	for _, vr := range o.values {
		if !f(vr.Name, vr.Variable) {
			return
		}
	}
}

func execEnv(env expand.Environ) []string {
	list := make([]string, 0, 64)
	for name, vr := range env.Each {
		if !vr.IsSet() {
			// If a variable is set globally but unset in the
			// runner, we need to ensure it's not part of the final
			// list. Seems like zeroing the element is enough.
			// This is a linear search, but this scenario should be
			// rare, and the number of variables shouldn't be large.
			for i, kv := range list {
				if strings.HasPrefix(kv, name+"=") {
					list[i] = ""
				}
			}
		}
		if vr.Exported && vr.Kind == expand.String {
			list = append(list, name+"="+vr.String())
		}
	}
	return list
}

func (r *Runner) lookupVar(name string) expand.Variable {
	if name == "" {
		panic("variable name must not be empty")
	}
	var vr expand.Variable
	switch name {
	case "#":
		vr.Kind, vr.Str = expand.String, strconv.Itoa(len(r.Params))
	case "@", "*":
		vr.Kind = expand.Indexed
		if r.Params == nil {
			// r.Params may be nil but positional parameters always exist
			vr.List = []string{}
		} else {
			vr.List = r.Params
		}
	case "!":
		if n := len(r.bgProcs); n > 0 {
			vr.Kind, vr.Str = expand.String, "g"+strconv.Itoa(n)
		}
	case "?":
		vr.Kind, vr.Str = expand.String, strconv.Itoa(int(r.lastExit.code))
	case "-":
		// Note that we don't support some of Bash's flags, such as h or B.
		vr.Kind, vr.Str = expand.String, r.posixOptFlags()
	case "$":
		vr.Kind, vr.Str = expand.String, strconv.Itoa(os.Getpid())
	case "PPID":
		vr.Kind, vr.Str = expand.String, strconv.Itoa(os.Getppid())
	case "RANDOM": // not for cryptographic use
		// Without a seed we use the global generator, which is seeded randomly
		// and also uses ChaCha8. Note that [Runner.subshell] does not copy the
		// seeded generator, so subshells start afresh like in bash.
		var n int
		if r.rand != nil {
			n = r.rand.IntN(randomMax)
		} else {
			n = mathrand.IntN(randomMax)
		}
		vr.Kind, vr.Str = expand.String, strconv.Itoa(n)
	case "SRANDOM": // pseudo-random generator from the system
		var p [4]byte
		cryptorand.Read(p[:])
		n := binary.NativeEndian.Uint32(p[:])
		vr.Kind, vr.Str = expand.String, strconv.FormatUint(uint64(n), 10)
	case "DIRSTACK":
		vr.Kind, vr.List = expand.Indexed, r.dirStack
	case "0":
		vr.Kind = expand.String
		if r.filename != "" {
			vr.Str = r.filename
		} else {
			vr.Str = "gosh"
		}
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		if i := int(name[0] - '1'); i < len(r.Params) {
			vr.Kind = expand.String
			vr.Str = r.Params[i]
		}
	}
	if vr.Kind != expand.Unknown {
		vr.Set = true
		return vr
	}
	if vr := r.writeEnv.Get(name); vr.Declared() {
		return vr
	}
	return expand.Variable{}
}

func (r *Runner) envGet(name string) string {
	return r.lookupVar(name).String()
}

func (r *Runner) delVar(name string) {
	if err := r.writeEnv.Set(name, expand.Variable{}); err != nil {
		r.errf("%s: %v\n", name, err)
		r.exit.code = 1
		return
	}
}

func (r *Runner) setVarString(name, value string) {
	r.setVar(name, expand.Variable{Set: true, Kind: expand.String, Str: value})
}

func (r *Runner) setVar(name string, vr expand.Variable) {
	if err := r.setVarErr(name, vr); err != nil {
		r.errf("%v\n", err)
		r.exit.code = 1
	}
}

// errReadOnly is returned when trying to modify a read-only variable.
var errReadOnly = errors.New("readonly variable")

// randomMax is one past the highest value that RANDOM expands to, like in Bash.
const randomMax = 32768

// setVarErr is like [Runner.setVar], but it returns any error rather than
// reporting it, for the sake of the expand package.
func (r *Runner) setVarErr(name string, vr expand.Variable) error {
	if r.opts[optAllExport] {
		vr.Exported = true
	}
	if err := r.writeEnv.Set(name, vr); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if name == "RANDOM" && vr.Kind == expand.String {
		// Assigning to RANDOM seeds its generator; the stored value is never
		// read, as [Runner.lookupVar] always computes a new number. We only
		// seed once the assignment succeeds so that attributes such as
		// read-only still apply. We use ChaCha8 rather than PCG, whose only
		// edge is speed, which is irrelevant next to expanding a variable.
		var seed [32]byte
		binary.LittleEndian.PutUint64(seed[:], uint64(atoi(vr.Str)))
		r.rand = mathrand.New(mathrand.NewChaCha8(seed))
	}
	return nil
}

func (r *Runner) setVarWithIndex(prev expand.Variable, name string, index syntax.ArithmExpr, vr expand.Variable) {
	if vr.Kind == expand.String && index == nil {
		// When assigning a string to an array, fall back to the
		// zero value for the index.
		switch prev.Kind {
		case expand.Indexed:
			index = &syntax.Word{Parts: []syntax.WordPart{
				&syntax.Lit{Value: "0"},
			}}
		case expand.Associative:
			index = &syntax.Word{Parts: []syntax.WordPart{
				&syntax.DblQuoted{},
			}}
		}
	}
	if index == nil {
		r.setVar(name, vr)
		return
	}

	// from the syntax package, we know that value must be a string if index
	// is non-nil; nested arrays are forbidden.
	valStr := vr.Str

	var list []string
	var indexes []int
	switch prev.Kind {
	case expand.String:
		list = append(list, prev.Str)
	case expand.Indexed:
		// TODO: only clone when inside a subshell and getting a var from outside for the first time
		list = slices.Clone(prev.List)
		indexes = slices.Clone(prev.Indexes)
	case expand.Associative:
		// if the existing variable is already an AssocArray, try our
		// best to convert the key to a string
		w, ok := index.(*syntax.Word)
		if !ok {
			return
		}
		k := r.literal(w)

		// TODO: only clone when inside a subshell and getting a var from outside for the first time
		prev.Map = maps.Clone(prev.Map)
		if prev.Map == nil {
			prev.Map = make(map[string]string)
		}
		prev.Map[k] = valStr
		r.setVar(name, prev)
		return
	}
	k := r.arithm(index)
	if k < 0 {
		// Negative indices count from one past the maximum index.
		if k += internal.IndexedMax(list, indexes) + 1; k < 0 {
			r.errf("%s: bad array subscript\n", name)
			r.exit.code = 1
			return
		}
	}
	list, indexes = internal.SetIndexedElem(list, indexes, k, valStr)
	prev.Kind = expand.Indexed
	prev.List = list
	prev.Indexes = indexes
	r.setVar(name, prev)
}

// cutElemSubscript splits an array element argument like `a[3]`, as used by
// the unset builtin, into the array name and the subscript between brackets.
func cutElemSubscript(arg string) (name, sub string, ok bool) {
	i := strings.IndexByte(arg, '[')
	if i > 0 && strings.HasSuffix(arg, "]") && syntax.ValidName(arg[:i]) {
		return arg[:i], arg[i+1 : len(arg)-1], true
	}
	return "", "", false
}

// unsetElem unsets a single element of an indexed or associative array, like
// `unset 'a[3]'`. Unsetting an indexed array element may leave a hole.
func (r *Runner) unsetElem(name, sub string) {
	vr := r.lookupVar(name)
	if n, v := vr.Resolve(r.writeEnv); n != "" {
		name, vr = n, v
	}
	switch vr.Kind {
	case expand.Indexed:
		if sub == "@" || sub == "*" {
			r.delVar(name)
			return
		}
		expr, err := syntax.NewParser().Arithmetic(strings.NewReader(sub))
		if err != nil {
			r.errf("unset: %s[%s]: bad array subscript\n", name, sub)
			r.exit.code = 1
			return
		}
		if expr == nil {
			return // an empty subscript like `unset 'a[]'` is a no-op
		}
		k := r.arithm(expr)
		if k < 0 {
			// Negative indices count from one past the maximum index.
			if k += internal.IndexedMax(vr.List, vr.Indexes) + 1; k < 0 {
				r.errf("unset: %s[%s]: bad array subscript\n", name, sub)
				r.exit.code = 1
				return
			}
		}
		// TODO: only clone when inside a subshell and getting a var from outside for the first time
		vr.List = slices.Clone(vr.List)
		vr.Indexes = slices.Clone(vr.Indexes)
		vr.List, vr.Indexes = internal.DeleteIndexedElem(vr.List, vr.Indexes, k)
		r.setVar(name, vr)
	case expand.Associative:
		if sub == "@" || sub == "*" {
			r.delVar(name)
			return
		}
		// TODO: only clone when inside a subshell and getting a var from outside for the first time
		vr.Map = maps.Clone(vr.Map)
		delete(vr.Map, sub)
		r.setVar(name, vr)
	case expand.String:
		// A scalar can be unset via subscript zero.
		if sub == "0" {
			r.delVar(name)
		} else {
			r.errf("unset: %s: not an array variable\n", name)
			r.exit.code = 1
		}
	}
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

// TODO: make assignVal and [setVar] consistent with the [expand.WriteEnviron] interface

func (r *Runner) assignVal(name string, prev expand.Variable, as *syntax.Assign, valType string) (string, expand.Variable) {
	if n, v := prev.Resolve(r.writeEnv); n != "" {
		name, prev = n, v
	}
	prev.Set = true
	if as.Value != nil {
		s := r.literal(as.Value)
		if !as.Append {
			prev.Kind = expand.String
			if valType == "-n" {
				prev.Kind = expand.NameRef
			}
			prev.Str = s
			return name, prev
		}
		switch prev.Kind {
		case expand.String, expand.Unknown:
			prev.Kind = expand.String
			prev.Str += s
		case expand.Indexed:
			// Appends to the element at index 0, creating it if unset.
			if len(prev.List) > 0 && (prev.Indexes == nil || prev.Indexes[0] == 0) {
				prev.List[0] += s
			} else {
				prev.List, prev.Indexes = internal.SetIndexedElem(prev.List, prev.Indexes, 0, s)
			}
		case expand.Associative:
			// TODO
		}
		return name, prev
	}
	if as.Array == nil {
		// don't return the zero value, as that's an unset variable
		prev.Kind = expand.String
		if valType == "-n" {
			prev.Kind = expand.NameRef
		}
		prev.Str = ""
		return name, prev
	}
	// Array assignment.
	elems := as.Array.Elems
	if valType == "" {
		valType = "-a" // indexed
		if len(elems) > 0 && stringIndex(elems[0].Index) {
			valType = "-A" // associative
		}
	}
	if valType == "-A" {
		amap := make(map[string]string, len(elems))
		for _, elem := range elems {
			k := r.literal(elem.Index.(*syntax.Word))
			amap[k] = r.literal(elem.Value)
		}
		if !as.Append {
			prev.Kind = expand.Associative
			prev.Map = amap
			return name, prev
		}
		// TODO
		return name, prev
	}
	// The base array which the new elements are set on; empty unless
	// we are appending to an existing value.
	var list []string
	var indexes []int
	if as.Append {
		switch prev.Kind {
		case expand.Unknown:
		case expand.String:
			list = []string{prev.Str}
		case expand.Indexed:
			// TODO: only clone when inside a subshell and getting a var from outside for the first time
			list = slices.Clone(prev.List)
			indexes = slices.Clone(prev.Indexes)
		case expand.Associative:
			// TODO
			return name, prev
		default:
			// Should only happen if we forgot a case above.
			panic(fmt.Sprintf("unexpected conversion of kind %d", prev.Kind))
		}
	}
	// Evaluate values for each array element. An explicit index like
	// [5]=x resets our index counter, which otherwise advances for every
	// value, starting after the maximum index of the base array.
	index := internal.IndexedMax(list, indexes) + 1
	for _, elem := range elems {
		if elem.Index != nil {
			// Index resets our index with a literal value.
			index = r.arithm(elem.Index)
			if index < 0 {
				// Negative indices count from one past the maximum index.
				if index += internal.IndexedMax(list, indexes) + 1; index < 0 {
					r.errf("%s: bad array subscript\n", name)
					r.exit.code = 1
					break
				}
			}
			list, indexes = internal.SetIndexedElem(list, indexes, index, r.literal(elem.Value))
			index++
		} else {
			// Implicit index, advancing for every word.
			for _, val := range r.fields(elem.Value) {
				list, indexes = internal.SetIndexedElem(list, indexes, index, val)
				index++
			}
		}
	}
	if list == nil {
		// An empty array like a=() must still expand to zero fields.
		list = []string{}
	}
	prev.Kind = expand.Indexed
	prev.List = list
	prev.Indexes = indexes
	return name, prev
}
