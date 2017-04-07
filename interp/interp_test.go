// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/mvdan/sh/syntax"
)

var fileCases = []struct {
	in, want string
}{
	// no-op programs
	{"", ""},
	{"true", ""},
	{":", ""},
	{"exit", ""},
	{"exit 0", ""},

	// exit codes
	{"exit 1", "exit status 1"},
	{"exit -1", "exit status 255"},
	{"exit 300", "exit status 44"},
	{"exit a", `strconv.Atoi: parsing "a": invalid syntax #NOCONFIRM`},
	{"exit 1 2", "1:1: exit cannot take multiple arguments #NOCONFIRM"},
	{"false", "exit status 1"},
	{"false; true", ""},
	{"false; exit", "exit status 1"},

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
		"exit status 1",
	},
	{
		"if false; then :; else false; fi",
		"exit status 1",
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

func TestFile(t *testing.T) {
	for i, c := range fileCases {
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			file, err := syntax.Parse(strings.NewReader(c.in), "", 0)
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
			want := c.want
			if i := strings.Index(want, " #NOCONFIRM"); i >= 0 {
				want = want[:i]
			}
			if got := buf.String(); got != want {
				t.Fatalf("wrong output in %q:\nwant: %q\ngot:  %q",
					c.in, want, got)
			}
		})
	}
}

func TestFileConfirm(t *testing.T) {
	for i, c := range fileCases {
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			if strings.Contains(c.want, " #NOCONFIRM") {
				return
			}
			cmd := exec.Command("bash")
			cmd.Stdin = strings.NewReader(c.in)
			out, err := cmd.CombinedOutput()
			got := string(out)
			if err != nil {
				got += err.Error()
			}
			if got != c.want {
				t.Fatalf("wrong output in %q:\nwant: %q\ngot:  %q",
					c.in, c.want, got)
			}
		})
	}
}
