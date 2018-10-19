// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package shell

import (
	"context"
	"os"
	"strings"

	"mvdan.cc/sh/expand"
	"mvdan.cc/sh/syntax"
)

// Expand performs shell expansion on s, using env to resolve variables.
// The expansion will apply to parameter expansions like $var and
// ${#var}, but also to arithmetic expansions like $((var + 3)), and brace
// expressions like foo{1,2,3}.
//
// If env is nil, the current environment variables are used. Empty variables
// are treated as unset; to support variables which are set but empty, use
// expand.Context directly.
//
// Subshells like $(echo foo) aren't supported to avoid running arbitrary code.
// To support those, use an interpreter with expand.Context.
//
// An error will be reported if the input string had invalid syntax.
func Expand(s string, env func(string) string) (string, error) {
	p := syntax.NewParser()
	word, err := p.Document(strings.NewReader(s))
	if err != nil {
		return "", err
	}
	if env == nil {
		env = os.Getenv
	}
	ectx := expand.Context{Env: expand.FuncEnviron(env)}
	ctx := context.Background()
	fields := ectx.ExpandFields(ctx, word)
	return strings.Join(fields, ""), nil
}
