# mvdan-sh

This package is a JavaScript version of a shell package written in Go, available
at https://github.com/mvdan/sh.

It is transpiled from Go to JS using GopherJS, available at
https://github.com/gopherjs/gopherjs

Here is a simple usage example:

```
const sh = require('mvdan-sh')
const syntax = sh.syntax

var parser = syntax.NewParser()
var printer = syntax.NewPrinter()

var src = "echo 'foo'"
var f = parser.Parse(src, "src.js")

// print out the syntax tree
syntax.DebugPrint(f)
console.log()

// replace all single quoted string values
syntax.Walk(f, function(node) {
        if (syntax.NodeType(node) == "SglQuoted") {
                node.Value = "bar"
        }
        return true
})

// print the code back out
console.log(printer.Print(f)) // echo 'bar'
```
