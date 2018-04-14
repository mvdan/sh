package main

import (
	"strings"

	"github.com/gopherjs/gopherjs/js"

	"mvdan.cc/sh/syntax"
)

func main() {
	exps := js.Module.Get("exports")

	exps.Set("syntax", map[string]interface{}{})
	stx := exps.Get("syntax")
	stx.Set("NewParser", func() *js.Object {
		p := syntax.NewParser()
		return js.MakeWrapper(jsParser{p})
	})
}

type jsParser struct {
	*syntax.Parser
}

func (p jsParser) Parse(src, name string) *js.Object {
	f, _ := p.Parser.Parse(strings.NewReader(src), name)
	return js.MakeWrapper(f)
}
