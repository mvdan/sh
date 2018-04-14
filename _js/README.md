# mvdan-sh

This package is a JavaScript version of a shell package written in Go, available
at https://github.com/mvdan/sh.

It is transpiled from Go to JS using GopherJS, available at
https://github.com/gopherjs/gopherjs

Here is a simple usage example:

```
const sh = require('mvdan-sh')
const syntax = sh.syntax

var p = syntax.NewParser()

var src = "echo 'foo'"
var f = p.Parse(src, "src.js")

var stmt = f.StmtList.Stmts[0]
var args = stmt.Cmd.Args
console.log(args[0])
```
