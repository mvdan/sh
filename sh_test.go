// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"os"
	"path/filepath"
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
	println(path)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := parse(f, path); err != nil {
		t.Fatalf("Parse error: %v", err)
	}
}
