// Copyright (c) 2026, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"io"
	"os/exec"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/go-quicktest/qt"
)

func TestZshSubscripts(t *testing.T) {
	t.Parallel()
	inputs := []string{
		`typeset -A h; h[foo-bar]=1; print -r -- $h[foo-bar]`,
		`typeset -A h; h[a/b]=2; print -r -- "${h[a/b]}"`,
		`typeset -A h; h[a:b]=3; print -r -- "${h[a:b]}"`,
		`typeset -A h; h[a+b]=4; print -r -- "${h[a+b]}"`,
		`typeset -A h; h[-n]=5; print -r -- "$h[-n]"`,
		`typeset -A h; h=("a - b" 6 a-b 7); print -r -- "${h[a - b]}" "${h[a-b]}"`,
		`typeset -A h; h[foo-bar]=8; print -r -- $(( $+h[foo-bar] ))`,
		`a=(one two three); i=1; print -r -- "${a[i+1]}" "${a[1,2]}" "${a[-1]}"`,
		`a=(one two three); print -r -- "${a[$((1+1))]}" "${a[$(print 2)]}"`,
		`a=(one two three); b=(2); print -r -- "${a[b[1]]}" "${a[${b[1]}]}"`,
		`a=(one two three); print -r -- "${a[(r)t*]}"`,
		`typeset -A h; h=("a]b" 9); print -r -- "${h[a\]b]}"`,
	}
	key := strings.Repeat("key-", 2048) + "end"
	inputs = append(inputs, "typeset -A h; h["+key+"]=10; print -r -- ${h["+key+"]}")
	for _, src := range inputs {
		t.Run(src, func(t *testing.T) {
			for _, opts := range [][]PrinterOption{nil, {Minify(true)}, {SingleLine(true)}} {
				node, err := NewParser(Variant(LangZsh), ZshSubscriptsAsWords(true)).Parse(iotest.OneByteReader(strings.NewReader(src)), "")
				qt.Assert(t, qt.IsNil(err))
				out, err := strPrint(NewPrinter(opts...), node)
				qt.Assert(t, qt.IsNil(err))
				again, err := NewParser(Variant(LangZsh), ZshSubscriptsAsWords(true)).Parse(strings.NewReader(out), "")
				qt.Assert(t, qt.IsNil(err), qt.Commentf("formatted: %s", out))
				out2, err := strPrint(NewPrinter(opts...), again)
				qt.Assert(t, qt.IsNil(err))
				qt.Assert(t, qt.Equals(out2, out))
				t.Run("zsh", func(t *testing.T) {
					external := externalShells[LangZsh]
					external.require(t)
					type result struct {
						stdout, stderr string
						status         int
					}
					run := func(source string) result {
						t.Helper()
						cmd := exec.Command(external.cmd, "-f", "-c", source)
						var stdout, stderr strings.Builder
						cmd.Stdout, cmd.Stderr = &stdout, &stderr
						err := cmd.Run()
						qt.Assert(t, qt.IsNotNil(cmd.ProcessState), qt.Commentf("run: %v", err))
						status := cmd.ProcessState.ExitCode()
						qt.Assert(t, qt.IsTrue(status >= 0), qt.Commentf("run: %v", err))
						return result{stdout.String(), stderr.String(), status}
					}
					before, after := run(src), run(out)
					qt.Assert(t, qt.Equals(after, before), qt.Commentf("formatted: %s", out))
				})
			}
		})
	}
}

func TestZshSubscriptsAsWords(t *testing.T) {
	t.Parallel()
	for _, lang := range []LangVariant{LangZsh, LangBash, LangMirBSDKorn} {
		t.Run(lang.String(), func(t *testing.T) {
			parser := NewParser(Variant(lang))
			for _, enabled := range []bool{false, true, false} {
				ZshSubscriptsAsWords(enabled)(parser)
				var index ArithmExpr = &BinaryArithm{Op: Sub, X: litWord("foo"), Y: litWord("bar")}
				if lang == LangZsh && enabled {
					index = litWord("foo-bar")
				}
				want := fullProg(&CallExpr{Assigns: []*Assign{{Name: lit("h"), Index: index, Value: litWord("1")}}})
				singleParse(parser, "h[foo-bar]=1", want)(t)
			}
		})
	}
	for _, key := range []string{"foo-bar", "a/b", "a:b", "a+b", "a - b", "a[1]", `a\]b`} {
		parser := NewParser(Variant(LangZsh), ZshSubscriptsAsWords(true))
		singleParse(parser, "${h["+key+"]}", fullProg(&ParamExp{Param: lit("h"), Index: litWord(key)}))(t)
	}
}

func FuzzZshSubscripts(f *testing.F) {
	for _, source := range []string{
		"h[foo-bar]=1", "${h[a:b]}", "${h[a - b]}", "${a[b[1]]}",
		"${a[${b[1]}]}", "${a[$(echo 1)]}", "${a[$((1+1))]}",
		`${h[a\]b]}`, "${a[(r)t*]}", "${a[", "h[]",
	} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) {
		parser := NewParser(Variant(LangZsh), ZshSubscriptsAsWords(true), KeepComments(true))
		node, err := parser.Parse(strings.NewReader(source), "")
		if err != nil {
			t.Skip()
		}
		// Match FuzzParsePrint's invariant: parsing and printing arbitrary
		// accepted source must not panic. Fixed cases above check idempotence.
		for _, opts := range [][]PrinterOption{nil, {Minify(true)}} {
			qt.Assert(t, qt.IsNil(NewPrinter(opts...).Print(io.Discard, node)))
		}
	})
}
