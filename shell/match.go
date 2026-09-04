// Copyright (c) 2026, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package shell

import (
	"mvdan.cc/sh/v3/internal"
	"mvdan.cc/sh/v3/pattern"
)

// Match reports whether name matches the shell pattern, following the rules of
// Bash's case statements and [[ ]] conditionals. The pattern must match the
// entire name, and extended operators such as @(a|b) are always recognized.
// Unlike [path.Match], slashes are ordinary characters, so "*" matches them.
// For more options, such as treating slashes as path separators,
// use the [pattern] package directly.
//
// An error will be reported if the pattern had invalid syntax, unlike Bash,
// where a malformed pattern simply does not match. The negation operator !(...)
// is only supported once per pattern, surrounded by literal text.
func Match(pat, name string) (bool, error) {
	matcher, err := internal.ExtendedPatternMatcher(pat, pattern.EntireString|pattern.ExtendedOperators)
	if err != nil {
		return false, err
	}
	return matcher(name), nil
}
