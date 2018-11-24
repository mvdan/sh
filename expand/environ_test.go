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
			pairs: []string{"A=b", "A=x", "c=", "c=y"},
			want:  []string{"A=x", "c=y"},
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
