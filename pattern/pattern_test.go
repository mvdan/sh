// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package pattern

import (
	"fmt"
	"regexp"
	"regexp/syntax"
	"testing"

	"github.com/go-quicktest/qt"
)

var regexpTests = []struct {
	pat     string
	mode    Mode
	want    string
	wantErr bool

	mustMatch    []string
	mustNotMatch []string
}{
	{pat: ``, want: ``},
	{pat: `foo`, want: `foo`},
	{pat: `foóà中`, mode: Filenames, want: `foóà中`},
	{pat: `.`, want: `\.`},
	{pat: `foo*`, want: `(?s)foo.*`},
	{pat: `foo*`, mode: Shortest, want: `(?s)foo.*?`},
	{pat: `foo*`, mode: Shortest | Filenames, want: `foo([^/.][^/]*)??`},
	{pat: `*foo`, mode: Filenames, want: `([^/.][^/]*)?foo`},
	{
		pat: `*foo`, mode: Filenames | EntireString, want: `^([^/.][^/]*)?foo$`,
		mustMatch:    []string{"foo", "prefix-foo", "prefix.foo"},
		mustNotMatch: []string{"foo-suffix", "/prefix/foo", ".foo", ".prefix-foo"},
	},
	{pat: `**`, want: `(?s).*.*`},
	{
		pat: `**`, mode: Filenames | EntireString, want: `(?s)^(/|[^/.][^/]*)*$`,
		mustMatch:    []string{"/foo", "/prefix/foo", "/a.b.c/foo", "/a/b/c/foo", "/foo/suffix.ext"},
		mustNotMatch: []string{"/.prefix/foo", "/prefix/.foo"},
	},
	{
		pat: `**`, mode: Filenames | NoGlobStar | EntireString, want: `^([^/.][^/]*)?$`,
		mustMatch:    []string{"foo.bar"},
		mustNotMatch: []string{"foo/bar", ".foo"},
	},
	{pat: `/**/foo`, want: `(?s)/.*.*/foo`},
	{
		pat: `/**/foo`, mode: Filenames | EntireString, want: `(?s)^/((/|[^/.][^/]*)*/)?foo$`,
		mustMatch:    []string{"/foo", "/prefix/foo", "/a.b.c/foo", "/a/b/c/foo"},
		mustNotMatch: []string{"/foo/suffix", "prefix/foo", "/.prefix/foo", "/prefix/.foo"},
	},
	{pat: `/**/foo`, mode: Filenames | NoGlobStar, want: `/([^/.][^/]*)?/foo`},
	{pat: `/**/à`, mode: Filenames, want: `(?s)/((/|[^/.][^/]*)*/)?à`},
	{
		pat: `/**foo`, mode: Filenames, want: `/([^/.][^/]*)?foo`,
		// These all match because without EntireString, we match substrings.
		mustMatch: []string{"/foo", "/prefix-foo", "/foo-suffix", "/sub/foo"},
	},
	{
		pat: `/**foo`, mode: Filenames | EntireString, want: `^/([^/.][^/]*)?foo$`,
		mustMatch:    []string{"/foo", "/prefix-foo"},
		mustNotMatch: []string{"/foo-suffix", "/sub/foo", "/.foo", "/.prefix-foo"},
	},
	{
		pat: `/foo**`, mode: Filenames | EntireString, want: `^/foo([^/.][^/]*)?$`,
		mustMatch:    []string{"/foo", "/foo-suffix"},
		mustNotMatch: []string{"/prefix-foo", "/foo/sub"},
	},
	{pat: `\*`, want: `\*`},
	{pat: `\`, wantErr: true},
	{pat: `?`, want: `(?s).`},
	{pat: `?`, mode: Filenames, want: `[^/]`},
	{pat: `?à`, want: `(?s).à`},
	{pat: `\a`, want: `a`},
	{pat: `(`, want: `\(`},
	{pat: `a|b`, want: `a\|b`},
	{pat: `x{3}`, want: `x\{3\}`},
	{pat: `{3,4}`, want: `\{3,4\}`},
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
	for i, tc := range regexpTests {
		t.Run(fmt.Sprintf("%02d", i), func(t *testing.T) {
			got, gotErr := Regexp(tc.pat, tc.mode)
			if tc.wantErr && gotErr == nil {
				t.Fatalf("(%q, %#b) did not error", tc.pat, tc.mode)
			}
			if !tc.wantErr && gotErr != nil {
				t.Fatalf("(%q, %#b) errored with %q", tc.pat, tc.mode, gotErr)
			}
			if got != tc.want {
				t.Fatalf("(%q, %#b) got %q, wanted %q", tc.pat, tc.mode, got, tc.want)
			}
			_, rxErr := syntax.Parse(got, syntax.Perl)
			if gotErr == nil && rxErr != nil {
				t.Fatalf("regexp/syntax.Parse(%q) failed with %q", got, rxErr)
			}
			rx := regexp.MustCompile(got)
			for _, s := range tc.mustMatch {
				qt.Check(t, qt.IsTrue(rx.MatchString(s)), qt.Commentf("must match: %q", s))
			}
			for _, s := range tc.mustNotMatch {
				qt.Check(t, qt.IsFalse(rx.MatchString(s)), qt.Commentf("must not match: %q", s))
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
	{`{`, false, `{`},
}

func TestMeta(t *testing.T) {
	t.Parallel()
	for _, tc := range metaTests {
		if got := HasMeta(tc.pat, 0); got != tc.wantHas {
			t.Errorf("HasMeta(%q, 0) got %t, wanted %t",
				tc.pat, got, tc.wantHas)
		}
		if got := QuoteMeta(tc.pat, 0); got != tc.wantQuote {
			t.Errorf("QuoteMeta(%q, 0) got %q, wanted %q",
				tc.pat, got, tc.wantQuote)
		}
	}
}
