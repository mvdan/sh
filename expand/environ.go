// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package expand

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

type FuncEnviron func(string) string

func (f FuncEnviron) Get(name string) Variable {
	value := f(name)
	if value == "" {
		return Variable{}
	}
	return Variable{Value: value}
}

func (f FuncEnviron) Set(name string, vr Variable)             { panic("FuncEnviron is read-only") }
func (f FuncEnviron) Delete(name string)                       { panic("FuncEnviron is read-only") }
func (f FuncEnviron) Each(func(name string, vr Variable) bool) {}
func (f FuncEnviron) Sub() Environ                             { return f }
