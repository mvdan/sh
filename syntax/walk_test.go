// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"fmt"
	"testing"
)

func TestWalk(t *testing.T) {
	t.Parallel()
	for i, c := range fileTests {
		for j, prog := range c.All {
			t.Run(fmt.Sprintf("%03d-%d", i, j), func(t *testing.T) {
				Walk(nopVisitor{}, prog)
			})
		}
	}
}

type nopVisitor struct{}

func (v nopVisitor) Visit(node Node) Visitor {
	if _, ok := node.(*Lit); ok {
		return nil
	}
	return v
}

type newNode struct{}

func (newNode) Pos() Pos { return 0 }
func (newNode) End() Pos { return 0 }

func TestWalkUnexpectedType(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("did not panic")
		}
	}()
	Walk(nopVisitor{}, newNode{})
}
