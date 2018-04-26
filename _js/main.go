package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/gopherjs/gopherjs/js"

	"mvdan.cc/sh/syntax"
)

func main() {
	exps := js.Module.Get("exports")

	exps.Set("syntax", map[string]interface{}{})

	stx := exps.Get("syntax")
	stx.Set("NodeType", func(v interface{}) (typ string) {
		if v == nil {
			return "nil"
		}
		node, ok := v.(syntax.Node)
		if !ok {
			throw("NodeType requires a Node argument")
		}
		typ = fmt.Sprintf("%T", node)
		if i := strings.LastIndexAny(typ, "*.]"); i >= 0 {
			typ = typ[i+1:]
		}
		return typ
	})

	stx.Set("NewParser", func() *js.Object {
		p := syntax.NewParser()
		return js.MakeWrapper(jsParser{p})
	})

	stx.Set("Walk", func(node syntax.Node, jsFn func(*js.Object) bool) {
		f := func(node syntax.Node) bool {
			if node == nil {
				return jsFn(nil)
			}
			return jsFn(js.MakeWrapper(node))
		}
		syntax.Walk(node, f)

	})
	stx.Set("DebugPrint", func(node syntax.Node) {
		syntax.DebugPrint(os.Stdout, node)
	})

	stx.Set("NewPrinter", func() *js.Object {
		p := syntax.NewPrinter()
		return js.MakeWrapper(jsPrinter{p})
	})
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

type jsPrinter struct {
	*syntax.Printer
}

func (p jsPrinter) Print(file *syntax.File) string {
	var buf bytes.Buffer
	if err := p.Printer.Print(&buf, file); err != nil {
		throw(err)
	}
	return buf.String()
}
