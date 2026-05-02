// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package expand

import (
	"fmt"
	"iter"
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Braces performs brace expansion on a word, given that it contains any
// [syntax.BraceExp] parts. For example, the word with a brace expansion
// "foo{bar,baz}" will return two literal words, "foobar" and "foobaz".
//
// Note that the resulting words may share word parts.
//
// Deprecated: use [BracesSeq], which yields words lazily and reports an
// error rather than letting a large sequence allocate huge amounts.
func Braces(word *syntax.Word) []*syntax.Word {
	var all []*syntax.Word
	bracesSeqRec(word, func(w *syntax.Word) bool {
		all = append(all, w)
		return true
	})
	return all
}

// BracesSeq performs brace expansion on a word, given that it contains any
// [syntax.BraceExp] parts. For example, the word with a brace expansion
// "foo{bar,baz}" will return two literal words, "foobar" and "foobaz".
//
// The iteration yields an error and stops if the total expansion is too
// large, including combinatorial blow-ups across multiple brace expansions
// like {1..100}{1..100}{1..100}. This may be configurable with cfg in the
// future; the parameter is entirely unused for now.
//
// Note that the resulting words may share word parts.
func BracesSeq(cfg *Config, word *syntax.Word) iter.Seq2[*syntax.Word, error] {
	return func(yield func(*syntax.Word, error) bool) {
		// 16Ki expanded elements is more than any script should need in practice,
		// but it's small enough where we don't waste too much memory and CPU.
		const limit = 16 << 10
		count := 0
		bracesSeqRec(word, func(w *syntax.Word) bool {
			count++
			if count > limit {
				yield(nil, fmt.Errorf("brace expansion would exceed %d elements", limit))
				return false
			}
			return yield(w, nil)
		})
	}
}

// bracesSeqRec yields each fully-expanded word descended from word.
// It returns false if iteration should stop.
func bracesSeqRec(word *syntax.Word, yield func(*syntax.Word) bool) bool {
	var left []syntax.WordPart
	for i, wp := range word.Parts {
		br, ok := wp.(*syntax.BraceExp)
		if !ok {
			left = append(left, wp)
			continue
		}
		rest := word.Parts[i+1:]
		// Yield each word produced by recursing on `next`,
		// after prepending `left` to its Parts.
		expand := func(next *syntax.Word) bool {
			return bracesSeqRec(next, func(w *syntax.Word) bool {
				w.Parts = append(append([]syntax.WordPart(nil), left...), w.Parts...)
				return yield(w)
			})
		}
		if br.Sequence {
			fromLit := br.Elems[0].Lit()
			toLit := br.Elems[1].Lit()
			zeros := max(extraLeadingZeros(fromLit), extraLeadingZeros(toLit))

			chars := false
			// ParseInt with bit size 64 to ensure consistent behavior on 32-bit platforms.
			from, err1 := strconv.ParseInt(fromLit, 10, 64)
			to, err2 := strconv.ParseInt(toLit, 10, 64)
			if err1 != nil || err2 != nil {
				chars = true
				from = int64(fromLit[0])
				to = int64(toLit[0])
			}
			upward := from <= to
			incr := int64(1)
			if !upward {
				incr = -1
			}
			if len(br.Elems) > 2 {
				// ParseInt with bit size 64 to ensure consistent behavior on 32-bit platforms.
				n, _ := strconv.ParseInt(br.Elems[2].Lit(), 10, 64)
				if n != 0 && n > 0 == upward {
					incr = n
				}
			}
			for n := from; (upward && n <= to) || (!upward && n >= to); n += incr {
				next := *word
				lit := &syntax.Lit{}
				if chars {
					lit.Value = string(rune(n))
				} else {
					lit.Value = strings.Repeat("0", zeros) + strconv.FormatInt(n, 10)
				}
				next.Parts = append([]syntax.WordPart{lit}, rest...)
				if !expand(&next) {
					return false
				}
			}
			return true
		}
		for _, elem := range br.Elems {
			next := *word
			next.Parts = append(append([]syntax.WordPart(nil), elem.Parts...), rest...)
			if !expand(&next) {
				return false
			}
		}
		return true
	}
	return yield(&syntax.Word{Parts: left})
}

func extraLeadingZeros(s string) int {
	for i, r := range s {
		if r != '0' {
			return i
		}
	}
	return 0 // "0" has no extra leading zeros
}
