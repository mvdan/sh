// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"bytes"
	"fmt"
	"testing"
)

var braceTests = []struct {
	in   *Word
	want []*Word
}{
	{
		litWord("a{b"),
		litWords("a{b"),
	},
	{
		litWord("a}b"),
		litWords("a}b"),
	},
	{
		litWord("{a,b{c,d}"),
		litWords("{a,bc", "{a,bd"),
	},
	{
		litWord("{a{b"),
		litWords("{a{b"),
	},
	{
		litWord("a{}"),
		litWords("a{}"),
	},
	{
		litWord("a{b}"),
		litWords("a{b}"),
	},
	{
		litWord("a{b,c}"),
		litWords("ab", "ac"),
	},
	{
		litWord("a{à,世界}"),
		litWords("aà", "a世界"),
	},
	{
		litWord("a{b,c}d{e,f}g"),
		litWords("abdeg", "abdfg", "acdeg", "acdfg"),
	},
	{
		litWord("a{b{x,y},c}d"),
		litWords("abxd", "abyd", "acd"),
	},
	{
		litWord("a{1,2,3,4,5}"),
		litWords("a1", "a2", "a3", "a4", "a5"),
	},
	{
		litWord("a{1.."),
		litWords("a{1.."),
	},
	{
		litWord("a{1..4"),
		litWords("a{1..4"),
	},
	{
		litWord("a{1.4}"),
		litWords("a{1.4}"),
	},
	{
		litWord("{a,b}{1..4"),
		litWords("a{1..4", "b{1..4"),
	},
	{
		litWord("a{1..4}"),
		litWords("a1", "a2", "a3", "a4"),
	},
	{
		litWord("a{1..2}b{4..5}c"),
		litWords("a1b4c", "a1b5c", "a2b4c", "a2b5c"),
	},
	{
		litWord("a{1..f}"),
		litWords("a{1..f}"),
	},
	{
		litWord("a{c..f}"),
		litWords("ac", "ad", "ae", "af"),
	},
	{
		litWord("a{-..f}"),
		litWords("a{-..f}"),
	},
	{
		litWord("a{3..-}"),
		litWords("a{3..-}"),
	},
	{
		litWord("a{1..10..3}"),
		litWords("a1", "a4", "a7", "a10"),
	},
	{
		litWord("a{1..4..0}"),
		litWords("a1", "a2", "a3", "a4"),
	},
	{
		litWord("a{4..1}"),
		litWords("a4", "a3", "a2", "a1"),
	},
	{
		litWord("a{4..1..-2}"),
		litWords("a4", "a2"),
	},
	{
		litWord("a{4..1..1}"),
		litWords("a4", "a3", "a2", "a1"),
	},
	{
		litWord("a{d..k..3}"),
		litWords("ad", "ag", "aj"),
	},
	{
		litWord("a{d..k..n}"),
		litWords("a{d..k..n}"),
	},
	{
		litWord("a{k..d..-2}"),
		litWords("ak", "ai", "ag", "ae"),
	},
}

func TestExpandBraces(t *testing.T) {
	t.Parallel()
	for i, tc := range braceTests {
		t.Run(fmt.Sprintf("%02d", i), func(t *testing.T) {
			inStr := printWords(tc.in)
			got := ExpandBraces(tc.in)
			gotStr := printWords(got...)
			wantStr := printWords(tc.want...)
			if gotStr != wantStr {
				t.Fatalf("mismatch in %q\nwant:\n%s\ngot: %s",
					inStr, wantStr, gotStr)
			}

		})
	}
}

func printWords(words ...*Word) string {
	p := NewPrinter()
	var buf bytes.Buffer
	var f File
	f.Stmts = append(f.Stmts, stmt(call(words...)))
	p.Print(&buf, &f)
	return buf.String()
}
