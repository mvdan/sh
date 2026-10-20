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
	"mvdan.cc/sh/v3/internal/arithm"
	"mvdan.cc/sh/v3/syntax"
)

// overlayEnviron is our main implementation of [expand.WriteEnviron].
//
// A runner's variables live in a chain of scopes: the global scope at the
// root, over which each function call, temporary environment of prefix
// assignments, and subshell adds a scope of its own. Reads walk the chain,
// and writes land in the scope which owns a variable, so that a function
// modifies its caller's local variables like in Bash.
type overlayEnviron struct {
	// parent is the enclosing scope, which is [Runner.Env] for the root.
	parent expand.Environ

	// values maps normalized variable names, per [overlayEnviron.normalize].
	values map[string]namedVariable

	// shared is set when values is shared with the scope of a parent shell,
	// so that it is copied before it is first modified; see [overlayEnviron.share].
	shared bool

	// sharedParent is set when parent is a scope of a parent shell, so that
	// it is copied before it or any scope beyond it is modified; see
	// [overlayEnviron.up].
	sharedParent bool

	kind scopeKind
}

type scopeKind uint8

const (
	// scopeGlobal holds global variables in the root scope. A subshell
	// clones the chain, so that it can modify any scope without affecting
	// the parent shell.
	scopeGlobal scopeKind = iota
	// scopeFunc holds the local variables of a function call.
	scopeFunc
	// scopeTemp holds the prefix assignments of a command, like `x=1 cmd`.
	scopeTemp
)

// namedVariable records the original name of a variable for platforms
// where variable names are matched in a case-insensitive way.
type namedVariable struct {
	// TODO(v4): remove along with normalize, as variables should be
	// case sensitive on Windows too; otherwise `for path in *` overwrites
	// PATH. See doc/plan-v4.md.
	Name string
	expand.Variable
}

func (o *overlayEnviron) normalize(name string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(name)
	}
	return name
}

// clone copies a chain of scopes for a background subshell, which runs
// alongside the parent shell, including the variables in [Runner.Env] into
// the root scope. Other subshells use [overlayEnviron.share] instead.
func (o *overlayEnviron) clone() *overlayEnviron {
	c := &overlayEnviron{kind: o.kind, values: maps.Clone(o.values)}
	switch parent := o.parent.(type) {
	case *overlayEnviron:
		c.parent = parent.clone()
	case nil:
	default:
		if c.values == nil {
			c.values = make(map[string]namedVariable)
		}
		for name, vr := range parent.Each {
			normalized := o.normalize(name)
			if _, ok := o.values[normalized]; !ok {
				c.values[normalized] = namedVariable{name, vr}
			}
		}
	}
	return c
}

// share returns a copy of this scope which shares its values and its parent
// scopes, so that a subshell copies each of them only when modifying it.
func (o *overlayEnviron) share() *overlayEnviron {
	return &overlayEnviron{
		parent:       o.parent,
		values:       o.values,
		shared:       true,
		sharedParent: true,
		kind:         o.kind,
	}
}

// up returns the parent scope to be modified, first copying it if it is
// shared with a parent shell.
func (o *overlayEnviron) up() *overlayEnviron {
	p := o.parent.(*overlayEnviron)
	if o.sharedParent {
		p = p.share()
		o.parent, o.sharedParent = p, false
	}
	return p
}

// unshare copies values if they are shared with a parent shell.
func (o *overlayEnviron) unshare() {
	if o.shared {
		o.values = maps.Clone(o.values)
		o.shared = false
	}
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

// own returns the variable held by this scope, if any.
// A nil scope holds no variables.
func (o *overlayEnviron) own(name string) (expand.Variable, bool) {
	if o == nil {
		return expand.Variable{}, false
	}
	vr, ok := o.values[o.normalize(name)]
	return vr.Variable, ok
}

// owner returns the scope which a plain assignment or unset of name applies
// to: the innermost scope holding it, or else the innermost global scope.
func (o *overlayEnviron) owner(name string) *overlayEnviron {
	normalized := o.normalize(name)
	for {
		if _, ok := o.values[normalized]; ok || o.kind == scopeGlobal {
			return o
		}
		o = o.up()
	}
}

// globalScope returns the innermost global scope.
func (o *overlayEnviron) globalScope() *overlayEnviron {
	for o.kind != scopeGlobal {
		o = o.up()
	}
	return o
}

// funcScope returns the scope of the current function call, or nil.
func (o *overlayEnviron) funcScope() *overlayEnviron {
	for {
		switch o.kind {
		case scopeFunc:
			return o
		case scopeGlobal:
			return nil
		}
		o = o.parent.(*overlayEnviron)
	}
}

// Set sets a variable like a plain assignment does, in the scope which owns
// it; see [overlayEnviron.owner].
func (o *overlayEnviron) Set(name string, vr expand.Variable) error {
	return o.owner(name).set(name, vr)
}

// setLocal declares a variable in the current scope, like the local builtin.
// Like in Bash, temporary scopes are skipped unless they hold the variable,
// so that `x=1 eval 'local y=2'` declares y beyond the eval command,
// whereas `x=1 eval 'local x=2'` only modifies the temporary x.
func (o *overlayEnviron) setLocal(name string, vr expand.Variable) error {
	vr.Local = true
	return o.localScope(name).set(name, vr)
}

// localScope returns the innermost temporary scope holding name, or else
// the innermost scope which is not temporary; see [overlayEnviron.setLocal].
func (o *overlayEnviron) localScope(name string) *overlayEnviron {
	normalized := o.normalize(name)
	for o.kind == scopeTemp {
		if _, ok := o.values[normalized]; ok {
			break
		}
		o = o.up()
	}
	return o
}

// setGlobal sets a variable in the global scope, like `declare -g`.
func (o *overlayEnviron) setGlobal(name string, vr expand.Variable) error {
	vr.Local = false
	return o.globalScope().set(name, vr)
}

// set writes a variable into this scope, where an unset variable hides any
// outer variable of the same name.
func (o *overlayEnviron) set(name string, vr expand.Variable) error {
	normalized := o.normalize(name)
	prev, inScope := o.values[normalized]
	if !inScope && o.parent != nil {
		prev.Variable = o.parent.Get(name)
	}
	if vr.Kind == expand.KeepValue {
		vr.Set = prev.Set
		vr.Kind = prev.Kind
		vr.Str = prev.Str
		vr.List = prev.List
		vr.Indexes = prev.Indexes
		vr.Map = prev.Map
	} else if prev.ReadOnly {
		return fmt.Errorf("%s: %w", name, errReadOnly)
	}
	o.unshare()
	if o.values == nil {
		o.values = make(map[string]namedVariable)
	}
	vr.Local = prev.Local || vr.Local
	o.values[normalized] = namedVariable{name, vr}
	return nil
}

// unset unsets a variable like the unset builtin. Like in Bash, a local
// variable unset within its own function stays declared and hides any outer
// variable, whereas one unset from a called function is removed entirely,
// revealing the outer variable.
func (o *overlayEnviron) unset(name string) error {
	scope := o.owner(name)
	if scope.kind != scopeGlobal && scope != o.funcScope() {
		if scope.Get(name).ReadOnly {
			return fmt.Errorf("%s: %w", name, errReadOnly)
		}
		scope.unshare()
		delete(scope.values, scope.normalize(name))
		return nil
	}
	return scope.set(name, expand.Variable{})
}

// localInFunc reports whether name is a local variable of the current function,
// including when it was declared outside of the current subshell.
func (o *overlayEnviron) localInFunc(name string) bool {
	normalized := o.normalize(name)
	for {
		if vr, ok := o.values[normalized]; ok {
			return vr.Local
		}
		if o.kind != scopeTemp {
			return false
		}
		o = o.parent.(*overlayEnviron)
	}
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

// execEnv lists the exported variables to run a program with, in the form
// "name=value". [expand.Environ.Each] may yield a name more than once, with
// the innermost variable last; like Bash, the innermost set variable wins,
// except that a local variable without a value does not hide an outer one.
func execEnv(env expand.Environ) []string {
	list := make([]string, 0, 64)
	index := make(map[string]int, 64)
	for name, vr := range env.Each {
		i, seen := index[name]
		switch {
		case vr.IsSet() && vr.Exported && vr.Kind == expand.String:
			if seen {
				list[i] = name + "=" + vr.String()
			} else {
				index[name] = len(list)
				list = append(list, name+"="+vr.String())
			}
		case vr.IsSet() || !vr.Local:
			// Hidden by a set variable which is not exported,
			// or unset by the shell.
			if seen {
				list[i] = ""
				delete(index, name)
			}
		}
	}
	return slices.DeleteFunc(list, func(s string) bool { return s == "" })
}

func (r *Runner) lookupVar(name string) expand.Variable {
	if name == "" {
		return expand.Variable{}
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
			vr.Kind, vr.Str = expand.String, r.bgProcID(&r.bgProcs[n-1])
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
	default:
		// Positional parameters like $1 or ${10}.
		if name[0] >= '1' && name[0] <= '9' && strings.Trim(name, "0123456789") == "" {
			if n, err := strconv.Atoi(name); err == nil && n <= len(r.Params) {
				vr.Kind = expand.String
				vr.Str = r.Params[n-1]
			}
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
	_, vr := r.resolveName(name)
	return vr.String()
}

// varErr reports a variable error, if any, and returns it for the caller
// to fail the command at hand. A fatal error, such as a value over the
// limits in [expand], abandons the current commands like an expansion error.
func (r *Runner) varErr(err error) error {
	if _, ok := errors.AsType[internal.FatalError](err); ok {
		r.expandErr(err)
	} else if err != nil {
		r.errf("%v\n", err)
	}
	return err
}

// failVar reports a variable error like [Runner.varErr].
func (r *Runner) failVar(format string, args ...any) error {
	return r.varErr(fmt.Errorf(format, args...))
}

// delVar unsets a variable, reporting any error like [Runner.varErr].
// A nameref unsets the variable it refers to, unless nameref is true,
// like `unset -n`.
func (r *Runner) delVar(name string, nameref bool) error {
	if !nameref {
		name, _ = r.resolveName(name)
	}
	return r.varErr(r.writeEnv.unset(name))
}

// resolveName follows any namerefs, returning the name of the variable
// which name ultimately refers to. The variable is returned too.
func (r *Runner) resolveName(name string) (string, expand.Variable) {
	vr := r.lookupVar(name)
	if n, v := vr.Resolve(r.writeEnv); n != "" {
		return n, v
	}
	return name, vr
}

// setVarString sets a variable's value to a string, keeping its attributes
// such as whether it is exported. Errors are handled like in [Runner.varErr].
func (r *Runner) setVarString(name, value string) error {
	vr := r.lookupVar(name)
	if vr.Kind == expand.NameRef && vr.Str == "" {
		// Like an assignment, this sets the target of a nameref without one.
		return r.setNameRef(name, vr, value, true)
	}
	if n, v := vr.Resolve(r.writeEnv); n != "" {
		name, vr = n, v
	}
	return r.setVar(name, expand.Variable{Set: true, Exported: vr.Exported, Kind: expand.String, Str: value})
}

// setNameRef sets the target of the nameref vr, keeping its attributes.
// Errors are handled like in [Runner.varErr]; see [Runner.checkNameRefTarget].
func (r *Runner) setNameRef(name string, vr expand.Variable, target string, allowSelf bool) error {
	if err := r.checkNameRefTarget("", name, target, allowSelf); err != nil {
		return err
	}
	vr.Set, vr.Str = true, target
	return r.setVar(name, vr)
}

// checkNameRefTarget fails like [Runner.varErr] if the nameref name cannot
// refer to target, which must be a variable name. Unless allowSelf is set,
// like for an assignment, target cannot be the nameref itself either.
// A declaration builtin like declare, given as decl, reports errors its own
// way, like in Bash.
func (r *Runner) checkNameRefTarget(decl, name, target string, allowSelf bool) error {
	prefix := ""
	if decl != "" {
		prefix = decl + ": "
	}
	switch {
	case !allowSelf && target == name:
		return r.failVar("%s%s: nameref variable self references not allowed", prefix, name)
	case syntax.ValidName(target):
		return nil
	case decl != "":
		return r.failVar("%s`%s': invalid variable name for name reference", prefix, target)
	}
	if _, _, elem := cutElemSubscript(target); elem {
		// TODO: support namerefs to array elements like a[1], as Bash does.
		return r.failVar("%s: invalid variable name for name reference", target)
	}
	return r.failVar("`%s': not a valid identifier", target)
}

// setVar sets a variable, reporting any error like [Runner.varErr].
// Namerefs must have been followed already; see [Runner.setVarIn].
func (r *Runner) setVar(name string, vr expand.Variable) error {
	return r.varErr(r.setVarIn(name, vr, declPlain))
}

// declScope is the scope which a declaration builtin assigns in.
type declScope uint8

const (
	declPlain  declScope = iota // like a plain assignment; see [overlayEnviron.Set]
	declLocal                   // the current scope, like `local`
	declGlobal                  // the global scope, like `declare -g`
)

// errReadOnly is returned when trying to modify a read-only variable.
var errReadOnly = errors.New("readonly variable")

// errAbandoned is returned by [Runner.assignVal] when an expansion error
// abandoned the current commands, which it has already reported.
var errAbandoned = errors.New("assignment abandoned")

// randomMax is one past the highest value that RANDOM expands to, like in Bash.
const randomMax = 32768

// setVarIn sets a variable in the given scope. The name must have any
// nameref followed already, as done by [Runner.assignVal] and
// [Runner.resolveName], since declaring a nameref sets the nameref itself.
func (r *Runner) setVarIn(name string, vr expand.Variable, scope declScope) error {
	if r.opts[optAllExport] {
		vr.Exported = true
	}
	// An array is bounded like the fields of an expansion, as it can
	// outgrow them via a+=(...). Strings are checked where they are built.
	if len(vr.List) > internal.MaxExpandFields || len(vr.Map) > internal.MaxExpandFields {
		return internal.Fatalf("%s: value would exceed %d elements", name, internal.MaxExpandFields)
	}
	var err error
	switch scope {
	case declLocal:
		err = r.writeEnv.setLocal(name, vr)
	case declGlobal:
		err = r.writeEnv.setGlobal(name, vr)
	default:
		err = r.writeEnv.Set(name, vr)
	}
	if err != nil {
		return err
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
// Errors are handled like in [Runner.delVar].
func (r *Runner) unsetElem(name, sub string) error {
	name, vr := r.resolveName(name)
	switch vr.Kind {
	case expand.Indexed:
		if sub == "@" || sub == "*" {
			return r.delVar(name, false)
		}
		expr, err := syntax.NewParser().Arithmetic(strings.NewReader(sub))
		if err != nil {
			return r.failVar("unset: %s[%s]: bad array subscript", name, sub)
		}
		if expr == nil {
			return nil // an empty subscript like `unset 'a[]'` is a no-op
		}
		i, err := r.subscript(expr)
		if err != nil {
			return err
		}
		k, ok := internal.IndexedAbs(vr.List, vr.Indexes, i)
		if !ok {
			return r.failVar("unset: %s[%s]: bad array subscript", name, sub)
		}
		// TODO: only clone when inside a subshell and getting a var from outside for the first time
		vr.List = slices.Clone(vr.List)
		vr.Indexes = slices.Clone(vr.Indexes)
		vr.List, vr.Indexes = internal.DeleteIndexedElem(vr.List, vr.Indexes, k)
		return r.setVar(name, vr)
	case expand.Associative:
		if sub == "@" || sub == "*" {
			return r.delVar(name, false)
		}
		// TODO: only clone when inside a subshell and getting a var from outside for the first time
		vr.Map = maps.Clone(vr.Map)
		delete(vr.Map, sub)
		return r.setVar(name, vr)
	case expand.String:
		// A scalar can be unset via subscript zero.
		if sub == "0" {
			return r.delVar(name, false)
		}
		return r.failVar("unset: %s: not an array variable", name)
	}
	return nil
}

func (r *Runner) setFunc(name string, body *syntax.Stmt) {
	r.unshareFuncs()
	if r.Funcs == nil {
		r.Funcs = make(map[string]*syntax.Stmt, 4)
	}
	r.Funcs[name] = body
}

// assocKey expands an associative array subscript into its key.
func (r *Runner) assocKey(index syntax.ArithmExpr) string {
	return r.literal(arithm.Word(index))
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

// indexedElems returns a copy of a variable's elements as an indexed array,
// where a string value is the first element.
func indexedElems(vr expand.Variable) ([]string, []int) {
	switch {
	case vr.Kind == expand.Indexed:
		// TODO: only clone when inside a subshell and getting a var from outside for the first time
		return slices.Clone(vr.List), slices.Clone(vr.Indexes)
	case vr.Kind == expand.String && vr.IsSet():
		return []string{vr.Str}, nil
	}
	return nil, nil
}

// convertKind converts a variable to the kind given by a declaration flag
// like -a or -A, if any, like Bash does: a string becomes the first
// element, whereas an array cannot be converted to the other kind of array.
// Errors are handled like in [Runner.delVar].
func (r *Runner) convertKind(name string, vr expand.Variable, valType string) (expand.Variable, error) {
	switch valType {
	case "-a":
		switch vr.Kind {
		case expand.Associative:
			return vr, r.failVar("declare: %s: cannot convert associative to indexed array", name)
		case expand.Indexed:
		default:
			vr.List, vr.Indexes = indexedElems(vr)
			vr.Kind, vr.Str = expand.Indexed, ""
		}
	case "-A":
		switch vr.Kind {
		case expand.Indexed:
			return vr, r.failVar("declare: %s: cannot convert indexed to associative array", name)
		case expand.Associative:
		default:
			if vr.IsSet() {
				vr.Map = map[string]string{"0": vr.Str}
			}
			vr.Kind, vr.Str = expand.Associative, ""
		}
	}
	return vr, nil
}

// assignVal computes a variable's new value for an assignment word like
// x=y, x+=y, x[i]=y, x=(a b), or x+=(a b), given its current value.
// valType is "-a", "-A", or "-n" when a declaration builtin gives the kind.
// The name is returned too, as a nameref is resolved first.
// Errors are handled like in [Runner.delVar].
// Like Bash, an expansion error which abandons the current commands
// leaves the variable unchanged, returning [errAbandoned].
func (r *Runner) assignVal(name string, prev expand.Variable, as *syntax.Assign, valType string) (string, expand.Variable, error) {
	exiting := r.exit.exiting
	abandoned := func() bool { return r.exit.exiting && !exiting }
	str := ""
	if as.Value != nil {
		if str = r.literal(as.Value); abandoned() {
			return name, prev, errAbandoned
		}
	}
	if prev.Kind == expand.NameRef && prev.Str == "" && valType != "-n" {
		// Assigning to a nameref without a target, like `declare -n r; r=x`,
		// sets the target. A declaration builtin checks it on its own.
		valType = "-n"
		if str != "" && as.Array == nil {
			if err := r.checkNameRefTarget("", name, str, true); err != nil {
				return name, prev, err
			}
		}
	}
	if valType != "-n" {
		// Assigning to a nameref assigns to the variable it refers to,
		// unless we are declaring the nameref itself.
		if n, v := prev.Resolve(r.writeEnv); n != "" {
			name, prev = n, v
		}
	}
	prev, err := r.convertKind(name, prev, valType)
	if err != nil {
		return name, prev, err
	}
	if as.Array != nil {
		prev, err := r.assignArray(name, prev, as, abandoned)
		return name, prev, err
	}
	// A scalar value like x=y or x[i]+=y; `x=` has no value at all.
	switch {
	case valType == "-n":
		prev.Kind, prev.Str = expand.NameRef, str
	case prev.Kind == expand.Associative:
		key := "0"
		if as.Index != nil {
			if key = r.assocKey(as.Index); abandoned() {
				return name, prev, errAbandoned
			}
		}
		// TODO: only clone when inside a subshell and getting a var from outside for the first time
		prev.Map = maps.Clone(prev.Map)
		if prev.Map == nil {
			prev.Map = make(map[string]string)
		}
		if as.Append {
			str = prev.Map[key] + str
		}
		prev.Map[key] = str
	case prev.Kind == expand.Indexed || as.Index != nil:
		// Assigning to an element, where a plain x=y sets the first one.
		list, indexes := indexedElems(prev)
		k := 0
		if as.Index != nil {
			i, err := r.subscript(as.Index)
			if err != nil {
				return name, prev, err
			}
			var ok bool
			if k, ok = internal.IndexedAbs(list, indexes, i); !ok {
				return name, prev, r.failVar("%s[%s]: bad array subscript", name, r.assocKey(as.Index))
			}
		}
		if as.Append {
			old, _ := internal.IndexedElem(list, indexes, k)
			str = old + str
		}
		list, indexes = internal.SetIndexedElem(list, indexes, k, str)
		prev.Kind, prev.Str = expand.Indexed, ""
		prev.List, prev.Indexes = list, indexes
	default:
		if as.Append && prev.Kind == expand.String {
			str = prev.Str + str
		}
		prev.Kind, prev.Str = expand.String, str
	}
	// Appending can grow a value past the limits in [expand].
	if len(str) > internal.MaxExpandSize {
		r.expandErr(internal.Fatalf("%s: value would exceed %d bytes", name, internal.MaxExpandSize))
		return name, prev, errAbandoned
	}
	prev.Set = true
	return name, prev, nil
}

// assignArray implements [Runner.assignVal] for an array value,
// returning [errAbandoned] once abandoned reports that an expansion error
// abandoned the current commands.
func (r *Runner) assignArray(name string, prev expand.Variable, as *syntax.Assign, abandoned func() bool) (expand.Variable, error) {
	elems := as.Array.Elems
	assoc := prev.Kind == expand.Associative
	if prev.Kind == expand.Unknown && len(elems) > 0 && stringIndex(elems[0].Index) {
		assoc = true // a quoted subscript like x=(["k"]=v)
	}
	if assoc {
		var amap map[string]string
		if as.Append {
			// TODO: only clone when inside a subshell and getting a var from outside for the first time
			amap = maps.Clone(prev.Map)
		}
		if amap == nil {
			amap = make(map[string]string, len(elems))
		}
		if len(elems) > 0 && elems[0].Index == nil {
			// Like Bash, words without subscripts are key-value pairs,
			// and a subscripted word like [k]=v is taken as a whole.
			var key string
			for i, elem := range elems {
				word := r.literal(elem.Value)
				if elem.Index != nil {
					word = "[" + r.assocKey(elem.Index) + "]=" + word
				}
				if i%2 == 0 {
					key, amap[word] = word, ""
				} else {
					amap[key] = word
				}
				if abandoned() {
					return prev, errAbandoned
				}
			}
		} else {
			for _, elem := range elems {
				if elem.Index == nil {
					return prev, r.failVar("%s: %s: must use subscript when assigning associative array", name, r.literal(elem.Value))
				}
				amap[r.assocKey(elem.Index)] = r.literal(elem.Value)
				if abandoned() {
					return prev, errAbandoned
				}
			}
		}
		prev.Kind, prev.Str = expand.Associative, ""
		prev.Map = amap
		prev.Set = true
		return prev, nil
	}
	// The base array which the new elements are set on; empty unless
	// we are appending to an existing value.
	var list []string
	var indexes []int
	if as.Append {
		list, indexes = indexedElems(prev)
	}
	// Evaluate values for each array element. An explicit index like
	// [5]=x resets our index counter, which otherwise advances for every
	// value, starting after the maximum index of the base array.
	index := internal.IndexedMax(list, indexes) + 1
	for _, elem := range elems {
		if elem.Index != nil {
			// Index resets our index with a literal value.
			i, err := r.subscript(elem.Index)
			if err != nil {
				return prev, err
			}
			var ok bool
			if index, ok = internal.IndexedAbs(list, indexes, i); !ok {
				return prev, r.failVar("%s: bad array subscript", name)
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
		if abandoned() {
			return prev, errAbandoned
		}
		// Each word may add a million elements, so stop before the next one;
		// [Runner.setVarIn] fails on the result.
		if len(list) > internal.MaxExpandFields {
			break
		}
	}
	if list == nil {
		// An empty array like a=() must still expand to zero fields.
		list = []string{}
	}
	prev.Kind, prev.Str = expand.Indexed, ""
	prev.List, prev.Indexes = list, indexes
	prev.Set = true
	return prev, nil
}
