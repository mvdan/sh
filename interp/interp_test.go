// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/mvdan/sh/syntax"
)

func TestFile(t *testing.T) {
	cases := []struct {
		prog, want string
	}{
		{"", ""},
		{"true", ""},
		{"false", "exit code 1"},
		{"false; true", ""},
		{"echo foo", "foo\n"},
		{"if true; then echo foo; fi", "foo\n"},
		{"if false; then echo foo; fi", ""},
	}
	for i, c := range cases {
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			file, err := syntax.Parse(strings.NewReader(c.prog), "", 0)
			if err != nil {
				t.Fatalf("could not parse: %v", err)
			}
			var buf bytes.Buffer
			r := Runner{
				File:   file,
				Stdout: &buf,
			}
			if err := r.Run(); err != nil {
				buf.WriteString(err.Error())
			}
			if got := buf.String(); got != c.want {
				t.Fatalf("wrong output in %q:\nwant: %q\ngot:  %q",
					c.prog, c.want, got)
			}
		})
	}
}
