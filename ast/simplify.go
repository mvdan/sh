// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package ast

// Simplify modifies a given node in-place. It does the following:
// * replace `foo` by $(foo)
func Simplify(node Node) {
	s := &simplifier{}
	Walk(s, node)
}

type simplifier struct{}

func (s *simplifier) Visit(node Node) Visitor {
	switch x := node.(type) {
	case nil:
	case *CmdSubst:
		x.Backquotes = false
	}
	return s
}
