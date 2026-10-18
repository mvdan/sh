// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"slices"
	"strconv"
	"strings"
)

// SplitBraces parses brace expansions within a word's literal parts.
// If any valid brace expansions are found, they are replaced with BraceExp nodes,
// and the function returns true.
// Otherwise, the word is left untouched and the function returns false.
//
// For example, a literal word "foo{bar,baz}" will result in a word containing
// the literal "foo", and a brace expansion with the elements "bar" and "baz".
//
// It does not return an error; malformed brace expansions are simply skipped.
// For example, the literal word "a{b" is left unchanged.
func SplitBraces(word *Word) bool {
	// In the common case where a word cannot have brace expansions, skip any allocs.
	if !litsContainAny(word.Parts, "{") || !litsContainAny(word.Parts, ",.") {
		return false
	}
	var s braceSplitter
	s.init(word.Parts)
	parts := s.split(0, len(s.toks)-1)
	if parts == nil {
		return false
	}
	word.Parts = parts
	return true
}

// litsContainAny reports whether any literal part contains any of the chars.
func litsContainAny(parts []WordPart, chars string) bool {
	for _, part := range parts {
		if lit, ok := part.(*Lit); ok && strings.ContainsAny(lit.Value, chars) {
			return true
		}
	}
	return false
}

// braceSplitter implements [SplitBraces] following Bash's algorithm:
// the first opening brace with a valid closing brace forms a brace expansion,
// whose elements and the text following it are then split recursively.
//
// A closing brace is valid when it follows a comma or ".." at the same level
// of nesting. Before that, closing braces at the same level are literal,
// so that "{a},b}" expands to "a}" and "b".
//
// The literals are joined as text, and each brace, comma, and dot in it
// is a token, with a sentinel at either end. A backwards pass over the
// tokens records what Bash would find in the text after each of them,
// reusing the results of the tokens which follow, so that the split
// is linear. Methods take token indices unless they say otherwise; byte
// offsets in text are named off, start, or end, and part indices k.
type braceSplitter struct {
	parts []WordPart
	offs  []int  // byte offset in text of each part
	text  string // the bytes of each literal, and a zero byte for other parts
	toks  []braceToken

	depth int // how many brace expansions enclose the text being split
}

// braceToken is a brace, comma, or dot in a [braceSplitter]'s text,
// or a sentinel at either end, with what is known about the text after it.
// Escaped bytes are not tokens, except for dots, which still count towards
// a ".." in Bash's valid_seqterm.
type braceToken struct {
	off     int  // byte offset in text; -1 for the start sentinel
	kind    byte // '{', '}', ',', '.', or zero for the sentinels
	escaped bool // whether the dot follows a backslash

	// dotsAt is the first "..", escaped or not, after this token,
	// or the end sentinel if there is none.
	dotsAt int
	braceScan
}

// braceScan is what Bash's brace_gobbler finds in the text after a token,
// ignoring the bounds of any enclosing text. A brace index is the end
// sentinel when there is no such brace. For an opening brace, closeAt is
// its own closing brace, and matchAt the one it forms a brace expansion with.
type braceScan struct {
	closeAt  int  // the first closing brace at the same level
	matchAt  int  // the valid closing brace for an opening brace just before the text
	matchSeq bool // whether matchAt would form a sequence expression
}

// singleOffs is the offs of a word with a single literal, whose text is not copied.
var singleOffs = []int{0}

func (s *braceSplitter) init(parts []WordPart) {
	s.parts = parts
	if lit, ok := parts[0].(*Lit); ok && len(parts) == 1 {
		s.text, s.offs = lit.Value, singleOffs
	} else {
		s.offs = make([]int, len(parts))
		var sb strings.Builder
		for k, wp := range parts {
			s.offs[k] = sb.Len()
			if lit, ok := wp.(*Lit); ok {
				sb.WriteString(lit.Value)
			} else {
				sb.WriteByte(0)
			}
		}
		s.text = sb.String()
	}

	// Count the bytes which can be tokens, escaped or not, plus the two
	// sentinels, to allocate toks once.
	n := 2 + strings.Count(s.text, "{") + strings.Count(s.text, "}") +
		strings.Count(s.text, ",") + strings.Count(s.text, ".")
	s.toks = make([]braceToken, 1, n)
	s.toks[0].off = -1
	for i := 0; i < len(s.text); i++ {
		switch c := s.text[i]; c {
		case '\\':
			if i++; i < len(s.text) && s.text[i] == '.' {
				s.toks = append(s.toks, braceToken{off: i, kind: '.', escaped: true})
			}
		case '{', '}', ',', '.':
			s.toks = append(s.toks, braceToken{off: i, kind: c})
		}
	}
	end := len(s.toks)
	s.toks = append(s.toks, braceToken{off: len(s.text), dotsAt: end, braceScan: braceScan{closeAt: end, matchAt: end}})

	// Walk backwards, so that each token can reuse the results which follow it.
	st, dots := s.toks[end].braceScan, end
	for t := end - 1; t > 0; t-- {
		cur := &s.toks[t]
		cur.braceScan, cur.dotsAt = st, dots
		switch cur.kind {
		case '{':
			st = s.toks[st.closeAt].braceScan // continue after the closing brace
		case '}':
			// Not valid as a match until a comma or ".." is seen.
			st.closeAt = t
		case ',':
			st.matchAt, st.matchSeq = st.closeAt, false
		case '.':
			if !s.rawDots(t) {
				break
			}
			dots = t
			// Like Bash, "..}" is not a separator.
			sep := !cur.escaped && (cur.off+2 == len(s.text) || s.text[cur.off+2] != '}')
			// A separator after this one already claims the closing brace.
			if sep && st.matchAt != st.closeAt {
				st.matchAt, st.matchSeq = st.closeAt, true
			}
		}
	}
}

// rawDots reports whether tokens t and t+1 are adjacent dots forming "..",
// whether or not they are escaped.
func (s *braceSplitter) rawDots(t int) bool {
	return s.toks[t].kind == '.' && s.toks[t+1].kind == '.' && s.toks[t+1].off == s.toks[t].off+1
}

// dots is like [braceSplitter.rawDots], but ignores escaped dots.
// Only the first dot can be escaped, as the second follows a dot.
func (s *braceSplitter) dots(t int) bool {
	return s.rawDots(t) && !s.toks[t].escaped
}

// span returns the byte offsets of the text between tokens from and to.
func (s *braceSplitter) span(from, to int) (start, end int) {
	return s.toks[from].off + 1, s.toks[to].off
}

// split returns the parts for the text between tokens from and to
// with brace expansions split.
func (s *braceSplitter) split(from, to int) []WordPart {
	if s.depth > maxNesting {
		// Like the parser, leave deeper nesting literal so that a long word
		// cannot overflow the stack. One more level than the parser's is
		// split, so that expand can still report its own depth limit.
		return s.appendLit(nil, from, to)
	}
	var parts []WordPart
	litFrom := from  // the start of the text not yet appended to parts
	textFrom := from // the start of the remaining text, as with Bash's recursion
	for t := from + 1; t < to; t++ {
		if s.toks[t].kind != '{' || s.ignoreOpen(t, textFrom, to) {
			continue
		}
		match := s.toks[t].matchAt
		if match >= to {
			continue
		}
		var br *BraceExp
		if s.toks[t].matchSeq {
			if !s.looseSequence(t, match) {
				continue
			}
			// Like Bash, a sequence which looks valid but fails to expand
			// is kept as literal text along with any braces within it.
			br = s.sequence(t, match)
		} else {
			s.depth++
			br = s.list(t, match)
			s.depth--
		}
		if br != nil {
			br.Lbrace, br.Rbrace = s.pos(t), s.pos(match)
			if parts == nil {
				parts = make([]WordPart, 0, 4)
			}
			parts = s.appendLit(parts, litFrom, t)
			parts = append(parts, br)
			litFrom = match
		}
		textFrom = match
		t = match
	}
	if parts == nil && s.depth == 0 {
		return nil // no brace expansions in the whole word
	}
	return s.appendLit(parts, litFrom, to)
}

// ignoreOpen reports whether Bash ignores the opening brace at token t
// within the text between tokens from and to.
func (s *braceSplitter) ignoreOpen(t, from, to int) bool {
	off := s.toks[t].off
	start, end := s.span(from, to)
	space := func(b byte) bool { return b == ' ' || b == '\t' || b == '\n' }
	return (off == start || space(s.text[off-1])) &&
		(off+1 == end || space(s.text[off+1]) || s.text[off+1] == '}')
}

// list splits the text between the braces at tokens open and close
// into the elements of a brace expansion.
func (s *braceSplitter) list(open, close int) *BraceExp {
	br := &BraceExp{Elems: make([]*Word, 0, 2)}
	elemFrom := open
	for t := open + 1; t < close; t++ {
		switch s.toks[t].kind {
		case '{':
			t = s.toks[t].closeAt // skip nested braces
		case ',':
			br.Elems = append(br.Elems, &Word{Parts: s.split(elemFrom, t)})
			elemFrom = t
		}
	}
	br.Elems = append(br.Elems, &Word{Parts: s.split(elemFrom, close)})
	return br
}

// sequence splits the text between the braces at tokens open and close
// into the elements of a sequence expression, returning nil if it is malformed.
func (s *braceSplitter) sequence(open, close int) *BraceExp {
	// Validate the terms as text, so that a malformed sequence allocates no nodes.
	from, to := [3]int{open}, [3]int{} // the tokens around each term
	n := 1
	for t := open + 1; t < close; t++ {
		if s.dots(t) {
			if n == 3 {
				return nil // sequences like {1..2..3..4} are literal in bash
			}
			to[n-1], from[n] = t, t+1
			n++
			t++
		}
	}
	to[n-1] = close
	var terms [3]string
	for i := range n {
		start, end := s.span(from[i], to[i])
		terms[i] = s.text[start:end]
	}
	isInt := func(val string) bool {
		// ParseInt with bit size 64 to ensure consistent behavior on 32-bit platforms.
		_, err := strconv.ParseInt(val, 10, 64)
		return err == nil
	}
	isChar := func(val string) bool { return len(val) == 1 && asciiLetter(val[0]) }
	// The start and end must both be integers or both be single letters.
	ok := isChar(terms[0]) && isChar(terms[1]) || isInt(terms[0]) && isInt(terms[1])
	if !ok || n == 3 && !isInt(terms[2]) {
		return nil
	}

	br := &BraceExp{Sequence: true, Elems: make([]*Word, 0, n)}
	words := make([]Word, n) // one allocation for all the elements
	for i := range n {
		words[i].Parts = s.appendLit(nil, from[i], to[i])
		br.Elems = append(br.Elems, &words[i])
	}
	return br
}

// looseSequence reports whether the sequence expression between the braces at
// tokens open and close looks valid at a glance, following Bash's valid_seqterm,
// which only checks the start of each term around the first "..".
func (s *braceSplitter) looseSequence(open, close int) bool {
	d := s.toks[open].dotsAt
	if d >= close {
		return false
	}
	start, end := s.span(open, close)
	lhs, rhs := s.text[start:s.toks[d].off], s.text[s.toks[d].off+2:end]
	if len(lhs) == 0 || len(rhs) == 0 {
		return false
	}
	intStart := func(s string) bool {
		return asciiDigit(s[0]) || ((s[0] == '+' || s[0] == '-') && len(s) > 1 && asciiDigit(s[1]))
	}
	charStart := func(s string) bool {
		return asciiLetter(s[0]) && (len(s) == 1 || s[1] == '.')
	}
	return (intStart(lhs) && intStart(rhs)) || (charStart(lhs) && charStart(rhs))
}

// partAt returns the index of the part containing text[off].
func (s *braceSplitter) partAt(off int) int {
	k, found := slices.BinarySearch(s.offs, off)
	if !found {
		k--
	}
	return k
}

// pos returns the position of token t, which must be within a literal.
// Like [braceSplitter.appendLit], it is approximate after an escaped newline,
// which is not part of a literal's value.
func (s *braceSplitter) pos(t int) Pos {
	off := s.toks[t].off
	k := s.partAt(off)
	lit := s.parts[k].(*Lit)
	i := off - s.offs[k]
	if i == len(lit.Value)-1 {
		// A token ending the literal is placed from its end, which is exact.
		return posAddCol(lit.ValueEnd, -1)
	}
	return posAddCol(lit.ValuePos, i)
}

// appendLit appends the parts making up the text between tokens from and to,
// slicing any literals which are only partially included.
func (s *braceSplitter) appendLit(parts []WordPart, from, to int) []WordPart {
	start, end := s.span(from, to)
	if start >= end {
		return parts
	}
	for k := s.partAt(start); k < len(s.parts) && s.offs[k] < end; k++ {
		lit, ok := s.parts[k].(*Lit)
		if !ok {
			parts = append(parts, s.parts[k])
			continue
		}
		i, j := max(start-s.offs[k], 0), min(end-s.offs[k], len(lit.Value))
		if i >= j {
			continue
		}
		if i > 0 || j < len(lit.Value) {
			l2 := *lit
			l2.Value = lit.Value[i:j]
			l2.ValuePos = posAddCol(lit.ValuePos, i)
			if j < len(lit.Value) {
				l2.ValueEnd = posAddCol(lit.ValuePos, j)
			}
			lit = &l2
		}
		parts = append(parts, lit)
	}
	return parts
}
