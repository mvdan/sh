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

// what kind of command did we parse?
var stmt = f.StmtList.Stmts[0]
console.log(syntax.NodeType(stmt.Cmd)) // CallExpr

// change 'foo' to 'bar'
stmt.Cmd.Args[1].Parts[0].Value = "bar"

// print the code back out
console.log(printer.Print(f)) // echo 'bar'
```
