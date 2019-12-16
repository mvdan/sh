// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package pattern

import (
	"fmt"
	"regexp/syntax"
	"testing"
)

var translateTests = []struct {
	pat     string
	mode    Mode
	want    string
	wantErr bool
}{
	{pat: ``, want: ``},
	{pat: `foo`, want: `foo`},
	{pat: `foóà中`, mode: Filenames | Braces, want: `foóà中`},
	{pat: `.`, want: `\.`},
	{pat: `foo*`, want: `foo.*`},
	{pat: `foo*`, mode: Shortest, want: `foo.*?`},
	{pat: `foo*`, mode: Shortest | Filenames, want: `foo[^/]*?`},
	{pat: `*foo`, mode: Filenames, want: `[^/]*foo`},
	{pat: `**`, want: `.*.*`},
	{pat: `**`, mode: Filenames, want: `.*`},
	{pat: `/**/foo`, want: `/.*.*/foo`},
	{pat: `/**/foo`, mode: Filenames, want: `/(.*/|)foo`},
	{pat: `/**/à`, mode: Filenames, want: `/(.*/|)à`},
	{pat: `/**foo`, mode: Filenames, want: `/.*foo`},
	{pat: `\*`, want: `\*`},
	{pat: `\`, wantErr: true},
	{pat: `?`, want: `.`},
	{pat: `?`, mode: Filenames, want: `[^/]`},
	{pat: `?à`, want: `.à`},
	{pat: `\a`, want: `a`},
	{pat: `(`, want: `\(`},
	{pat: `a|b`, want: `a\|b`},
	{pat: `x{3}`, want: `x\{3\}`},
	{pat: `{3,4}`, want: `\{3,4\}`},
	{pat: `{3,4}`, mode: Braces, want: `(?:3|4)`},
	{pat: `{3,`, want: `\{3,`},
	{pat: `{3,`, mode: Braces, want: `\{3,`},
	{pat: `{3,{4}`, mode: Braces, want: `\{3,\{4\}`},
	{pat: `{3,{4}}`, mode: Braces, want: `(?:3|\{4\})`},
	{pat: `{3,{4,[56]}}`, mode: Braces, want: `(?:3|(?:4|[56]))`},
	{pat: `{3..5}`, mode: Braces, want: `(?:3|4|5)`},
	{pat: `{9..12}`, mode: Braces, want: `(?:9|10|11|12)`},
	{pat: `[a]`, want: `[a]`},
	{pat: `[abc]`, want: `[abc]`},
	{pat: `[^bc]`, want: `[^bc]`},
	{pat: `[!bc]`, want: `[^bc]`},
	{pat: `[[]`, want: `[[]`},
	{pat: `[\]]`, want: `[\]]`},
	{pat: `[\]]`, mode: Filenames, want: `[\]]`},
	{pat: `[]]`, want: `[]]`},
	{pat: `[!]]`, want: `[^]]`},
	{pat: `[^]]`, want: `[^]]`},
	{pat: `[a/b]`, want: `[a/b]`},
	{pat: `[a/b]`, mode: Filenames, want: `\[a/b\]`},
	{pat: `[`, wantErr: true},
	{pat: `[\`, wantErr: true},
	{pat: `[^`, wantErr: true},
	{pat: `[!`, wantErr: true},
	{pat: `[!bc]`, want: `[^bc]`},
	{pat: `[]`, wantErr: true},
	{pat: `[^]`, wantErr: true},
	{pat: `[!]`, wantErr: true},
	{pat: `[ab`, wantErr: true},
	{pat: `[a-]`, want: `[a-]`},
	{pat: `[z-a]`, wantErr: true},
	{pat: `[a-a]`, want: `[a-a]`},
	{pat: `[aa]`, want: `[aa]`},
	{pat: `[0-4A-Z]`, want: `[0-4A-Z]`},
	{pat: `[-a]`, want: `[-a]`},
	{pat: `[^-a]`, want: `[^-a]`},
	{pat: `[a-]`, want: `[a-]`},
	{pat: `[[:digit:]]`, want: `[[:digit:]]`},
	{pat: `[[:`, wantErr: true},
	{pat: `[[:digit`, wantErr: true},
	{pat: `[[:wrong:]]`, wantErr: true},
	{pat: `[[=x=]]`, wantErr: true},
	{pat: `[[.x.]]`, wantErr: true},
}

func TestRegexp(t *testing.T) {
	t.Parallel()
	for i, tc := range translateTests {
		t.Run(fmt.Sprintf("%02d", i), func(t *testing.T) {
			got, gotErr := Regexp(tc.pat, tc.mode)
			if tc.wantErr && gotErr == nil {
				t.Fatalf("(%q, %b) did not error", tc.pat, tc.mode)
			}
			if !tc.wantErr && gotErr != nil {
				t.Fatalf("(%q, %b) errored with %q", tc.pat, tc.mode, gotErr)
			}
			if got != tc.want {
				t.Fatalf("(%q, %b) got %q, wanted %q", tc.pat, tc.mode, got, tc.want)
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
	mode      Mode
	wantHas   bool
	wantQuote string
}{
	{``, 0, false, ``},
	{`foo`, 0, false, `foo`},
	{`.`, 0, false, `.`},
	{`*`, 0, true, `\*`},
	{`*`, Shortest | Filenames, true, `\*`},
	{`foo?`, 0, true, `foo\?`},
	{`\[`, 0, false, `\\\[`},
	{`{`, 0, false, `{`},
	{`{`, Braces, true, `\{`},
}

func TestMeta(t *testing.T) {
	t.Parallel()
	for _, tc := range metaTests {
		if got := HasMeta(tc.pat, tc.mode); got != tc.wantHas {
			t.Errorf("HasMeta(%q, %b) got %t, wanted %t",
				tc.pat, tc.mode, got, tc.wantHas)
		}
		if got := QuoteMeta(tc.pat, tc.mode); got != tc.wantQuote {
			t.Errorf("QuoteMeta(%q, %b) got %q, wanted %q",
				tc.pat, tc.mode, got, tc.wantQuote)
		}
	}
}
