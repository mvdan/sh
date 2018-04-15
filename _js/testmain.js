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

{
	// accessing fields or methods creates separate objects
	var src = "echo 'foo'"
	var f = p.Parse(src, "src")

	assert.strictEqual(f.StmtList.Stmts == f.StmtList.Stmts, false)
	assert.strictEqual(f.StmtList.Stmts === f.StmtList.Stmts, false)
	var stmts = f.StmtList.Stmts
	assert.strictEqual(stmts == stmts, true)
	assert.strictEqual(stmts === stmts, true)
}

{
	// parse errors
	var src = "echo ${"
	try {
		var f = p.Parse(src, "src")
		assert.fail("did not error")
	} catch (err) {
	}
}

{
	// getting the types of nodes
	var src = "echo 'foo'"
	var f = p.Parse(src, "src")

	var cmd = f.StmtList.Stmts[0].Cmd
	assert.strictEqual(syntax.NodeType(cmd), "CallExpr")
	assert.strictEqual(syntax.NodeType(cmd.Args[0].Parts[0]), "Lit")
}

{
	// running Walk
	var src = "foo bar"
	var f = p.Parse(src, "src")

	var nilCount = 0
	var nonNilCount = 0
	var seenBar = false
	syntax.Walk(f, function(node) {
		if (node == null) {
			nilCount++
		} else {
			nonNilCount++
			if (node.Value == "bar") {
				seenBar = true
			}
		}
		return true
	})
	assert.strictEqual(nonNilCount, 7)
	assert.strictEqual(nilCount, 7)
	assert.strictEqual(seenBar, true)
}
