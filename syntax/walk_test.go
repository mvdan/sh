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
				Walk(prog, func(node Node) bool {
					_, ok := node.(*Lit)
					return !ok
				})
			})
		}
	}
}

type newNode struct{}

func (newNode) Pos() Pos { return Pos{} }
func (newNode) End() Pos { return Pos{} }

func TestWalkUnexpectedType(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("did not panic")
		}
	}()
	Walk(newNode{}, func(node Node) bool {
		return true
	})
}
