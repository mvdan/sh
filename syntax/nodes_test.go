// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"fmt"
	"strings"
	"testing"
)

func TestPosition(t *testing.T) {
	t.Parallel()
	parserBash := NewParser(KeepComments(true))
	parserPosix := NewParser(KeepComments(true), Variant(LangPOSIX))
	parserMirBSD := NewParser(KeepComments(true), Variant(LangMirBSDKorn))
	for i, c := range fileTests {
		for j, in := range c.Strs {
			t.Run(fmt.Sprintf("%03d-%d", i, j), func(t *testing.T) {
				parser := parserPosix
				if c.Bash != nil {
					parser = parserBash
				} else if c.MirBSDKorn != nil {
					parser = parserMirBSD
				}
				prog, err := parser.Parse(strings.NewReader(in), "")
				if err != nil {
					t.Fatalf("Unexpected error in %q: %v", in, err)
				}
				v := &posWalker{
					t:     t,
					f:     prog,
					lines: strings.Split(in, "\n"),
				}
				Walk(prog, v.Visit)
			})
		}
	}
}

type posWalker struct {
	t     *testing.T
	f     *File
	lines []string
}

func (v *posWalker) Visit(n Node) bool {
	if n == nil {
		return true
	}
	p := n.Pos()
	if !p.IsValid() && len(v.f.Stmts) > 0 {
		v.t.Fatalf("Invalid Pos")
	}
	if c, ok := n.(*Comment); ok {
		if v.f.Pos().After(c.Pos()) {
			v.t.Fatalf("A Comment is before its File")
		}
		if c.End().After(v.f.End()) {
			v.t.Fatalf("A Comment is after its File")
		}
	}
	return true
}

func TestWeirdOperatorString(t *testing.T) {
	t.Parallel()
	op := RedirOperator(1000)
	want := "token(1000)"
	if got := op.String(); got != want {
		t.Fatalf("token.String() mismatch: want %s, got %s", want, got)
	}
}
