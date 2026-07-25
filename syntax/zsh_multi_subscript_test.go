package syntax

import (
	"strings"
	"testing"
)

func TestZshMultiSubscript(t *testing.T) {
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
	in := "echo $var[1][2]\n"
	f, err := NewParser(Variant(LangZsh)).Parse(strings.NewReader(in), "")
	if err != nil {
		t.Fatalf("parse: %v", err)
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
