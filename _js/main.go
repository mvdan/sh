package main

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/gopherjs/gopherjs/js"

	"mvdan.cc/sh/syntax"
)

func main() {
	exps := js.Module.Get("exports")

	exps.Set("syntax", map[string]interface{}{})

	stx := exps.Get("syntax")

	// Type helpers just for JS
	stx.Set("NodeType", func(v interface{}) string {
		if v == nil {
			return "nil"
		}
		node, ok := v.(syntax.Node)
		if !ok {
			throw("NodeType requires a Node argument")
		}
		typ := fmt.Sprintf("%T", node)
		if i := strings.LastIndexAny(typ, "*.]"); i >= 0 {
			typ = typ[i+1:]
		}
		return typ
	})

	// Parser
	stx.Set("NewParser", func(options ...func(interface{})) *js.Object {
		p := syntax.NewParser()
		jp := js.MakeFullWrapper(jsParser{p})
		// Apply the options after we've wrapped the parser, as
		// otherwise we cannot internalise the value.
		for _, opt := range options {
			opt(jp)
		}
		return jp
	})

	stx.Set("KeepComments", func(v interface{}) {
		syntax.KeepComments(v.(jsParser).Parser)
	})
	stx.Set("Variant", func(l syntax.LangVariant) func(interface{}) {
		if math.IsNaN(float64(l)) {
			throw("Variant requires a LangVariant argument")
		}
		return func(v interface{}) {
			syntax.Variant(l)(v.(jsParser).Parser)
		}
	})
	stx.Set("LangBash", syntax.LangBash)
	stx.Set("LangPOSIX", syntax.LangPOSIX)
	stx.Set("LangMirBSDKorn", syntax.LangMirBSDKorn)
	stx.Set("StopAt", func(word string) func(interface{}) {
		return func(v interface{}) {
			syntax.StopAt(word)(v.(jsParser).Parser)
		}
	})

	// Printer
	stx.Set("NewPrinter", func() *js.Object {
		p := syntax.NewPrinter()
		return js.MakeFullWrapper(jsPrinter{p})
	})

	// Syntax utilities
	stx.Set("Walk", func(node syntax.Node, jsFn func(*js.Object) bool) {
		f := func(node syntax.Node) bool {
			if node == nil {
				return jsFn(nil)
			}
			return jsFn(js.MakeFullWrapper(node))
		}
		syntax.Walk(node, f)

	})
	stx.Set("DebugPrint", func(node syntax.Node) {
		syntax.DebugPrint(os.Stdout, node)
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
	return js.MakeFullWrapper(f)
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
