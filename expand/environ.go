// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package expand

import (
	"sort"
	"strings"
)

type Environ interface {
	Get(name string) Variable
	Set(name string, vr Variable)
	Delete(name string)
	Each(func(name string, vr Variable) bool)
}

type Variable struct {
	Local    bool
	Exported bool
	ReadOnly bool
	NameRef  bool
	Value    interface{} // string, []string, or map[string]string
}

// String returns the variable's value as a string. In general, this only makes
// sense if the variable has a string value or no value at all.
func (v Variable) String() string {
	switch x := v.Value.(type) {
	case string:
		return x
	case []string:
		if len(x) > 0 {
			return x[0]
		}
	case map[string]string:
		// nothing to do
	}
	return ""
}

// maxNameRefDepth defines the maximum number of times to follow references when
// resolving a variable. Otherwise, simple name reference loops could crash a
// program quite easily.
const maxNameRefDepth = 100

// Resolve follows a number of nameref variables, returning the last reference
// name that was followed and the variable that it points to.
func (v Variable) Resolve(env Environ) (string, Variable) {
	name := ""
	for i := 0; i < maxNameRefDepth; i++ {
		if !v.NameRef {
			return name, v
		}
		name = v.Value.(string)
		v = env.Get(name)
	}
	return name, Variable{}
}

func FuncEnviron(fn func(string) string) Environ {
	return funcEnviron(fn)
}

type funcEnviron func(string) string

func (f funcEnviron) Get(name string) Variable {
	value := f(name)
	if value == "" {
		return Variable{}
	}
	return Variable{Value: value}
}

func (f funcEnviron) Set(name string, vr Variable)             { panic("FuncEnviron is read-only") }
func (f funcEnviron) Delete(name string)                       { panic("FuncEnviron is read-only") }
func (f funcEnviron) Each(func(name string, vr Variable) bool) {}

func ListEnviron(pairs ...string) Environ {
	list := append([]string{}, pairs...)
	sort.Strings(list)
	last := ""
	for i := 0; i < len(list); i++ {
		s := list[i]
		sep := strings.IndexByte(s, '=')
		if sep < 0 {
			// invalid element; remove it
			list = append(list[:i], list[i+1:]...)
			continue
		}
		name := s[:sep]
		if last == name {
			// duplicate; the last one wins
			list = append(list[:i-1], list[i:]...)
			continue
		}
		last = name
	}
	return listEnviron(list)
}

type listEnviron []string

func (l listEnviron) Get(name string) Variable {
	prefix := name + "="
	for _, pair := range l {
		if val := strings.TrimPrefix(pair, prefix); val != pair {
			return Variable{Value: val}
		}
	}
	return Variable{}
}

func (l listEnviron) Set(name string, vr Variable) { panic("ListEnviron is read-only") }
func (l listEnviron) Delete(name string)           { panic("ListEnviron is read-only") }
func (l listEnviron) Each(fn func(name string, vr Variable) bool) {
	for _, pair := range l {
		i := strings.IndexByte(pair, '=')
		if i < 0 {
			// can't happen; see above
			panic("expand.listEnviron: did not expect malformed name-value pair: " + pair)
		}
		name, value := pair[:i], pair[i+1:]
		if !fn(name, Variable{Value: value}) {
			return
		}
	}
}
