// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package typedjson_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"

	"mvdan.cc/sh/v3/syntax"
	"mvdan.cc/sh/v3/syntax/typedjson"
)

var update = flag.Bool("u", false, "update output files")

func TestRoundtripZsh(t *testing.T) {
	t.Parallel()

	// ${a[(r)foo]} produces a FlagsArithm node, which only appears with zsh.
	src := "echo ${a[(r)foo]}\n"
	parser := syntax.NewParser(syntax.Variant(syntax.LangZsh))
	node, err := parser.Parse(strings.NewReader(src), "")
	qt.Assert(t, qt.IsNil(err))

	var buf bytes.Buffer
	qt.Assert(t, qt.IsNil(typedjson.Encode(&buf, node)))

	node2, err := typedjson.Decode(&buf)
	qt.Assert(t, qt.IsNil(err))

	sb := new(strings.Builder)
	qt.Assert(t, qt.IsNil(syntax.NewPrinter().Print(sb, node2)))
	qt.Assert(t, qt.Equals(sb.String(), src))
}

// allNodeNames lists the name of every [syntax.Node] type,
// all of which must be encodable and decodable as a root node.
var allNodeNames = []string{
	"ArithmCmd", "ArithmExp", "ArrayElem", "ArrayExpr", "Assign",
	"BinaryArithm", "BinaryCmd", "BinaryTest", "Block", "BraceExp",
	"CStyleLoop", "CallExpr", "CaseClause", "CaseItem", "CmdSubst", "Comment",
	"CoprocClause", "DblQuoted", "DeclClause", "ExtGlob", "File", "FlagsArithm",
	"ForClause", "FuncDecl", "IfClause", "LetClause", "Lit", "ParamExp",
	"ParenArithm", "ParenTest", "ProcSubst", "Redirect", "SglQuoted", "Stmt",
	"Subshell", "TestClause", "TestDecl", "TimeClause", "UnaryArithm",
	"UnaryTest", "WhileClause", "Word", "WordIter",
}

// TestRoundtripAnyNode checks that any node can be encoded and decoded,
// not just [syntax.File], and that every node type is covered.
func TestRoundtripAnyNode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		lang syntax.LangVariant
		src  string
	}{
		{syntax.LangBash, `
foo=bar qux >redir <<EOF
hdoc
EOF
baz=(one [two]=three)
! foo | bar || baz & 'sgl' "dbl" ${foo:-x} $(cmd) $((1 + -2)) $(( ($x) )) @(glob) <(proc) {a,b}
if foo; then bar; else baz; fi
for i in x; do foo; done
for ((i = 0; i < 3; i++)); do foo; done
while foo; do bar; done
case i in foo) bar ;; esac
{ foo; }
(foo)
foo() { bar; }
declare -A foo
let x=1
time foo
coproc foo
((2))
[[ ! (foo && -n bar) ]]
# comment
`},
		// ${a[(r)foo]} produces a FlagsArithm node, which only appears with zsh.
		{syntax.LangZsh, "echo ${a[(r)foo]}\n"},
		// TestDecl only appears with bats.
		{syntax.LangBats, "@test \"name\" {\n\tfoo\n}\n"},
	}

	seen := make(map[string]bool)
	roundtrip := func(node syntax.Node) {
		var buf bytes.Buffer
		qt.Assert(t, qt.IsNil(typedjson.Encode(&buf, node)))
		encoded := buf.String()

		var typed struct{ Type string }
		qt.Assert(t, qt.IsNil(json.Unmarshal(buf.Bytes(), &typed)))
		qt.Assert(t, qt.Not(qt.Equals(typed.Type, "")))
		seen[typed.Type] = true

		// Decoding and encoding again must give the same JSON,
		// as no information is lost along the way.
		node2, err := typedjson.Decode(strings.NewReader(encoded))
		qt.Assert(t, qt.IsNil(err), qt.Commentf("node: %s", encoded))
		buf.Reset()
		qt.Assert(t, qt.IsNil(typedjson.Encode(&buf, node2)))
		qt.Assert(t, qt.Equals(buf.String(), encoded))
	}
	for _, test := range tests {
		parser := syntax.NewParser(syntax.Variant(test.lang), syntax.KeepComments(true))
		file, err := parser.Parse(strings.NewReader(test.src), "")
		qt.Assert(t, qt.IsNil(err))
		syntax.Walk(file, func(node syntax.Node) bool {
			if node == nil {
				return false
			}
			roundtrip(node)
			return true
		})
	}

	// The parser never produces a BraceExp, and [syntax.Walk] does not know
	// how to walk one either, so cover it on its own.
	file, err := syntax.NewParser().Parse(strings.NewReader("{a,b}"), "")
	qt.Assert(t, qt.IsNil(err))
	word := file.Stmts[0].Cmd.(*syntax.CallExpr).Args[0]
	qt.Assert(t, qt.IsTrue(syntax.SplitBraces(word)))
	roundtrip(word.Parts[0].(*syntax.BraceExp))

	qt.Assert(t, qt.ContentEquals(slices.Sorted(maps.Keys(seen)), allNodeNames))
}

// TestDecodeErrors checks that malformed input results in errors
// rather than panics from unchecked reflection operations.
func TestDecodeErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"Empty", ``, "EOF"},
		{"BadJSON", `{`, "unexpected EOF"},
		{"Null", `null`, "cannot decode JSON null into a syntax node"},
		{"RootString", `"x"`, "cannot decode JSON string into syntax.Node"},
		{"RootNumber", `3`, "cannot decode JSON number into syntax.Node"},
		{"RootBool", `true`, "cannot decode JSON boolean into syntax.Node"},
		{"RootArray", `[]`, "cannot decode JSON array into syntax.Node"},
		{"RootNoType", `{}`, `missing "Type" to decode a JSON object into syntax.Node`},
		{"UnknownType", `{"Type":"Foo"}`, `unknown type: "Foo"`},
		{"UnknownField", `{"Type":"File","Foo":3}`, `unknown field for syntax.File: "Foo"`},

		{"StringIntoSlice", `{"Type":"File","Stmts":"x"}`, "cannot decode JSON string into []*syntax.Stmt"},
		{"NumberIntoSlice", `{"Type":"File","Stmts":3}`, "cannot decode JSON number into []*syntax.Stmt"},
		{"ObjectIntoSlice", `{"Type":"File","Stmts":{}}`, "cannot decode JSON object into []*syntax.Stmt"},
		{"NumberIntoString", `{"Type":"File","Name":3}`, "cannot decode JSON number into string"},
		{"ArrayIntoString", `{"Type":"File","Name":[]}`, "cannot decode JSON array into string"},
		{"BoolIntoString", `{"Type":"File","Name":true}`, "cannot decode JSON boolean into string"},
		{"StringIntoBool", `{"Type":"Word","Parts":[{"Type":"ParamExp","Short":"x"}]}`, "cannot decode JSON string into bool"},
		{"ArrayIntoStruct", `{"Type":"File","Stmts":[[]]}`, "cannot decode JSON array into *syntax.Stmt"},
		{"MismatchedType", `{"Type":"File","Stmts":[{"Cmd":{"Type":"Word"}}]}`, "cannot decode Word into syntax.Command"},
		{"NoTypeForInterface", `{"Type":"File","Stmts":[{"Cmd":{}}]}`, `missing "Type" to decode a JSON object into syntax.Command`},

		{"StringIntoPos", `{"Type":"Lit","ValuePos":"x"}`, `cannot decode JSON string into a position`},
		{"ArrayIntoPos", `{"Type":"Lit","ValuePos":[]}`, `cannot decode JSON array into a position`},
		{"PosMissingField", `{"Type":"Lit","ValuePos":{"Offset":0,"Line":1}}`, `a position must contain exactly the fields [Offset Line Col]`},
		{"PosWrongField", `{"Type":"Lit","ValuePos":{"Offset":0,"Line":1,"Column":1}}`, `a position must contain the field "Col"`},
		{"PosStringField", `{"Type":"Lit","ValuePos":{"Offset":0,"Line":1,"Col":"x"}}`, `cannot decode JSON string into the position field "Col"`},
		{"PosNegativeField", `{"Type":"Lit","ValuePos":{"Offset":-1,"Line":1,"Col":1}}`, `the position field "Offset" is out of range: -1`},
		{"PosHugeField", `{"Type":"Lit","ValuePos":{"Offset":1e30,"Line":1,"Col":1}}`, `the position field "Offset" is out of range: 1e+30`},
		{"PosFractionField", `{"Type":"Lit","ValuePos":{"Offset":1.5,"Line":1,"Col":1}}`, `the position field "Offset" is out of range: 1.5`},

		{"UnknownOperator", `{"Type":"ExtGlob","Op":"x"}`, `invalid GlobOperator: "x"`},
		{"NumberIntoOperator", `{"Type":"ExtGlob","Op":3}`, "cannot decode JSON number into syntax.GlobOperator; a string is required"},
		{"OptStateOverflow", `{"Type":"ParamExp","Split":300}`, "cannot decode the JSON number 300 into syntax.OptState"},
		{"OptStateNegative", `{"Type":"ParamExp","Split":-1}`, "cannot decode the JSON number -1 into syntax.OptState"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			node, err := typedjson.Decode(strings.NewReader(test.input))
			qt.Assert(t, qt.IsNil(node))
			qt.Assert(t, qt.IsNotNil(err))
			qt.Assert(t, qt.StringContains(err.Error(), test.want))
		})
	}
}

// FuzzDecode checks that no input can cause the decoder to panic.
// Note that a decoded tree may still be incomplete, such as a binary command
// missing its operands, which the printer and interpreter do not support.
func FuzzDecode(f *testing.F) {
	jsonInput, err := os.ReadFile(filepath.Join("testdata", "roundtrip", "file.json"))
	qt.Assert(f, qt.IsNil(err))
	f.Add(string(jsonInput))
	f.Add(`{"Type":"File","Stmts":"x"}`)
	f.Add(`{"Type":"Lit","ValuePos":{"Offset":0,"Line":1,"Col":1},"Value":"x"}`)
	f.Add(`{"Type":"Word","Parts":[{"Type":"ParamExp","Excl":true,"Names":"@"}]}`)

	f.Fuzz(func(t *testing.T, src string) {
		typedjson.Decode(strings.NewReader(src))
	})
}

func TestRoundtrip(t *testing.T) {
	t.Parallel()

	dir := filepath.Join("testdata", "roundtrip")
	shellPaths, err := filepath.Glob(filepath.Join(dir, "*.sh"))
	qt.Assert(t, qt.IsNil(err))
	for _, shellPath := range shellPaths {
		name := strings.TrimSuffix(filepath.Base(shellPath), ".sh")
		jsonPath := filepath.Join(dir, name+".json")
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			shellInput, err := os.ReadFile(shellPath)
			qt.Assert(t, qt.IsNil(err))
			jsonInput, err := os.ReadFile(jsonPath)
			if !*update { // allow it to not exist
				qt.Assert(t, qt.IsNil(err))
			}
			sb := new(strings.Builder)

			// Parse the shell source and check that it is well formatted.
			parser := syntax.NewParser(syntax.KeepComments(true))
			node, err := parser.Parse(bytes.NewReader(shellInput), "")
			qt.Assert(t, qt.IsNil(err))

			printer := syntax.NewPrinter()
			sb.Reset()
			err = printer.Print(sb, node)
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Equals(sb.String(), string(shellInput)))

			// Validate writing the pretty JSON.
			sb.Reset()
			encOpts := typedjson.EncodeOptions{Indent: "\t"}
			err = encOpts.Encode(sb, node)
			qt.Assert(t, qt.IsNil(err))
			got := sb.String()
			if *update {
				err := os.WriteFile(jsonPath, []byte(got), 0o666)
				qt.Assert(t, qt.IsNil(err))
			} else {
				qt.Assert(t, qt.Equals(got, string(jsonInput)))
			}

			// Ensure we don't use the originally parsed node again.
			node = nil

			// Validate reading the pretty JSON and check that it formats the same.
			node2, err := typedjson.Decode(bytes.NewReader(jsonInput))
			qt.Assert(t, qt.IsNil(err))

			sb.Reset()
			err = printer.Print(sb, node2)
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Equals(sb.String(), string(shellInput)))

			// Validate that emitting the JSON again produces the same result.
			sb.Reset()
			err = encOpts.Encode(sb, node2)
			qt.Assert(t, qt.IsNil(err))
			got = sb.String()
			qt.Assert(t, qt.Equals(got, string(jsonInput)))
		})
	}
}
