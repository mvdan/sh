// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package pattern

import (
	"fmt"
	"regexp/syntax"
	"testing"
)

var translateTests = []struct {
	pattern string
	greedy  bool
	want    string
	wantErr bool
}{
	{``, false, ``, false},
	{`foo`, false, `foo`, false},
	{`.`, false, `\.`, false},
	{`foo*`, false, `foo.*?`, false},
	{`foo*`, true, `foo.*`, false},
	{`\*`, false, `\*`, false},
	{`\`, false, "", true},
	{`?`, false, `.`, false},
	{`\a`, false, `a`, false},
	{`(`, false, `\(`, false},
	{`a|b`, false, `a\|b`, false},
	{`x{3}`, false, `x\{3\}`, false},
	{`[a]`, false, `[a]`, false},
	{`[abc]`, false, `[abc]`, false},
	{`[^bc]`, false, `[^bc]`, false},
	{`[!bc]`, false, `[^bc]`, false},
	{`[[]`, false, `[[]`, false},
	{`[]]`, false, `[]]`, false},
	{`[^]]`, false, `[^]]`, false},
	{`[`, false, "", true},
	{`[]`, false, "", true},
	{`[^]`, false, "", true},
	{`[ab`, false, "", true},
	{`[a-]`, false, `[a-]`, false},
	{`[z-a]`, false, "", true},
	{`[a-a]`, false, "[a-a]", false},
	{`[aa]`, false, `[aa]`, false},
	{`[0-4A-Z]`, false, `[0-4A-Z]`, false},
	{`[-a]`, false, "[-a]", false},
	{`[^-a]`, false, "[^-a]", false},
	{`[a-]`, false, "[a-]", false},
	{`[[:digit:]]`, false, `[[:digit:]]`, false},
	{`[[:`, false, "", true},
	{`[[:digit`, false, "", true},
	{`[[:wrong:]]`, false, "", true},
	{`[[=x=]]`, false, "", true},
	{`[[.x.]]`, false, "", true},
}

func TestRegexp(t *testing.T) {
	t.Parallel()
	for i, tc := range translateTests {
		t.Run(fmt.Sprintf("%02d", i), func(t *testing.T) {
			got, gotErr := Regexp(tc.pattern, tc.greedy)
			if tc.wantErr && gotErr == nil {
				t.Fatalf("(%q, %v) did not error", tc.pattern, tc.greedy)
			}
			if !tc.wantErr && gotErr != nil {
				t.Fatalf("(%q, %v) errored with %q", tc.pattern, tc.greedy, gotErr)
			}
			if got != tc.want {
				t.Fatalf("(%q, %v) got %q, wanted %q", tc.pattern, tc.greedy, got, tc.want)
			}
			_, rxErr := syntax.Parse(got, syntax.Perl)
			if gotErr == nil && rxErr != nil {
				t.Fatalf("regexp/syntax.Parse(%q) failed with %q", got, rxErr)
			}
		})
	}
}

var metaTests = []struct {
	pat       string
	wantHas   bool
	wantQuote string
}{
	{``, false, ``},
	{`foo`, false, `foo`},
	{`.`, false, `.`},
	{`*`, true, `\*`},
	{`foo?`, true, `foo\?`},
	{`\[`, false, `\\\[`},
}

func TestMeta(t *testing.T) {
	t.Parallel()
	for _, tc := range metaTests {
		if got := HasMeta(tc.pat); got != tc.wantHas {
			t.Errorf("HasMeta(%q) got %t, wanted %t",
				tc.pat, got, tc.wantHas)
		}
		if got := QuoteMeta(tc.pat); got != tc.wantQuote {
			t.Errorf("QuoteMeta(%q) got %q, wanted %q",
				tc.pat, got, tc.wantQuote)
		}
	}
}
