// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package expand

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"mvdan.cc/sh/v3/syntax"
)

func parseWord(t *testing.T, src string) *syntax.Word {
	t.Helper()
	p := syntax.NewParser()
	word, err := p.Document(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	return word
}

func TestConfigNils(t *testing.T) {
	os.Setenv("EXPAND_GLOBAL", "value")
	tests := []struct {
		name string
		cfg  *Config
		src  string
		want string
	}{
		{
			"NilConfig",
			nil,
			"$EXPAND_GLOBAL",
			"",
		},
		{
			"ZeroConfig",
			&Config{},
			"$EXPAND_GLOBAL",
			"",
		},
		{
			"EnvConfig",
			&Config{Env: ListEnviron(os.Environ()...)},
			"$EXPAND_GLOBAL",
			"value",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			word := parseWord(t, tc.src)
			got, err := Literal(tc.cfg, word)
			if err != nil {
				t.Fatalf("did not want error, got %v", err)
			}
			if got != tc.want {
				t.Fatalf("wanted %q, got %q", tc.want, got)
			}
		})
	}
}

func TestFieldsIdempotency(t *testing.T) {
	tests := []struct {
		src  string
		want []string
	}{
		{
			"{1..4}",
			[]string{"1", "2", "3", "4"},
		},
		{
			"a{1..4}",
			[]string{"a1", "a2", "a3", "a4"},
		},
	}
	for _, tc := range tests {
		word := parseWord(t, tc.src)
		for range 2 {
			got, err := Fields(nil, word)
			if err != nil {
				t.Fatalf("did not want error, got %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("wanted %q, got %q", tc.want, got)
			}
		}
	}
}

func Test_glob(t *testing.T) {
	fs := fstest.MapFS{
		"case/A":             {},
		"case/AB":            {},
		"case/a":             {},
		"case/ab":            {},
		"globstar/foo":       {},
		"globstar/a/foo":     {},
		"globstar/a/b/c/foo": {},
	}

	tests := []struct {
		base string
		pat  string
		want []string
		cfg  Config
	}{
		{base: "case", pat: "a*", want: []string{"a", "ab"}},
		{base: "case", pat: "A*", want: []string{"A", "AB"}},
		{base: "case", pat: "*b", want: []string{"ab"}},
		{base: "case", pat: "b*", want: nil},
		{base: "case", pat: "a*", want: []string{"A", "AB", "a", "ab"}, cfg: Config{NoCaseGlob: true}},
		{base: "case", pat: "A*", want: []string{"A", "AB", "a", "ab"}, cfg: Config{NoCaseGlob: true}},
		{base: "case", pat: "*b", want: []string{"AB", "ab"}, cfg: Config{NoCaseGlob: true}},
		{base: "case", pat: "b*", want: nil, cfg: Config{NoCaseGlob: true}},

		{base: "globstar", pat: "**", want: []string{"a", "foo"}, cfg: Config{GlobStar: false}},
		{base: "globstar", pat: "**", want: []string{"a", "a/b", "a/b/c", "a/b/c/foo", "a/foo", "foo"}, cfg: Config{GlobStar: true}},
		{base: "globstar", pat: "**/", want: []string{"a/"}, cfg: Config{GlobStar: false}},
		{base: "globstar", pat: "**/", want: []string{"a/", "a/b/", "a/b/c/"}, cfg: Config{GlobStar: true}},
		{base: "globstar", pat: "**/foo", want: []string{"a/foo"}, cfg: Config{GlobStar: false}},
		{base: "globstar", pat: "**/foo", want: []string{"a/foo", "a/b/c/foo"}, cfg: Config{GlobStar: true}},
		{base: "globstar", pat: "**foo", want: []string{"foo"}, cfg: Config{GlobStar: false}},
		{base: "globstar", pat: "**foo", want: []string{"foo"}, cfg: Config{GlobStar: true}},
	}
	for _, tc := range tests {
		tc.cfg.ReadDir2 = fs.ReadDir
		got, err := tc.cfg.glob(tc.base, tc.pat)
		if err != nil {
			t.Fatalf("did not want error, got %v", err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("wanted %q, got %q", tc.want, got)
		}
	}
}
