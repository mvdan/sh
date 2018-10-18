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

func (v Variable) Resolve(env Environ) Variable {
	for i := 0; i < maxNameRefDepth; i++ {
		if !v.NameRef {
			return v
		}
		v = env.Get(v.Value.(string))
	}
	return Variable{}
}
