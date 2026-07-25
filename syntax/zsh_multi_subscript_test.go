package syntax

import (
	"strings"
	"testing"
)

func TestZshMultiSubscript(t *testing.T) {
	t.Parallel()
	// https://github.com/mvdan/sh/issues/1361
	in := "echo ${var[1][2]}\n"
	f, err := NewParser(Variant(LangZsh)).Parse(strings.NewReader(in), "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var buf strings.Builder
	if err := NewPrinter().Print(&buf, f); err != nil {
		t.Fatalf("print: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "${var[1][2]}") {
		t.Fatalf("printed %q, want multi-subscript preserved", out)
	}
}

func TestZshMultiSubscriptShort(t *testing.T) {
	t.Parallel()
	in := "echo $var[1][2]\n"
	f, err := NewParser(Variant(LangZsh)).Parse(strings.NewReader(in), "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Walk to ParamExp and check End covers second subscript.
	var pe *ParamExp
	Walk(f, func(node Node) bool {
		if p, ok := node.(*ParamExp); ok {
			pe = p
			return false
		}
		return true
	})
	if pe == nil {
		t.Fatal("ParamExp not found")
	}
	if len(pe.ExtraIndexes) != 1 {
		t.Fatalf("ExtraIndexes=%d, want 1", len(pe.ExtraIndexes))
	}
	got := in[pe.Pos().Offset():pe.End().Offset()]
	if got != "$var[1][2]" {
		t.Fatalf("span %q, want $var[1][2]", got)
	}
	var buf strings.Builder
	if err := NewPrinter().Print(&buf, f); err != nil {
		t.Fatalf("print: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "var[1][2]") && !strings.Contains(out, "${var[1][2]}") {
		t.Fatalf("printed %q, want multi-subscript preserved", out)
	}
}
