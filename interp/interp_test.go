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
		// no-op programs
		{"", ""},
		{"true", ""},
		{":", ""},

		// exit codes
		{"false", "exit code 1"},
		{"false; true", ""},

		// echo
		{"echo foo", "foo\n"},

		// if
		{
			"if true; then echo foo; fi",
			"foo\n",
		},
		{
			"if false; then echo foo; fi",
			"",
		},
		{
			"if true; then echo foo; else echo bar; fi",
			"foo\n",
		},
		{
			"if false; then echo foo; else echo bar; fi",
			"bar\n",
		},
		{
			"if true; then false; fi",
			"exit code 1",
		},
		{
			"if false; then :; else false; fi",
			"exit code 1",
		},
		{
			"if false; then :; elif true; then echo foo; fi",
			"foo\n",
		},
		{
			"if false; then :; elif false; then :; elif true; then echo foo; fi",
			"foo\n",
		},
		{
			"if false; then :; elif false; then :; else echo foo; fi",
			"foo\n",
		},
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
