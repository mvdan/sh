// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package expand

import "mvdan.cc/sh/syntax"

// Braces performs Bash brace expansion on a word. For example, passing it a
// single-literal word "foo{bar,baz}" will return two single-literal words,
// "foobar" and "foobaz".
//
// It does not return an error; malformed brace expansions are simply skipped.
// For example, "a{b{c,d}" results in the words "a{bc" and "a{bd".
//
// Note that the resulting words may have more word parts than necessary, such
// as contiguous *syntax.Lit nodes, and that these parts may be shared between
// words.
func Braces(word *syntax.Word) []*syntax.Word {
	return syntax.ExpandBraces(word)
}
