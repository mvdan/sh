// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package expand

import (
	"strings"
	"testing"

	"github.com/go-quicktest/qt"
	"mvdan.cc/sh/v3/syntax"
)

// testEnv is a minimal WriteEnviron backed by a map, used to observe side
// effects (assignments) performed during arithmetic evaluation.
type testEnv map[string]string

func (e testEnv) Get(name string) Variable {
	if val, ok := e[name]; ok {
		return Variable{Set: true, Kind: String, Str: val}
	}
	return Variable{}
}

func (e testEnv) Each(fn func(name string, vr Variable) bool) {
	for name, val := range e {
		if !fn(name, Variable{Set: true, Kind: String, Str: val}) {
			return
		}
	}
}

func (e testEnv) Set(name string, vr Variable) error {
	e[name] = vr.String()
	return nil
}

func parseArithm(t *testing.T, src string) syntax.ArithmExpr {
	t.Helper()
	expr, err := syntax.NewParser().Arithmetic(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	return expr
}

func TestArithmShortCircuit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		src  string
		want int
		env  string // value of x after evaluation
	}{
		{"0 && (x = 1)", 0, "0"},
		{"1 && (x = 2)", 1, "2"},
		{"1 || (x = 1)", 1, "0"},
		{"0 || (x = 2)", 1, "2"},
		// The non-short-circuiting operators still evaluate both sides.
		{"0 + (x = 5)", 5, "5"},
	}
	for _, tc := range tests {
		t.Run(tc.src, func(t *testing.T) {
			env := testEnv{"x": "0"}
			got, err := Arithm(&Config{Env: env}, parseArithm(t, tc.src))
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Equals(got, tc.want))
			qt.Assert(t, qt.Equals(env["x"], tc.env))
		})
	}
}
