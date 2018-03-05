// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import "strconv"

// TODO: consider making these special syntax nodes

type brace struct {
	seq   bool // {x..y[..incr]} instead of {x,y[,...]}
	chars bool // sequence is of chars, not numbers
	elems []*braceWord
}

// braceWord is like Word, but with braceWordPart.
type braceWord struct {
	parts []braceWordPart
}

// braceWordPart contains any WordPart or a brace.
type braceWordPart interface{}

var (
	litLeftBrace  = &Lit{Value: "{"}
	litComma      = &Lit{Value: ","}
	litDots       = &Lit{Value: ".."}
	litRightBrace = &Lit{Value: "}"}
)

func splitBraces(word *Word) *braceWord {
	top := &braceWord{}
	acc := top
	var cur *brace
	open := []*brace{}

	pop := func() *brace {
		old := cur
		open = open[:len(open)-1]
		if len(open) == 0 {
			cur = nil
			acc = top
		} else {
			cur = open[len(open)-1]
			acc = cur.elems[len(cur.elems)-1]
		}
		return old
	}

	for _, wp := range word.Parts {
		lit, ok := wp.(*Lit)
		if !ok {
			acc.parts = append(acc.parts, wp)
			continue
		}
		last := 0
		for j := 0; j < len(lit.Value); j++ {
			addlit := func() {
				if last == j {
					return // empty lit
				}
				l2 := *lit
				l2.Value = l2.Value[last:j]
				acc.parts = append(acc.parts, &l2)
			}
			switch lit.Value[j] {
			case '{':
				addlit()
				acc = &braceWord{}
				cur = &brace{elems: []*braceWord{acc}}
				open = append(open, cur)
			case ',':
				if cur == nil {
					continue
				}
				addlit()
				acc = &braceWord{}
				cur.elems = append(cur.elems, acc)
			case '.':
				if cur == nil {
					continue
				}
				if j+1 >= len(lit.Value) || lit.Value[j+1] != '.' {
					continue
				}
				addlit()
				cur.seq = true
				acc = &braceWord{}
				cur.elems = append(cur.elems, acc)
				j++
			case '}':
				if cur == nil {
					continue
				}
				addlit()
				br := pop()
				if len(br.elems) == 1 {
					// return {x} to a non-brace
					acc.parts = append(acc.parts, litLeftBrace)
					acc.parts = append(acc.parts, br.elems[0].parts...)
					acc.parts = append(acc.parts, litRightBrace)
					break
				}
				if !br.seq {
					acc.parts = append(acc.parts, br)
					break
				}
				var chars [2]bool
				broken := false
				for i, elem := range br.elems[:2] {
					val := braceWordLit(elem)
					if _, err := strconv.Atoi(val); err == nil {
					} else if len(val) == 1 &&
						'a' <= val[0] && val[0] <= 'z' {
						chars[i] = true
					} else {
						broken = true
					}
				}
				if len(br.elems) == 3 {
					// increment must be a number
					val := braceWordLit(br.elems[2])
					if _, err := strconv.Atoi(val); err != nil {
						broken = true
					}
				}
				// are start and end both chars or
				// non-chars?
				if chars[0] != chars[1] {
					broken = true
				}
				if !broken {
					br.chars = chars[0]
					acc.parts = append(acc.parts, br)
					break
				}
				// return broken {x..y[..incr]} to a non-brace
				acc.parts = append(acc.parts, litLeftBrace)
				for i, elem := range br.elems {
					if i > 0 {
						acc.parts = append(acc.parts, litDots)
					}
					acc.parts = append(acc.parts, elem.parts...)
				}
				acc.parts = append(acc.parts, litRightBrace)
			default:
				continue
			}
			last = j + 1
		}
		if last == 0 {
			acc.parts = append(acc.parts, lit)
		} else {
			left := *lit
			left.Value = left.Value[last:]
			acc.parts = append(acc.parts, &left)
		}
	}
	// open braces that were never closed fall back to non-braces
	for acc != top {
		br := pop()
		acc.parts = append(acc.parts, litLeftBrace)
		for i, elem := range br.elems {
			if i > 0 {
				if br.seq {
					acc.parts = append(acc.parts, litDots)
				} else {
					acc.parts = append(acc.parts, litComma)
				}
			}
			acc.parts = append(acc.parts, elem.parts...)
		}
	}
	return top
}

func braceWordLit(v interface{}) string {
	word, _ := v.(*braceWord)
	if word == nil || len(word.parts) != 1 {
		return ""
	}
	lit, ok := word.parts[0].(*Lit)
	if !ok {
		return ""
	}
	return lit.Value
}

func expandRec(bw *braceWord) []*Word {
	var all []*Word
	var left []WordPart
	for i, wp := range bw.parts {
		br, ok := wp.(*brace)
		if !ok {
			left = append(left, wp.(WordPart))
			continue
		}
		if br.seq {
			var from, to int
			if br.chars {
				from = int(braceWordLit(br.elems[0])[0])
				to = int(braceWordLit(br.elems[1])[0])
			} else {
				from, _ = strconv.Atoi(braceWordLit(br.elems[0]))
				to, _ = strconv.Atoi(braceWordLit(br.elems[1]))
			}
			upward := from <= to
			incr := 1
			if !upward {
				incr = -1
			}
			if len(br.elems) > 2 {
				val := braceWordLit(br.elems[2])
				n, _ := strconv.Atoi(val)
				if n != 0 && n > 0 == upward {
					incr = n
				}
			}
			n := from
			for {
				if upward && n > to {
					break
				}
				if !upward && n < to {
					break
				}
				next := *bw
				next.parts = next.parts[i+1:]
				lit := &Lit{}
				if br.chars {
					lit.Value = string(n)
				} else {
					lit.Value = strconv.Itoa(n)
				}
				next.parts = append([]braceWordPart{lit}, next.parts...)
				exp := expandRec(&next)
				for _, w := range exp {
					w.Parts = append(left, w.Parts...)
				}
				all = append(all, exp...)
				n += incr
			}
			return all
		}
		for _, elem := range br.elems {
			next := *bw
			next.parts = next.parts[i+1:]
			next.parts = append(elem.parts, next.parts...)
			exp := expandRec(&next)
			for _, w := range exp {
				w.Parts = append(left, w.Parts...)
			}
			all = append(all, exp...)
		}
		return all
	}
	return []*Word{{Parts: left}}
}

// ExpandBraces performs Bash brace expansion on a word. For example,
// passing it a single-literal word "foo{bar,baz}" will return two
// single-literal words, "foobar" and "foobaz".
//
// It does not return an error; malformed brace expansions are simply
// skipped. For example, "a{b{c,d}" results in the words "a{bc" and
// "a{bd".
func ExpandBraces(word *Word) []*Word {
	topBrace := splitBraces(word)
	return expandRec(topBrace)
}
