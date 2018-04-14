const assert = require('assert').strict

const sh = require('./index')

var syntax = sh.syntax
var p = syntax.NewParser()

{
	// parsing a simple program
	var src = "echo 'foo'"
	var f = p.Parse(src, "src")

	var stmts = f.StmtList.Stmts
	assert.strictEqual(stmts.length, 1)

	var args = stmts[0].Cmd.Args
	assert.strictEqual(args.length, 2)
	assert.strictEqual(args[0].Parts.length, 1)
	assert.strictEqual(args[0].Parts[0].Value, "echo")
}
