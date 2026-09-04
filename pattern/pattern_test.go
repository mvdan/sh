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
	wantErr string

	mustMatch    []string
	mustNotMatch []string
}{
	{pat: ``, want: ``},
	{pat: `foo`, want: `foo`},
	{
		pat: `foo`, mode: NoGlobCase, want: `(?si)foo`,
		mustMatch:    []string{"foo", "FOO", "Foo"},
		mustNotMatch: []string{"bar"},
	},
	{pat: `foóà中`, mode: Filenames, want: `foóà中`},
	{pat: `.`, want: `(?s)\.`},
	{pat: `foo*`, want: `(?s)foo.*`},
	{pat: `foo*`, mode: Shortest, want: `(?sU)foo.*`},
	{pat: `foo*`, mode: Shortest | Filenames, want: `(?sU)foo[^/]*`},
	{
		pat: `*foo*`, mode: EntireString, want: `(?s)^.*foo.*$`,
		mustMatch:    []string{"foo", "prefix-foo", "foo-suffix", "foo.suffix", ".foo.", "a\nbfooc\nd"},
		mustNotMatch: []string{"bar"},
	},
	{
		pat: `foo*`, mode: Filenames | EntireString, want: `(?s)^foo[^/]*$`,
		mustMatch:    []string{"foo", "foo-suffix", "foo.suffix", "foo\nsuffix"},
		mustNotMatch: []string{"prefix-foo", "foo/suffix"},
	},
	{
		pat: `foo/*`, mode: Filenames | EntireString, want: `(?s)^foo/([^/.][^/]*)?$`,
		mustMatch:    []string{"foo/", "foo/suffix"},
		mustNotMatch: []string{"foo/.suffix", "foo/bar/baz"},
	},
	{
		pat: `foo/*`, mode: Filenames | EntireString | GlobLeadingDot, want: `(?s)^foo/[^/]*$`,
		mustMatch:    []string{"foo/", "foo/suffix", "foo/.suffix"},
		mustNotMatch: []string{"foo/bar/baz"},
	},
	{pat: `*foo`, mode: Filenames, want: `(?s)([^/.][^/]*)?foo`},
	{
		pat: `*foo`, mode: Filenames | EntireString, want: `(?s)^([^/.][^/]*)?foo$`,
		mustMatch:    []string{"foo", "prefix-foo", "prefix.foo"},
		mustNotMatch: []string{"foo-suffix", "/prefix/foo", ".foo", ".prefix-foo"},
	},
	{pat: `**`, want: `(?s).*.*`},
	{
		pat: `**`, mode: Filenames | EntireString, want: `(?s)^(/|[^/.][^/]*)*$`,
		mustMatch:    []string{"/foo", "/prefix/foo", "/a.b.c/foo", "/a/b/c/foo", "/foo/suffix.ext", "/a\n/\nb"},
		mustNotMatch: []string{"/.prefix/foo", "/prefix/.foo"},
	},
	{
		pat: `**`, mode: Filenames | NoGlobStar | EntireString, want: `(?s)^([^/.][^/]*)?$`,
		mustMatch:    []string{"foo.bar"},
		mustNotMatch: []string{"foo/bar", ".foo"},
	},
	{
		pat: `**`, mode: Filenames | EntireString | GlobLeadingDot, want: `(?s)^.*$`,
		mustMatch: []string{"/foo", "/prefix/foo", "/a.b.c/foo", "/a/b/c/foo", "/foo/suffix.ext", "/a\n/\nb", "/.prefix/foo", "/prefix/.foo"},
	},
	{pat: `/**/foo`, want: `(?s)/.*.*/foo`},
	{
		pat: `/**/foo`, mode: Filenames | EntireString, want: `(?s)^/((/|[^/.][^/]*)*/)?foo$`,
		mustMatch:    []string{"/foo", "/prefix/foo", "/a.b.c/foo", "/a/b/c/foo"},
		mustNotMatch: []string{"/foo/suffix", "prefix/foo", "/.prefix/foo", "/prefix/.foo"},
	},
	{
		pat: `/**/foo`, mode: Filenames | EntireString | GlobLeadingDot, want: `(?s)^/(.*/)?foo$`,
		mustMatch:    []string{"/foo", "/prefix/foo", "/a.b.c/foo", "/a/b/c/foo", "/.prefix/foo"},
		mustNotMatch: []string{"/foo/suffix", "prefix/foo", "/prefix/.foo"},
	},
	{pat: `/**/foo`, mode: Filenames | NoGlobStar, want: `(?s)/([^/.][^/]*)?/foo`},
	{pat: `/**/à`, mode: Filenames, want: `(?s)/((/|[^/.][^/]*)*/)?à`},
	{
		pat: `/**foo`, mode: Filenames, want: `(?s)/([^/.][^/]*)?foo`,
		// These all match because without EntireString, we match substrings.
		mustMatch: []string{"/foo", "/prefix-foo", "/foo-suffix", "/sub/foo"},
	},
	{
		pat: `/**foo`, mode: Filenames | EntireString, want: `(?s)^/([^/.][^/]*)?foo$`,
		mustMatch:    []string{"/foo", "/prefix-foo"},
		mustNotMatch: []string{"/foo-suffix", "/sub/foo", "/.foo", "/.prefix-foo"},
	},
	{
		pat: `/foo**`, mode: Filenames | EntireString, want: `(?s)^/foo[^/]*$`,
		mustMatch:    []string{"/foo", "/foo-suffix", "/foo.suffix"},
		mustNotMatch: []string{"/prefix-foo", "/foo/sub"},
	},
	{pat: `\*`, want: `(?s)\*`},
	{pat: `\`, wantErr: `^\\ at end of pattern$`},
	{pat: `?`, want: `(?s).`},
	{
		pat: `?`, mode: EntireString, want: `(?s)^.$`,
		mustMatch:    []string{"a", "\n", " "},
		mustNotMatch: []string{"abc", ""},
	},
	{pat: `?`, mode: Filenames, want: `(?s)[^/]`},
	{pat: `?à`, want: `(?s).à`},
	{pat: `ŀ*`, want: `(?s)ŀ.*`},
	{pat: `\a`, want: `(?s)a`},
	{pat: `(`, want: `(?s)\(`},
	{pat: `a|b`, want: `(?s)a\|b`},
	{pat: `x{3}`, want: `(?s)x\{3\}`},
	{pat: `{3,4}`, want: `(?s)\{3,4\}`},
	{pat: `[a]`, want: `(?s)[a]`},
	{pat: `[abc]`, want: `(?s)[abc]`},
	{pat: `[^bc]`, want: `(?s)[^bc]`},
	{pat: `[!bc]`, want: `(?s)[^bc]`},
	{pat: `[[]`, want: `(?s)[[]`},
	{pat: `[\]]`, want: `(?s)[\]]`},
	{pat: `[\]]`, mode: Filenames, want: `(?s)[\]]`},
	{pat: `[]]`, want: `(?s)[]]`},
	{pat: `[!]]`, want: `(?s)[^]]`},
	{pat: `[^]]`, want: `(?s)[^]]`},
	{pat: `[a/b]`, want: `(?s)[a/b]`},
	{
		pat: `[a/b]`, mode: EntireString | Filenames, want: `(?s)^\[a/b\]$`,
		mustMatch:    []string{"[a/b]"},
		mustNotMatch: []string{"a", "/", "b"},
	},
	{
		pat: `[]/a]`, mode: EntireString | Filenames, want: `(?s)^\[\]/a\]$`,
		mustMatch:    []string{"[]/a]"},
		mustNotMatch: []string{"]", "/", "a", "/a]", "/a"},
	},
	{
		pat: `[[:alpha:]/]`, mode: EntireString | Filenames, want: `(?s)^\[\[:alpha:\]/\]$`,
		mustMatch:    []string{"[[:alpha:]/]"},
		mustNotMatch: []string{"/", "a", "Z"},
	},
	{
		pat: `[[:wrong:]/]`, mode: EntireString | Filenames, want: `(?s)^\[\[:wrong:\]/\]$`,
		mustMatch:    []string{"[[:wrong:]/]"},
		mustNotMatch: []string{"/", "a", "Z"},
	},
	{pat: `[[.x.]]`, mode: Filenames, wantErr: `^charClass invalid$`},
	{
		pat: `[[./.]]`, mode: EntireString | Filenames, want: `(?s)^\[\[\./\.\]\]$`,
		mustMatch:    []string{"[[./.]]"},
		mustNotMatch: []string{"/", "x"},
	},
	{
		pat: `[a/*`, mode: EntireString | Filenames, want: `(?s)^\[a/([^/.][^/]*)?$`,
		mustMatch:    []string{"[a/", "[a/file"},
		mustNotMatch: []string{"[a/.file", "a/file"},
	},
	{pat: `[a/\`, mode: Filenames, wantErr: `^\\ at end of pattern$`},
	{
		pat: `[\]/]`, mode: EntireString | Filenames, want: `(?s)^\[\\\]/\]$`,
		mustMatch:    []string{`[\]/]`},
		mustNotMatch: []string{"]/]", "/"},
	},
	{
		pat: `[!]/a]`, mode: EntireString | Filenames, want: `(?s)^\[!\]/a\]$`,
		mustMatch:    []string{"[!]/a]"},
		mustNotMatch: []string{"]/a]", "/"},
	},
	{
		pat: `[\[:digit:]/]`, mode: EntireString | Filenames, want: `(?s)^[\[:digit:]/\]$`,
		mustMatch:    []string{"[/]", ":/]", "d/]"},
		mustNotMatch: []string{`[\[:digit:]/]`, "/"},
	},
	// An unmatched "[" is a literal, like in Bash.
	{
		pat: `[`, mode: EntireString, want: `(?s)^\[$`,
		mustMatch:    []string{"["},
		mustNotMatch: []string{"", "[]"},
	},
	// TODO: bash treats the trailing backslash as a literal instead.
	{pat: `[\`, wantErr: `^\\ at end of pattern$`},
	{pat: `[^`, want: `(?s)\[\^`},
	{pat: `[!`, want: `(?s)\[!`},
	{pat: `[!bc]`, want: `(?s)[^bc]`},
	{pat: `[]`, want: `(?s)\[\]`},
	{pat: `[^]`, want: `(?s)\[\^\]`},
	{pat: `[!]`, want: `(?s)\[!\]`},
	{
		pat: `[ab`, mode: EntireString, want: `(?s)^\[ab$`,
		mustMatch:    []string{"[ab"},
		mustNotMatch: []string{"ab", "a"},
	},
	{
		pat: `[a*`, mode: EntireString, want: `(?s)^\[a.*$`,
		mustMatch:    []string{"[a", "[abc"},
		mustNotMatch: []string{"a", "[b"},
	},
	{pat: `[z-a`, want: `(?s)\[z-a`},
	{pat: `[[ab`, want: `(?s)\[\[ab`},
	{pat: `[a[b`, want: `(?s)\[a\[b`},
	{
		pat: `[[abc]`, mode: EntireString, want: `(?s)^[[abc]$`,
		mustMatch:    []string{"[", "a"},
		mustNotMatch: []string{"[a", "d"},
	},
	{
		pat: `[[:alpha:]`, mode: EntireString, want: `(?s)^\[[:alpha:]$`,
		mustMatch:    []string{"[a", "[:"},
		mustNotMatch: []string{"[1", "["},
	},
	// Invalid character classes are errors even in an unmatched bracket.
	{pat: `[[:wrong:]`, wantErr: `^charClass invalid$`},
	{pat: `[[.x.]`, wantErr: `^charClass invalid$`},
	{pat: `[z-a[:wrong:]`, wantErr: `^charClass invalid$`},
	{pat: `[a-]`, want: `(?s)[a-]`},
	{
		pat: `[\0]`, want: `(?s)[0]`,
		mustMatch:    []string{"0"},
		mustNotMatch: []string{"\x00", "a"},
	},
	{
		pat: `[\d]`, want: `(?s)[d]`,
		mustMatch:    []string{"d"},
		mustNotMatch: []string{"5"},
	},
	{pat: `[\5]`, want: `(?s)[5]`},
	{
		pat: `[\é]`, want: `(?s)[é]`,
		mustMatch: []string{"é"},
	},
	{
		pat: `[\-]`, want: `(?s)[\-]`,
		mustMatch:    []string{"-"},
		mustNotMatch: []string{"a"},
	},
	{pat: `[z-a]`, wantErr: `^invalid range: z-a$`},
	{pat: `[a-a]`, want: `(?s)[a-a]`},
	{pat: `[aa]`, want: `(?s)[aa]`},
	{pat: `[0-4A-Z]`, want: `(?s)[0-4A-Z]`},
	{pat: `[-a]`, want: `(?s)[-a]`},
	{pat: `[^-a]`, want: `(?s)[^-a]`},
	{pat: `[a-]`, want: `(?s)[a-]`},
	{pat: `[[:digit:]]`, want: `(?s)[[:digit:]]`},
	{
		pat: `*[![:space:]]*`, mode: EntireString, want: `(?s)^.*[^[:space:]].*$`,
		mustMatch:    []string{"x", "  x  ", "\t\n!"},
		mustNotMatch: []string{"", " ", " \t\n "},
	},
	{pat: `[!a[:space:]0-9]`, want: `(?s)[^a[:space:]0-9]`},
	{pat: `[a[:digit]]`, wantErr: `^charClass invalid$`},
	{pat: `[[:`, wantErr: `^charClass invalid$`},
	// Like Bash, an unclosed extended operator group is literal text,
	// and the operator is parsed as a regular character.
	{
		pat: `@(a`, mode: ExtendedOperators | EntireString, want: `(?s)^@\(a$`,
		mustMatch:    []string{"@(a"},
		mustNotMatch: []string{"a"},
	},
	{
		pat: `@(a|b`, mode: ExtendedOperators | EntireString, want: `(?s)^@\(a\|b$`,
		mustMatch:    []string{"@(a|b"},
		mustNotMatch: []string{"a", "b"},
	},
	{
		pat: `*(a`, mode: ExtendedOperators | EntireString, want: `(?s)^.*\(a$`,
		mustMatch:    []string{"(a", "foo(a"},
		mustNotMatch: []string{"a"},
	},
	{pat: `+(`, mode: ExtendedOperators, want: `(?s)\+\(`},
	{pat: `!(a`, mode: ExtendedOperators | EntireString, want: `(?s)^!\(a$`},
	{
		pat: `@(a|@(b)`, mode: ExtendedOperators | EntireString, want: `(?s)^@\(a\|(b)$`,
		mustMatch:    []string{"@(a|b"},
		mustNotMatch: []string{"a", "b"},
	},
	{pat: `[[:digit`, wantErr: `^charClass invalid$`},
	{pat: `[[:wrong:]]`, wantErr: `^charClass invalid$`},
	{pat: `[[=x=]]`, wantErr: `^charClass invalid$`},
	{pat: `[[.x.]]`, wantErr: `^charClass invalid$`},
}

func TestRegexp(t *testing.T) {
	t.Parallel()
	for i, tc := range regexpTests {
		t.Run(fmt.Sprintf("%02d", i), func(t *testing.T) {
			t.Logf("input: pattern=%q mode=%#b\n", tc.pat, tc.mode)
			got, gotErr := Regexp(tc.pat, tc.mode)
			if tc.wantErr != "" {
				qt.Assert(t, qt.ErrorMatches(gotErr, tc.wantErr))
			} else {
				qt.Assert(t, qt.IsNil(gotErr))
			}
			if got != tc.want {
				t.Errorf("(%q, %#b) got %q, wanted %q", tc.pat, tc.mode, got, tc.want)
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
	{`[ab]`, true, `\[ab]`},
	{`[ab`, false, `\[ab`},
	{`ab]`, false, `ab]`},
	{`[a\]`, false, `\[a\\]`},
	{`[[:wrong:]]`, true, `\[\[:wrong:]]`},
	{`[[:`, false, `\[\[:`},
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
