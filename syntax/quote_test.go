// Copyright (c) 2021, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"testing"

	qt "github.com/frankban/quicktest"
)

func TestQuote(t *testing.T) {
	t.Parallel()
	tests := [...]struct {
		str  string
		lang LangVariant
		want interface{}
	}{
		{"", LangBash, ``},
		{"\a", LangBash, `$'\a'`},
		{"\b", LangBash, `$'\b'`},
		{"\f", LangBash, `$'\f'`},
		{"\n", LangBash, `$'\n'`},
		{"\r", LangBash, `$'\r'`},
		{"\t", LangBash, `$'\t'`},
		{"\v", LangBash, `$'\v'`},
		{"null\x00", LangBash, &QuoteError{4, quoteErrNull}},
		{"posix\x1b", LangPOSIX, &QuoteError{5, quoteErrPOSIX}},
		{"posix\n", LangPOSIX, &QuoteError{5, quoteErrPOSIX}},
		{"mksh16\U00086199", LangMirBSDKorn, &QuoteError{6, quoteErrMksh}},
		{"\x1b\x1caaa", LangBash, `$'\x1b\x1caaa'`},
		{"\x1b\x1caaa", LangMirBSDKorn, `$'\x1b\x1c'$'aaa'`},
		{"\xff\x00", LangBash, &QuoteError{1, quoteErrNull}},
	}

	for _, test := range tests {
		test := test
		t.Run("", func(t *testing.T) {
			t.Parallel()

			got, gotErr := Quote(test.str, test.lang)
			switch want := test.want.(type) {
			case string:
				qt.Assert(t, got, qt.Equals, want)
				qt.Assert(t, gotErr, qt.IsNil)
			case *QuoteError:
				qt.Assert(t, got, qt.Equals, "")
				qt.Assert(t, gotErr, qt.DeepEquals, want)
			default:
				t.Fatalf("unexpected type: %T", want)
			}
		})
	}
}
