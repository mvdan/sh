// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"io"
	"os/exec"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"
)

func FuzzQuote(f *testing.F) {
	if _, err := exec.LookPath("bash"); err != nil {
		f.Skipf("requires bash to verify quoted strings")
	}

	// Keep in sync with ExampleQuote.
	f.Add("foo", uint8(LangBash))
	f.Add("bar $baz", uint8(LangBash))
	f.Add(`"won't"`, uint8(LangBash))
	f.Add(`~/home`, uint8(LangBash))
	f.Add("#1304", uint8(LangBash))
	f.Add("name=value", uint8(LangBash))
	f.Add(`glob-*`, uint8(LangBash))
	f.Add("invalid-\xe2'", uint8(LangBash))
	f.Add("nonprint-\x0b\x1b", uint8(LangBash))
	f.Fuzz(func(t *testing.T, s string, langVariant uint8) {
		lang := LangVariant(langVariant)
		quoted, err := Quote(s, lang)
		if err != nil {
			// Cannot be quoted; not interesting.
			t.Skip()
		}

		var shellProgram string
		switch lang {
		case LangBash:
			requireBash52(t)
			shellProgram = "bash"
		case LangPOSIX:
			requireDash059(t)
			shellProgram = "dash"
		case LangMirBSDKorn:
			requireMksh59(t)
			shellProgram = "mksh"
		case LangBats:
			t.Skip() // bats has no shell and its syntax is just bash
		default:
			t.Skip() // invalid/unknown lang variant
		}

		// Verify that our parser ends up with a simple command with one word.
		f, err := NewParser(Variant(lang)).Parse(strings.NewReader(quoted), "")
		if err != nil {
			t.Fatalf("parse error on %q quoted as %s: %v", s, quoted, err)
		}
		qt.Assert(t, qt.Equals(len(f.Stmts), 1), qt.Commentf("in: %q, quoted: %s", s, quoted))
		call, ok := f.Stmts[0].Cmd.(*CallExpr)
		qt.Assert(t, qt.IsTrue(ok), qt.Commentf("in: %q, quoted: %s", s, quoted))
		qt.Assert(t, qt.Equals(len(call.Args), 1), qt.Commentf("in: %q, quoted: %s", s, quoted))

		// Also check that the single word only uses literals or quoted strings.
		Walk(call.Args[0], func(node Node) bool {
			switch node.(type) {
			case nil, *Word, *Lit, *SglQuoted, *DblQuoted:
			default:
				t.Fatalf("unexpected node type: %T", node)
			}
			return true
		})

		// The process below shouldn't run arbitrary code,
		// since our parser checks above should catch the use of ';' or '$',
		// in the case that Quote were too naive to quote them.
		out, err := exec.Command(shellProgram, "-c", "printf %s "+quoted).CombinedOutput()
		if err != nil {
			t.Fatalf("%s error on %q quoted as %s: %v: %s", shellProgram, s, quoted, err, out)
		}
		want, got := s, string(out)
		if want != got {
			t.Fatalf("%s output mismatch on %q quoted as %s: got %q (len=%d)",
				shellProgram, want, quoted, got, len(got))
		}
	})
}

func FuzzParsePrint(f *testing.F) {
	add := func(src string, variant LangVariant) {
		// For now, default to just KeepComments.
		f.Add(src, uint8(variant), true, false,
			uint8(0), false, false, false, false, false, false, false)
	}

	for _, test := range errorCases {
		add(test.in, LangBash)
	}
	for _, test := range printTests {
		add(test.in, LangBash)
	}
	for _, test := range fileTests {
		for _, in := range test.inputs {
			if test.bash != nil {
				add(in, LangBash)
			}
			if test.posix != nil {
				add(in, LangPOSIX)
			}
			if test.mksh != nil {
				add(in, LangMirBSDKorn)
			}
			if test.bats != nil {
				add(in, LangBats)
			}
		}
	}

	f.Fuzz(func(t *testing.T,
		src string,

		// parser options
		// TODO: also fuzz StopAt
		langVariant uint8, // 0-3
		keepComments bool,

		simplify bool,

		// printer options
		indent uint8, // 0-255
		binaryNextLine bool,
		switchCaseIndent bool,
		spaceRedirects bool,
		keepPadding bool,
		minify bool,
		singleLine bool,
		functionNextLine bool,
	) {
		lang := LangVariant(langVariant)
		if lang.count() != 1 || !lang.is(langResolvedVariants) {
			t.Skip()
		}
		if indent > 16 {
			t.Skip() // more indentation won't really be interesting
		}

		parser := NewParser(Variant(lang), KeepComments(keepComments))
		prog, err := parser.Parse(strings.NewReader(src), "")
		if err != nil {
			t.Skip() // not valid shell syntax
		}

		if simplify {
			Simplify(prog)
		}

		printer := NewPrinter()
		Indent(uint(indent))(printer)
		BinaryNextLine(binaryNextLine)(printer)
		SwitchCaseIndent(switchCaseIndent)(printer)
		SpaceRedirects(spaceRedirects)(printer)
		KeepPadding(keepPadding)(printer)
		Minify(minify)(printer)
		SingleLine(singleLine)(printer)
		FunctionNextLine(functionNextLine)(printer)

		if err := printer.Print(io.Discard, prog); err != nil {
			t.Skip() // e.g. invalid option
		}
	})
}
