// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

//go:build go1.18
// +build go1.18

package syntax

import (
	"io"
	"os/exec"
	"strings"
	"testing"
)

func FuzzQuote(f *testing.F) {
	if _, err := exec.LookPath("bash"); err != nil {
		f.Skipf("requires bash to verify quoted strings")
	}

	// Keep in sync with ExampleQuote.
	f.Add("foo")
	f.Add("bar $baz")
	f.Add(`"won't"`)
	f.Add(`~/home`)
	f.Add("#1304")
	f.Add("name=value")
	f.Add(`glob-*`)
	f.Add("invalid-\xe2'")
	f.Add("nonprint-\x0b\x1b")
	f.Fuzz(func(t *testing.T, s string) {
		quoted, ok := Quote(s)
		if !ok {
			// Contains a null byte; not interesting.
			t.Skip()
		}
		// Beware that this might run arbitrary code
		// if Quote is too naive and allows ';' or '$'.
		//
		// Also note that this fuzzing would not catch '=',
		// as we don't use the quoted string as a first argument
		// to avoid running random commands.
		//
		// We could consider ways to fully sandbox the bash process,
		// but for now that feels overkill.
		out, err := exec.Command("bash", "-c", "printf %s "+quoted).CombinedOutput()
		if err != nil {
			t.Fatalf("bash error on %q quoted as %s: %v: %s", s, quoted, err, out)
		}
		want, got := s, string(out)
		if want != got {
			t.Fatalf("output mismatch on %q quoted as %s: got %q (len=%d)", want, quoted, got, len(got))
		}
	})
}

func FuzzParsePrint(f *testing.F) {
	add := func(src string, variant LangVariant) {
		// For now, default to just KeepComments.
		f.Add(src, uint8(variant), true, false,
			uint8(0), false, false, false, false, false, false, false)
	}

	for _, test := range shellTests {
		add(test.in, LangBash)
	}
	for _, test := range printTests {
		add(test.in, LangBash)
	}
	for _, test := range fileTests {
		for _, in := range test.Strs {
			if test.Bash != nil {
				add(in, LangBash)
			}
			if test.Posix != nil {
				add(in, LangPOSIX)
			}
			if test.MirBSDKorn != nil {
				add(in, LangMirBSDKorn)
			}
			if test.Bats != nil {
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
		if langVariant > 3 {
			t.Skip() // lang variants are 0-3
		}
		if indent > 16 {
			t.Skip() // more indentation won't really be interesting
		}

		parser := NewParser()
		Variant(LangVariant(langVariant))(parser)
		KeepComments(keepComments)(parser)

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
