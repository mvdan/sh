// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

// +build gofuzz

package syntax

import (
	"bytes"
	"encoding/binary"
	"io/ioutil"
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

	parser := NewParser()
	lang := LangVariant(parserOpts & maskLangVariant) // range 0-3
	Variant(lang)(parser)
	KeepComments(parserOpts&maskKeepComments != 0)(parser)

	prog, err := parser.Parse(bytes.NewReader(src), "")
	if err != nil {
		return 0
	}

	if parserOpts&maskSimplify != 0 {
		Simplify(prog)
	}

	printer := NewPrinter()
	indent := uint(printerOpts & maskIndent) // range 0-15
	Indent(indent)(printer)
	BinaryNextLine(printerOpts&maskBinaryNextLine != 0)(printer)
	SwitchCaseIndent(printerOpts&maskSwitchCaseIndent != 0)(printer)
	SpaceRedirects(printerOpts&maskSpaceRedirects != 0)(printer)
	KeepPadding(printerOpts&maskKeepPadding != 0)(printer)
	Minify(printerOpts&maskMinify != 0)(printer)
	SingleLine(printerOpts&maskSingleLine != 0)(printer)
	FunctionNextLine(printerOpts&maskFunctionNextLine != 0)(printer)

	printer.Print(ioutil.Discard, prog)

	return 1
}
