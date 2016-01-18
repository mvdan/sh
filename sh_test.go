// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	paths, err := filepath.Glob("testdata/*.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		testParse(t, path)
	}
}

func testParse(t *testing.T, path string) {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := parse(f, path); err != nil {
		t.Fatalf("Parse error: %v", err)
	}
}

func TestParseErr(t *testing.T) {
	errs := []string{
		"'",
		"\"",
		";",
		"{",
		"{{}",
		"}",
		"{}}",
		"{#}",
		"{}(){}",
		"=",
		"&",
		"|",
		"foo;;",
		"foo(",
		"foo'",
		"foo()",
		"foo() {",
		"foo &&",
		"foo |",
		"foo ||",
		"foo >",
		"foo >>",
		"foo >&",
		"foo <",
	}
	for _, s := range errs {
		r := strings.NewReader(s)
		if err := parse(r, "stdin.go"); err == nil {
			t.Fatalf("Expected error in: %s", s)
		}
	}
}
