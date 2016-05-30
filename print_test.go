// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"bytes"
	"fmt"
	"testing"
)

func TestFprint(t *testing.T) {
	for i, c := range astTests {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			in := c.ast.(File)
			want := c.strs[0]
			var buf bytes.Buffer
			Fprint(&buf, in)
			got := buf.String()
			if got != want {
				t.Fatalf("AST print mismatch\nwant: %s\ngot:  %s",
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
