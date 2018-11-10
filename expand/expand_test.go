// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package expand

import (
	"os"
	"strings"
	"testing"

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
