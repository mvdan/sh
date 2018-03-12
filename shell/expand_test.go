// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package shell

import (
	"fmt"
	"os"
	"testing"
)

var expandTests = []struct {
	in   string
	env  func(string) string
	want string
}{
	{"foo", nil, "foo"},
	{"\nfoo\n", nil, "\nfoo\n"},
	{"a-$b-c", nil, "a--c"},
	{"a-$b-c", func(string) string { return "" }, "a--c"},
	{
		"a-$b-c",
		func(name string) string {
			return name + "_val"
		},
		"a-b_val-c",
	},
	{"${x//o/a}", func(string) string { return "foo" }, "faa"},
	{"${INTERP_GLOBAL:+hasOsEnv}", nil, "hasOsEnv"},
}

func TestExpand(t *testing.T) {
	os.Setenv("INTERP_GLOBAL", "value")
	for i := range expandTests {
		t.Run(fmt.Sprintf("%02d", i), func(t *testing.T) {
			tc := expandTests[i]
			t.Parallel()
			got, err := Expand(tc.in, tc.env)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("\nwant: %q\ngot:  %q", tc.want, got)
			}
		})
	}
}
