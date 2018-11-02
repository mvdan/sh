// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package expand

import (
	"reflect"
	"testing"
)

func TestListEnviron(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"Empty", nil, []string{}},
		{
			"Simple",
			[]string{"A=b", "c="},
			[]string{"A=b", "c="},
		},
		{
			"MissingEqual",
			[]string{"A=b", "invalid", "c="},
			[]string{"A=b", "c="},
		},
		{
			"DuplicateNames",
			[]string{"A=b", "A=x", "c=", "c=y"},
			[]string{"A=x", "c=y"},
		},
		{
			"NoName",
			[]string{"=b", "=c"},
			[]string{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotEnv := ListEnviron(tc.in...)
			got := []string(gotEnv.(listEnviron))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ListEnviron(%q) wanted %q, got %q",
					tc.in, tc.want, got)
			}
		})
	}
}
