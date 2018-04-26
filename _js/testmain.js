const assert = require('assert').strict

const sh = require('./index')

var syntax = sh.syntax
var parser = syntax.NewParser()
var printer = syntax.NewPrinter()

{
	// parsing a simple program
	var src = "echo 'foo'"
	var f = parser.Parse(src, "src")

	var stmts = f.StmtList.Stmts
	assert.equal(stmts.length, 1)

	var args = stmts[0].Cmd.Args
	assert.equal(args.length, 2)
	assert.equal(args[0].Parts.length, 1)
	assert.equal(args[0].Parts[0].Value, "echo")
}

{
	// accessing fields or methods creates separate objects
	var src = "echo 'foo'"
	var f = parser.Parse(src, "src")

	assert.equal(f.StmtList.Stmts == f.StmtList.Stmts, false)
	assert.equal(f.StmtList.Stmts === f.StmtList.Stmts, false)
	var stmts = f.StmtList.Stmts
	assert.equal(stmts == stmts, true)
	assert.equal(stmts === stmts, true)
}

{
	// parse errors
	var src = "echo ${"
	try {
		var f = parser.Parse(src, "src")
		assert.fail("did not error")
	} catch (err) {
	}
}

{
	// getting the types of nodes
	var src = "echo 'foo'"
	var f = parser.Parse(src, "src")

	var cmd = f.StmtList.Stmts[0].Cmd
	assert.equal(syntax.NodeType(cmd), "CallExpr")
	assert.equal(syntax.NodeType(cmd.Args[0].Parts[0]), "Lit")
}

{
	// running Walk
	var src = "foo bar"
	var f = parser.Parse(src, "src")

	var nilCount = 0
	var nonNilCount = 0
	var seenBar = false
	var seenCall = false
	syntax.Walk(f, function(node) {
		var typ = syntax.NodeType(node)
		if (node == null) {
			nilCount++
			assert.equal(typ, "nil")
		} else {
			nonNilCount++
			if (node.Value == "bar") {
				seenBar = true
			}
			assert.notEqual(typ, "nil")
		}
		if (typ == "CallExpr") {
			seenCall = true
		}
		return true
	})
	assert.equal(nonNilCount, 7)
	assert.equal(nilCount, 7)
	assert.equal(seenBar, true)
	assert.equal(seenCall, true)
}

{
	// printing
	var src = "echo      'foo'"
	var f = parser.Parse(src, "src")

	var out = printer.Print(f)
	assert.equal(out, "echo 'foo'\n")
}
