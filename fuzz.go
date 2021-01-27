// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

// +build gofuzz

package sh

import (
	"bytes"
	"encoding/binary"
	"io/ioutil"

	"mvdan.cc/sh/v3/syntax"
)

func Fuzz(data []byte) int {
	// The first byte contains parser options.
	// The second and third bytes contain printer options.
	// Below are the bit masks for them.
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

	if len(data) < 3 {
		return 0
	}
	parserOpts := data[0]
	printerOpts := binary.BigEndian.Uint16(data[1:3])
	src := data[3:]

	parser := syntax.NewParser()
	lang := syntax.LangVariant(parserOpts & maskLangVariant) // range 0-3
	syntax.Variant(lang)(parser)
	syntax.KeepComments(parserOpts&maskKeepComments != 0)(parser)

	prog, err := parser.Parse(bytes.NewReader(src), "")
	if err != nil {
		return 0
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

	printer.Print(ioutil.Discard, prog)

	return 1
}
