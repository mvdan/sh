// Copyright (c) 2026, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package shell

import (
	"testing"

	"github.com/go-quicktest/qt"
)

var splitTests = []struct {
	in   string
	want []string
}{
	{"", nil},
	{"foo", []string{"foo"}},
	{"\nfoo\n", []string{"foo"}},
	{"foo bar", []string{"foo", "bar"}},
	{"foo\tbar\nbaz", []string{"foo", "bar", "baz"}},
	{"foo 'bar baz'", []string{"foo", "bar baz"}},
	{`foo "bar baz"`, []string{"foo", "bar baz"}},
	{`foo\ bar`, []string{"foo bar"}},
	{`foo\\bar`, []string{`foo\bar`}},
	{`'a'"b"c\d`, []string{"abcd"}},
	{`''`, []string{""}},
	{`""`, []string{""}},
	{"a\\\nb", []string{"ab"}},
	{`"a\"b\\c\$d\` + "`" + `e\xf"`, []string{"a\"b\\c$d`e\\xf"}},
	{`'a\nb'`, []string{`a\nb`}},
	{`$'a\tb'`, []string{"a\tb"}},

	// No expansion; these are kept verbatim.
	{"$x", []string{"$x"}},
	{`"$x y"`, []string{"$x y"}},
	{"${x:-a b}", []string{"${x:-a b}"}},
	{"$(echo foo)", []string{"$(echo foo)"}},
	{"`echo foo`", []string{"`echo foo`"}},
	{"$((1 + 2))", []string{"$((1 + 2))"}},
	{"a$b\"c$d\"'e'", []string{"a$bc$de"}},
	{"~ ~/foo", []string{"~", "~/foo"}},
	{"{a,b} *.go", []string{"{a,b}", "*.go"}},
}

func TestSplit(t *testing.T) {
	t.Parallel()
	for _, tc := range splitTests {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			got, err := Split(tc.in)
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.DeepEquals(got, tc.want))
		})
	}
}

func TestSplitError(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"'foo", `"foo`, "$(foo", "foo )"} {
		_, err := Split(in)
		qt.Assert(t, qt.IsNotNil(err), qt.Commentf("input: %q", in))
	}
}

var joinTests = []struct {
	args []string
	want string
}{
	{nil, ""},
	{[]string{"foo"}, "foo"},
	{[]string{"foo", "bar"}, "foo bar"},
	{[]string{""}, "''"},
	{[]string{"foo bar", "baz"}, "'foo bar' baz"},
	{[]string{"$x", "~", "{a,b}", "*.go"}, "'$x' '~' '{a,b}' '*.go'"},
	{[]string{"it's"}, `"it's"`},
	{[]string{"a\nb", "\x1b[0m"}, `$'a\nb' $'\x1b[0m'`},
}

func TestJoin(t *testing.T) {
	t.Parallel()
	for _, tc := range joinTests {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			got, err := Join(tc.args...)
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Equals(got, tc.want))

			// Join must round-trip through both Split and Fields.
			split, err := Split(got)
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.DeepEquals(split, tc.args))
			fields, err := Fields(got, strEnviron())
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.DeepEquals(fields, tc.args))
		})
	}
}

func TestJoinError(t *testing.T) {
	t.Parallel()
	_, err := Join("foo", "a\x00b")
	qt.Assert(t, qt.ErrorMatches(err, `cannot quote character.*`))
}
