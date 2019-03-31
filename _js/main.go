package main

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/gopherjs/gopherjs/js"

	"mvdan.cc/sh/v3/syntax"
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
			panic("NodeType requires a Node argument")
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
		jp := js.MakeFullWrapper(&jsParser{Parser: *p})
		// Apply the options after we've wrapped the parser, as
		// otherwise we cannot internalise the value.
		for _, opt := range options {
			opt(jp)
		}
		return jp
	})
	stx.Set("IsIncomplete", syntax.IsIncomplete)

	stx.Set("KeepComments", func(v interface{}) {
		syntax.KeepComments(&v.(*jsParser).Parser)
	})
	stx.Set("Variant", func(l syntax.LangVariant) func(interface{}) {
		if math.IsNaN(float64(l)) {
			panic("Variant requires a LangVariant argument")
		}
		return func(v interface{}) {
			syntax.Variant(l)(&v.(*jsParser).Parser)
		}
	})
	stx.Set("LangBash", syntax.LangBash)
	stx.Set("LangPOSIX", syntax.LangPOSIX)
	stx.Set("LangMirBSDKorn", syntax.LangMirBSDKorn)
	stx.Set("StopAt", func(word string) func(interface{}) {
		return func(v interface{}) {
			syntax.StopAt(word)(&v.(*jsParser).Parser)
		}
	})

	// Printer
	stx.Set("NewPrinter", func() *js.Object {
		p := syntax.NewPrinter()
		return js.MakeFullWrapper(jsPrinter{p})
	})

	// Syntax utilities
	stx.Set("Walk", func(node syntax.Node, jsFn func(*js.Object) bool) {
		fn := func(node syntax.Node) bool {
			if node == nil {
				return jsFn(nil)
			}
			return jsFn(js.MakeFullWrapper(node))
		}
		syntax.Walk(node, fn)
	})
	stx.Set("DebugPrint", func(node syntax.Node) {
		syntax.DebugPrint(os.Stdout, node)
	})
	stx.Set("SplitBraces", func(w *syntax.Word) *js.Object {
		w = syntax.SplitBraces(w)
		return js.MakeFullWrapper(w)
	})
}

func throw(err error) {
	js.Global.Call("$throw", js.MakeFullWrapper(err))
}

// streamReader is an io.Reader wrapper for Node's stream.Readable. See
// https://nodejs.org/api/stream.html#stream_class_stream_readable
// TODO: support https://streams.spec.whatwg.org/#rs-class too?
type streamReader struct {
	stream *js.Object
}

func (r streamReader) Read(p []byte) (n int, err error) {
	obj := r.stream.Call("read", len(p))
	if obj == nil {
		return 0, io.EOF
	}
	bs := []byte(obj.String())
	return copy(p, bs), nil
}

type jsParser struct {
	syntax.Parser

	accumulated []*syntax.Stmt
	incomplete  bytes.Buffer
}

func adaptReader(src *js.Object) io.Reader {
	if src.Get("read") != js.Undefined {
		return streamReader{stream: src}
	}
	return strings.NewReader(src.String())
}

func (p *jsParser) Parse(src *js.Object, name string) *js.Object {
	f, err := p.Parser.Parse(adaptReader(src), name)
	if err != nil {
		throw(err)
	}
	return js.MakeFullWrapper(f)
}

func (p *jsParser) Incomplete() bool {
	return p.Parser.Incomplete()
}

func (p *jsParser) Interactive(src *js.Object, jsFn func([]*js.Object) bool) {
	fn := func(stmts []*syntax.Stmt) bool {
		objs := make([]*js.Object, len(stmts))
		for i, stmt := range stmts {
			objs[i] = js.MakeFullWrapper(stmt)
		}
		return jsFn(objs)
	}
	err := p.Parser.Interactive(adaptReader(src), fn)
	if err != nil {
		throw(err)
	}
}

func (p *jsParser) InteractiveStep(line string) []*syntax.Stmt {
	// pick up previous chunks of the incomplete statement
	r := strings.NewReader(p.incomplete.String() + line)
	lastEnd := uint(0)
	err := p.Parser.Interactive(r, func(stmts []*syntax.Stmt) bool {
		if len(stmts) > 0 {
			// don't re-parse finished statements
			lastEnd = stmts[len(stmts)-1].End().Offset()
		}
		p.accumulated = append(p.accumulated, stmts...)
		return false
	})
	if syntax.IsIncomplete(err) {
		// starting or continuing an incomplete statement
		p.incomplete.WriteString(line[lastEnd:])
		return p.accumulated
	}
	// complete; empty both fields and return
	p.incomplete.Reset()
	if err != nil {
		throw(err)
	}
	acc := p.accumulated
	p.accumulated = p.accumulated[:0]
	return acc
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
