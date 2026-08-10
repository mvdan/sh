// Copyright (c) 2026, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

func TestPrintTrailingBackslashes(t *testing.T) {
	t.Parallel()
	langs := [...]syntax.LangVariant{
		syntax.LangBash,
		syntax.LangPOSIX,
		syntax.LangMirBSDKorn,
		syntax.LangBats,
		syntax.LangZsh,
	}
	for _, lang := range langs {
		t.Run(lang.String(), func(t *testing.T) {
			t.Parallel()
			for slashCount := 1; slashCount <= 4; slashCount++ {
				for _, quoted := range []bool{false, true} {
					for _, finalNewline := range []bool{false, true} {
						name := fmt.Sprintf("slashes=%d/quoted=%t/newline=%t", slashCount, quoted, finalNewline)
						t.Run(name, func(t *testing.T) {
							slashes := strings.Repeat("\\", slashCount)
							word := "foo" + slashes
							if quoted {
								word = "'" + word + "'"
							}
							in := "echo " + word
							if finalNewline {
								in += "\n"
							}

							wantSlashCount := slashCount
							wantNewlineCount := 1
							if !quoted && slashCount%2 == 1 {
								if finalNewline {
									switch slashCount {
									case 1:
										wantSlashCount = 0
									case 3:
										wantNewlineCount = 2
									}
								} else {
									wantSlashCount++
								}
							}
							wantWord := "foo" + strings.Repeat("\\", wantSlashCount)
							if quoted {
								wantWord = "'" + wantWord + "'"
							}
							want := "echo " + wantWord + strings.Repeat("\n", wantNewlineCount)

							parser := syntax.NewParser(syntax.Variant(lang))
							prog := parseProgram(t, parser, in)
							wantValue := commandArgValue(t, prog)
							once := printProgram(t, prog)
							if once != want {
								t.Fatalf("first format mismatch:\nwant: %q\ngot:  %q", want, once)
							}

							progAgain := parseProgram(t, parser, once)
							gotValue := commandArgValue(t, progAgain)
							if gotValue != wantValue {
								t.Fatalf("argument changed after formatting: want %q, got %q", wantValue, gotValue)
							}
							twice := printProgram(t, progAgain)
							if twice != once {
								t.Fatalf("second format changed output:\nonce:  %q\ntwice: %q", once, twice)
							}
						})
					}
				}
			}
		})
	}
}

func parseProgram(t *testing.T, parser *syntax.Parser, src string) *syntax.File {
	t.Helper()
	prog, err := parser.Parse(strings.NewReader(src), "")
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return prog
}

func printProgram(t *testing.T, prog *syntax.File) string {
	t.Helper()
	var buf bytes.Buffer
	if err := syntax.NewPrinter().Print(&buf, prog); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func commandArgValue(t *testing.T, prog *syntax.File) string {
	t.Helper()
	if len(prog.Stmts) != 1 {
		t.Fatalf("got %d statements, want one", len(prog.Stmts))
	}
	call, ok := prog.Stmts[0].Cmd.(*syntax.CallExpr)
	if !ok || len(call.Args) != 2 {
		t.Fatalf("got command %#v, want a call with two arguments", prog.Stmts[0].Cmd)
	}
	fields, err := expand.Fields(&expand.Config{}, call.Args[1])
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 1 {
		t.Fatalf("expanded to %d fields, want one", len(fields))
	}
	return fields[0]
}
