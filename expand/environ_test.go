// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package expand

import (
	"reflect"
	"testing"
)

func TestListEnviron(t *testing.T) {
	tests := []struct {
		name  string
		upper bool
		pairs []string
		want  []string
	}{
		{
			name:  "Empty",
			pairs: nil,
			want:  []string{},
		},
		{
			name:  "Simple",
			pairs: []string{"A=b", "c="},
			want:  []string{"A=b", "c="},
		},
		{
			name:  "MissingEqual",
			pairs: []string{"A=b", "invalid", "c="},
			want:  []string{"A=b", "c="},
		},
		{
			name:  "DuplicateNames",
			pairs: []string{"A=x", "A=b", "c=", "c=y"},
			want:  []string{"A=b", "c=y"},
		},
		{
			name:  "NoName",
			pairs: []string{"=b", "=c"},
			want:  []string{},
		},
		{
			name:  "EmptyElements",
			pairs: []string{"A=b", "", "", "c="},
			want:  []string{"A=b", "c="},
		},
		{
			name:  "MixedCaseNoUpper",
			pairs: []string{"A=b1", "Path=foo", "a=b2"},
			want:  []string{"A=b1", "Path=foo", "a=b2"},
		},
		{
			name:  "MixedCaseUpper",
			upper: true,
			pairs: []string{"A=b1", "Path=foo", "a=b2"},
			want:  []string{"A=b2", "PATH=foo"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotEnv := listEnvironWithUpper(tc.upper, tc.pairs...)
			got := []string(gotEnv.(listEnviron))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ListEnviron(%t, %q) wanted %q, got %q",
					tc.upper, tc.pairs, tc.want, got)
			}
		})
	}
}

func TestGetWithSameSubPrefix(t *testing.T) {
	gotEnv := ListEnviron("GREETING=text1", "GREETING2=text2")
	got := gotEnv.Get("GREETING2").String()
	if got != "text2" {
		t.Fatalf("ListEnviron.Get(GREETING2) wanted text2, got %q", got)
	}
	got = gotEnv.Get("GREETING").String()
	if got != "text1" {
		t.Fatalf("ListEnviron.Get(GREETING) wanted text1, got %q", got)
	}
}
