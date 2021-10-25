package interp

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

// tracer prints expressions like a shell would do if its
// options '-o' is set to either 'xtrace' or its shorthand, '-x'.
type tracer struct {
	buf          bytes.Buffer
	printer      *syntax.Printer
	stdout       io.Writer
	isFirstPrint bool
}

func (r *Runner) tracer() *tracer {
	if !r.opts[optXTrace] {
		return nil
	}

	return &tracer{
		printer:      syntax.NewPrinter(),
		stdout:       r.stdout,
		isFirstPrint: true,
	}
}

func (t *tracer) setFirstPrint(isFirstPrint bool) {
	if t == nil {
		return
	}

	t.isFirstPrint = isFirstPrint
}

// string writes s to tracer.buf if tracer is non-nil,
// prepending "+" if tracer.isFirstPrint is true.
func (t *tracer) string(s string) {
	if t == nil {
		return
	}

	if t.isFirstPrint {
		t.buf.WriteString("+ ")
	}
	t.buf.WriteString(s)
}

func (t *tracer) stringf(f string, a ...interface{}) {
	if t == nil {
		return
	}

	t.string(fmt.Sprintf(f, a...))
}

// expr prints x to tracer.buf if tracer is non-nil,
// prepending "+" if tracer.isFirstPrint is true.
func (t *tracer) expr(x syntax.Node) {
	if t == nil {
		return
	}

	if t.isFirstPrint {
		t.buf.WriteString("+ ")
	}
	if err := t.printer.Print(&t.buf, x); err != nil {
		panic(err)
	}
}

// flush writes the contents of tracer.buf to the tracer.stdout.
func (t *tracer) flush() {
	if t == nil {
		return
	}

	t.stdout.Write(t.buf.Bytes())
}

// newLineFlush is like flush, but with extra new line before tracer.buf gets flushed.
func (t *tracer) newLineFlush() {
	if t == nil {
		return
	}

	t.buf.WriteString("\n")
	t.flush()
	// reset state
	t.buf.Reset()
	t.isFirstPrint = true
}

// call prints a command and its arguments with varying formats depending on the cmd type,
// for example, built-in command's arguments are printed enclosed in single quotes,
// otherwise, call defaults to printing with double quotes.
func (t *tracer) call(cmd string, args ...string) {
	if t == nil {
		return
	}

	s := strings.Join(args, " ")
	if strings.TrimSpace(s) == "" {
		// fields may be empty for function () {} declarations
		t.string(cmd)
	} else if isBuiltin(cmd) {
		if cmd == "set" {
			// TODO: only first occurence of set is not printed, succeeding calls are printed
			return
		}

		qs, err := syntax.Quote(s, syntax.LangBash)
		if err != nil { // should never happen
			panic(err)
		}
		t.stringf("%s %s", cmd, qs)
	} else {
		t.stringf("%s %s", cmd, s)
	}
}

func (t *tracer) wordParts(wp []syntax.WordPart, name string, vr expand.Variable) {
	if t == nil {
		return
	}

	for _, p := range wp {
		switch p.(type) {
		case *syntax.ArithmExp:
			t.stringf("%s=%s", name, vr)
		case *syntax.DblQuoted, *syntax.SglQuoted, *syntax.Lit:
			qs, err := syntax.Quote(vr.String(), syntax.LangBash)
			if err != nil { // should never happen
				panic(err)
			}
			t.stringf("%s=%s", name, qs)
		case *syntax.ParamExp, *syntax.ProcSubst, *syntax.ExtGlob, *syntax.BraceExp:
			// TODO
		}
	}
}
