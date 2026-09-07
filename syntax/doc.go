// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

// Package syntax implements parsing and formatting of shell programs.
// It supports POSIX Shell, Bash, mksh, Zsh, and Bats via [LangVariant].
//
// [NewParser] builds a syntax tree from source, [NewPrinter] formats it back
// into source and is the engine behind shfmt, and [Walk] and [Preorder]
// traverse it. [Simplify] removes redundant syntax, [Quote] quotes arbitrary
// strings as shell words, and [mvdan.cc/sh/v3/syntax/typedjson] encodes trees
// as JSON.
package syntax
