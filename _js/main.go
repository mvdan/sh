package main

import (
	"fmt"
	"strings"

	"github.com/gopherjs/gopherjs/js"

	"mvdan.cc/sh/syntax"
)

func main() {
	exps := js.Module.Get("exports")

	exps.Set("syntax", map[string]interface{}{})

	stx := exps.Get("syntax")
	stx.Set("NodeType", func(node syntax.Node) string {
		typ := fmt.Sprintf("%T", node)
		if i := strings.LastIndexAny(typ, "*.]"); i >= 0 {
			typ = typ[i+1:]
		}
		return typ
	})
	stx.Set("NewParser", func() *js.Object {
		p := syntax.NewParser()
		return js.MakeWrapper(jsParser{p})
	})
	stx.Set("Walk", syntax.Walk)
}

func throw(v interface{}) {
	js.Global.Call("$throwRuntimeError", fmt.Sprint(v))
}

type jsParser struct {
	*syntax.Parser
}

func (p jsParser) Parse(src, name string) *js.Object {
	f, err := p.Parser.Parse(strings.NewReader(src), name)
	if err != nil {
		throw(err)
	}
	return js.MakeWrapper(f)
}
