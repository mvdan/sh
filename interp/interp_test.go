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
		{"echo foo", "foo\n"},
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
				t.Fatalf("unexpected output:\nwant: %q\ngot:  %q",
					c.want, got)
			}
		})
	}
}
