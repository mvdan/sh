// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

//go:build go1.18
// +build go1.18

package syntax_test

import (
	"io/ioutil"
	"os/exec"
	"strings"
	"testing"

	"mvdan.cc/sh/v3/syntax"
)

func FuzzQuote(f *testing.F) {
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
		quoted, ok := syntax.Quote(s)
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
	// TODO: turn these into separate option parameters
	var zeroParserOpts uint8
	var zeroPrinterOpts uint16

	// TODO: probably use f.Add on table-driven test cases too?
	// In the past, any crashers found by go-fuzz got put there.

	f.Add("echo foo", zeroParserOpts, zeroPrinterOpts)
	f.Add("'foo' \"bar\" $'baz'", zeroParserOpts, zeroPrinterOpts)
	f.Add("if foo; then bar; baz; fi", zeroParserOpts, zeroPrinterOpts)
	f.Add("{ (foo; bar); baz; }", zeroParserOpts, zeroPrinterOpts)
	f.Add("$foo ${bar} ${baz[@]} $", zeroParserOpts, zeroPrinterOpts)
	f.Add("foo >bar <<EOF\nbaz\nEOF", zeroParserOpts, zeroPrinterOpts)
	f.Add("foo=bar baz=(x y z)", zeroParserOpts, zeroPrinterOpts)
	f.Add("foo && bar || baz", zeroParserOpts, zeroPrinterOpts)
	f.Add("foo | bar # baz", zeroParserOpts, zeroPrinterOpts)
	f.Add("foo \\\n bar \\\\ba\\z", zeroParserOpts, zeroPrinterOpts)

	f.Fuzz(func(t *testing.T, src string, parserOpts uint8, printerOpts uint16) {
		// Below are the bit masks for parserOpts and printerOpts.
		// Most masks are a single bit, for boolean options.
		const (
			// parser
			// TODO: also fuzz StopAt
			maskLangVariant  = 0b0000_0011 // two bits; 0-3 matching the iota
			maskKeepComments = 0b0000_0100
			maskSimplify     = 0b0000_1000 // pretend it's a parser option

			// printer
			maskIndent           = 0b0000_0000_0000_1111 // three bits; 0-15
			maskBinaryNextLine   = 0b0000_0000_0001_0000
			maskSwitchCaseIndent = 0b0000_0000_0010_0000
			maskSpaceRedirects   = 0b0000_0000_0100_0000
			maskKeepPadding      = 0b0000_0000_1000_0000
			maskMinify           = 0b0000_0001_0000_0000
			maskSingleLine       = 0b0000_0010_0000_0000
			maskFunctionNextLine = 0b0000_0100_0000_0000
		)

		parser := syntax.NewParser()
		lang := syntax.LangVariant(parserOpts & maskLangVariant) // range 0-3
		syntax.Variant(lang)(parser)
		syntax.KeepComments(parserOpts&maskKeepComments != 0)(parser)

		prog, err := parser.Parse(strings.NewReader(src), "")
		if err != nil {
			t.Skip() // not valid shell syntax
		}

		if parserOpts&maskSimplify != 0 {
			syntax.Simplify(prog)
		}

		printer := syntax.NewPrinter()
		indent := uint(printerOpts & maskIndent) // range 0-15
		syntax.Indent(indent)(printer)
		syntax.BinaryNextLine(printerOpts&maskBinaryNextLine != 0)(printer)
		syntax.SwitchCaseIndent(printerOpts&maskSwitchCaseIndent != 0)(printer)
		syntax.SpaceRedirects(printerOpts&maskSpaceRedirects != 0)(printer)
		syntax.KeepPadding(printerOpts&maskKeepPadding != 0)(printer)
		syntax.Minify(printerOpts&maskMinify != 0)(printer)
		syntax.SingleLine(printerOpts&maskSingleLine != 0)(printer)
		syntax.FunctionNextLine(printerOpts&maskFunctionNextLine != 0)(printer)

		if err := printer.Print(ioutil.Discard, prog); err != nil {
			t.Skip() // e.g. invalid option
		}
	})
}
