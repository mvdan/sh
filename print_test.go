// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func strFprint(v interface{}) string {
	var buf bytes.Buffer
	Fprint(&buf, v)
	return buf.String()
}

func TestFprintOld(t *testing.T) {
	for i, c := range astTests {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			in := c.ast.(File)
			want := c.strs[0] + "\n"
			got := strFprint(in)
			if got != want {
				t.Fatalf("AST print mismatch\nwant: %s\ngot:  %s",
					want, got)
			}
		})
	}
}

func parsePath(tb testing.TB, path string) File {
	f, err := os.Open(path)
	if err != nil {
		tb.Fatal(err)
	}
	defer f.Close()
	prog, err := Parse(f, "")
	if err != nil {
		tb.Fatal(err)
	}
	return prog
}

var printDir = filepath.Join("testdata", "print")

func TestFprint(t *testing.T) {
	outpaths, err := filepath.Glob(filepath.Join(printDir, "*.out.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, outPath := range outpaths {
		fullName := outPath[:len(outPath)-len(".out.sh")]
		name := fullName[len(printDir)+1:]
		t.Run(name, func(t *testing.T) {
			inPath := fullName + ".in.sh"
			prog := parsePath(t, inPath)
			got := strFprint(prog)

			outb, err := ioutil.ReadFile(outPath)
			if err != nil {
				t.Fatal(err)
			}
			want := string(outb)

			if got != want {
				t.Fatalf("Output mismatch in Fprint:\nwant:\n%s\ngot:\n%s",
					want, got)
			}
		})
	}
}

var errBadWriter = fmt.Errorf("write: expected error")

type badWriter struct{}

func (b badWriter) Write(p []byte) (int, error) { return 0, errBadWriter }

func TestWriteErr(t *testing.T) {
	var out badWriter
	f := File{
		Stmts: []Stmt{
			{
				Redirs: []Redirect{{}},
				Node:   Subshell{},
			},
		},
	}
	err := Fprint(out, f)
	if err == nil {
		t.Fatalf("Expected error with bad writer")
	}
	if err != errBadWriter {
		t.Fatalf("Error mismatch with bad writer:\nwant: %v\ngot:  %v",
			errBadWriter, err)
	}
}
