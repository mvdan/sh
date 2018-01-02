// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import "testing"

var translateTests = []struct {
	pattern string
	greedy  bool
	want    string
	wantErr bool
}{
	{``, false, ``, false},
	{`foo`, false, `foo`, false},
	{`foo*`, false, `foo.*?`, false},
	{`foo*`, true, `foo.*`, false},
	{`\*`, false, `\*`, false},
	{`?`, false, `.`, false},
	{`[abc]`, false, `[abc]`, false},
	{`[^bc]`, false, `[^bc]`, false},
	{`[!bc]`, false, `[^bc]`, false},
	{`[[]`, false, `[[]`, false},
	{`[]]`, false, `[]]`, false},
	{`[`, false, "", true},
	{`[ab`, false, "", true},
	{`[[:digit:]]`, false, `[[:digit:]]`, false},
	{`[[:`, false, "", true},
	{`[[:digit`, false, "", true},
	{`[[:wrong:]]`, false, "", true},
}

func TestTranslatePattern(t *testing.T) {
	for _, tc := range translateTests {
		got, gotErr := TranslatePattern(tc.pattern, tc.greedy)
		if tc.wantErr && gotErr == nil {
			t.Errorf("TranslatePattern(%q, %v) did not error",
				tc.pattern, tc.greedy)
		} else if !tc.wantErr && gotErr != nil {
			t.Errorf("TranslatePattern(%q, %v) errored with %q",
				tc.pattern, tc.greedy, gotErr)
		}
		if got != tc.want {
			t.Errorf("TranslatePattern(%q, %v) got %q, wanted %q",
				tc.pattern, tc.greedy, got, tc.want)
		}
	}
}
