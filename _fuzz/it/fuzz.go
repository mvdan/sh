// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

// +build gofuzz

package sh

import (
	"bytes"
	"io/ioutil"

	"mvdan.cc/sh/v3/syntax"
)

func Fuzz(data []byte) int {
	// first byte masks for parser/printer options:
	//   1: posix, not bash
	//   2: mksh, not bash
	//   4: keep comments
	//   8: simplify
	//  16: indent with spaces
	//  32: binary next line
	//  64: switch case indent
	// 128: keep padding
	if len(data) < 1 {
		return 0
	}
	opts, src := data[0], data[1:]
	parser := syntax.NewParser()
	lang := syntax.LangBash
	if opts&0x01 != 0 {
		lang = syntax.LangPOSIX
	} else if opts&0x02 != 0 {
		lang = syntax.LangMirBSDKorn
	}
	syntax.Variant(lang)(parser)
	if opts&0x04 != 0 {
		syntax.KeepComments(true)(parser)
	}
	prog, err := parser.Parse(bytes.NewReader(src), "")
	if err != nil {
		return 0
	}
	if opts&0x08 != 0 {
		syntax.Simplify(prog)
	}
	printer := syntax.NewPrinter()
	if opts&0x10 != 0 {
		syntax.Indent(4)(printer)
	}
	if opts&0x20 != 0 {
		syntax.BinaryNextLine(true)(printer)
	}
	if opts&0x40 != 0 {
		syntax.SwitchCaseIndent(true)(printer)
	}
	if opts&0x80 != 0 {
		syntax.KeepPadding(true)(printer)
	}
	printer.Print(ioutil.Discard, prog)
	return 1
}
