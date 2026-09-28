// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package expand

import (
	"fmt"
	"iter"
	"slices"
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
	bracesSeqRec(nil, word.Parts, nil, 0, func(w *syntax.Word, err error) bool {
		if err != nil {
			return false
		}
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
		bracesSeqRec(nil, word.Parts, nil, 0, func(w *syntax.Word, err error) bool {
			if err != nil {
				yield(nil, err)
				return false
			}
			count++
			if count > limit {
				yield(nil, fmt.Errorf("brace expansion would exceed %d elements", limit))
				return false
			}
			return yield(w, nil)
		})
	}
}

// maxBraceDepth is how many brace expansions may apply to each word,
// nested like {a,{b,c}} or in a row like {a,b}{c,d},
// so that a long word cannot overflow the Go stack.
const maxBraceDepth = 10_000

// braceRest is a linked list of the word parts left to expand
// after the brace expansions currently being expanded.
type braceRest struct {
	parts []syntax.WordPart
	next  *braceRest
}

// bracesSeqRec yields each fully-expanded word made of prefix,
// followed by the expansions of parts and rest.
// It returns false if iteration should stop.
//
// Siblings share the backing array of prefix, so that building each word
// takes time proportional to its length rather than to its depth.
func bracesSeqRec(prefix, parts []syntax.WordPart, rest *braceRest, depth int, yield func(*syntax.Word, error) bool) bool {
	// Find the next brace expansion, moving the parts before it to prefix.
	var br *syntax.BraceExp
	for {
		i := slices.IndexFunc(parts, func(wp syntax.WordPart) bool {
			_, ok := wp.(*syntax.BraceExp)
			return ok
		})
		if i >= 0 {
			br = parts[i].(*syntax.BraceExp)
			prefix = append(prefix, parts[:i]...)
			if len(parts) > i+1 {
				rest = &braceRest{parts: parts[i+1:], next: rest}
			}
			break
		}
		prefix = append(prefix, parts...)
		if rest == nil {
			return yield(&syntax.Word{Parts: slices.Clone(prefix)}, nil)
		}
		parts, rest = rest.parts, rest.next
	}
	if depth >= maxBraceDepth {
		yield(nil, fmt.Errorf("brace expansion is deeper than %d levels", maxBraceDepth))
		return false
	}
	expand := func(elem ...syntax.WordPart) bool {
		return bracesSeqRec(prefix, elem, rest, depth+1, yield)
	}
	if br.Sequence {
		return braceSequence(br, func(lit *syntax.Lit) bool { return expand(lit) })
	}
	for _, elem := range br.Elems {
		if !expand(elem.Parts...) {
			return false
		}
	}
	return true
}

// braceSequence yields each element of a sequence brace expansion like {1..9},
// returning false if iteration should stop.
func braceSequence(br *syntax.BraceExp, yield func(*syntax.Lit) bool) bool {
	fromLit := br.Elems[0].Lit()
	toLit := br.Elems[1].Lit()

	chars := false
	// ParseInt with bit size 64 to ensure consistent behavior on 32-bit platforms.
	from, err1 := strconv.ParseInt(fromLit, 10, 64)
	to, err2 := strconv.ParseInt(toLit, 10, 64)
	if err1 != nil || err2 != nil {
		chars = true
		from = int64(fromLit[0])
		to = int64(toLit[0])
	}
	// Endpoints with leading zeros pad all results to the
	// widest endpoint, e.g. {01..10} gives 01 02 [...] 09 10.
	width := 0
	if !chars && (hasLeadingZeros(fromLit) || hasLeadingZeros(toLit)) {
		width = max(len(fromLit), len(toLit))
	}
	upward := from <= to
	step := uint64(1)
	if len(br.Elems) > 2 {
		// ParseInt with bit size 64 to ensure consistent behavior on 32-bit platforms.
		n, _ := strconv.ParseInt(br.Elems[2].Lit(), 10, 64)
		// Only the absolute value of the step matters.
		if n < 0 {
			step = -uint64(n)
		} else if n > 0 {
			step = uint64(n)
		}
	}
	for n := from; ; {
		lit := &syntax.Lit{}
		switch {
		case chars:
			lit.Value = string(rune(n))
		case width > 0:
			lit.Value = fmt.Sprintf("%0*d", width, n)
		default:
			lit.Value = strconv.FormatInt(n, 10)
		}
		if !yield(lit) {
			return false
		}
		// Stop before stepping past the end, which may overflow.
		if upward {
			if uint64(to)-uint64(n) < step {
				return true
			}
			n += int64(step)
		} else {
			if uint64(n)-uint64(to) < step {
				return true
			}
			n -= int64(step)
		}
	}
}

func hasLeadingZeros(s string) bool {
	s = strings.TrimPrefix(s, "-")
	return len(s) > 1 && s[0] == '0'
}
