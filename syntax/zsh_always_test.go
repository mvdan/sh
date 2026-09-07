// Copyright (c) 2026, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"
)

func TestZshAlways(t *testing.T) {
	t.Parallel()
	inputs := []string{
		"{ print try; } always { print cleanup; }",
		"{} always {}",
		"{ :; } always\n{ :; }",
		"{ :; } always # comment\n{ :; }",
		"{ } always { }",
		"{\n# try\nprint try\n} always {\n# cleanup\nprint cleanup\n}",
		"{ { print nested; } always { print inner; }; } always { print outer; }",
		"f() { { return 7; } always { print cleanup; }; }; f; print $?",
		"f() { { return 7; } always { print cleanup; }; }; f",
		"for x in a b; do\n{\nprint $x\nbreak\n} always {\nprint cleanup\n}\ndone",
		"for x in a b; do\n{\nprint $x\ncontinue\n} always {\nprint cleanup\n}\ndone",
		"{ print redirected; } always { print cleanup; } >/dev/null",
		"{ cat <<EOF\ntry\nEOF\n} always { print cleanup; }",
	}
	for _, src := range inputs {
		t.Run(src, func(t *testing.T) {
			for _, opts := range [][]PrinterOption{nil, {Minify(true)}, {SingleLine(true)}, {KeepPadding(true)}, {BinaryNextLine(true)}, {FunctionNextLine(true)}} {
				parser := NewParser(Variant(LangZsh), KeepComments(true))
				node, err := parser.Parse(strings.NewReader(src), "")
				qt.Assert(t, qt.IsNil(err))
				out, err := strPrint(NewPrinter(opts...), node)
				qt.Assert(t, qt.IsNil(err))
				again, err := parser.Parse(strings.NewReader(out), "")
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
	for _, src := range []string{"{ :; } always", "{ :; } always echo bad", "{ :; } always {", "{ :; } always { :; } always { :; }"} {
		_, err := NewParser(Variant(LangZsh)).Parse(strings.NewReader(src), "")
		qt.Assert(t, qt.IsNotNil(err), qt.Commentf("source: %s", src))
	}
	for _, lang := range []LangVariant{LangBash, LangPOSIX, LangMirBSDKorn} {
		_, err := NewParser(Variant(lang)).Parse(strings.NewReader("{ :; } always { :; }"), "")
		qt.Assert(t, qt.IsNotNil(err), qt.Commentf("language: %s", lang))
	}
}
