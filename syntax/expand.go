// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import "strconv"

var (
	litLeftBrace  = &Lit{Value: "{"}
	litComma      = &Lit{Value: ","}
	litDots       = &Lit{Value: ".."}
	litRightBrace = &Lit{Value: "}"}
)

func splitBraces(word *Word) (*Word, bool) {
	any := false
	top := &Word{}
	acc := top
	var cur *BraceExp
	open := []*BraceExp{}

	pop := func() *BraceExp {
		old := cur
		open = open[:len(open)-1]
		if len(open) == 0 {
			cur = nil
			acc = top
		} else {
			cur = open[len(open)-1]
			acc = cur.Elems[len(cur.Elems)-1]
		}
		return old
	}
	addLit := func(lit *Lit) {
		acc.Parts = append(acc.Parts, lit)
	}
	addParts := func(parts ...WordPart) {
		acc.Parts = append(acc.Parts, parts...)
	}

	for _, wp := range word.Parts {
		lit, ok := wp.(*Lit)
		if !ok {
			addParts(wp)
			continue
		}
		last := 0
		for j := 0; j < len(lit.Value); j++ {
			addlitidx := func() {
				if last == j {
					return // empty lit
				}
				l2 := *lit
				l2.Value = l2.Value[last:j]
				addLit(&l2)
			}
			switch lit.Value[j] {
			case '{':
				addlitidx()
				acc = &Word{}
				cur = &BraceExp{Elems: []*Word{acc}}
				open = append(open, cur)
			case ',':
				if cur == nil {
					continue
				}
				addlitidx()
				acc = &Word{}
				cur.Elems = append(cur.Elems, acc)
			case '.':
				if cur == nil {
					continue
				}
				if j+1 >= len(lit.Value) || lit.Value[j+1] != '.' {
					continue
				}
				addlitidx()
				cur.Sequence = true
				acc = &Word{}
				cur.Elems = append(cur.Elems, acc)
				j++
			case '}':
				if cur == nil {
					continue
				}
				any = true
				addlitidx()
				br := pop()
				if len(br.Elems) == 1 {
					// return {x} to a non-brace
					addLit(litLeftBrace)
					addParts(br.Elems[0].Parts...)
					addLit(litRightBrace)
					break
				}
				if !br.Sequence {
					addParts(br)
					break
				}
				var chars [2]bool
				broken := false
				for i, elem := range br.Elems[:2] {
					val := elem.Lit()
					if _, err := strconv.Atoi(val); err == nil {
					} else if len(val) == 1 &&
						'a' <= val[0] && val[0] <= 'z' {
						chars[i] = true
					} else {
						broken = true
					}
				}
				if len(br.Elems) == 3 {
					// increment must be a number
					val := br.Elems[2].Lit()
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
					br.Chars = chars[0]
					addParts(br)
					break
				}
				// return broken {x..y[..incr]} to a non-brace
				addLit(litLeftBrace)
				for i, elem := range br.Elems {
					if i > 0 {
						addLit(litDots)
					}
					addParts(elem.Parts...)
				}
				addLit(litRightBrace)
			default:
				continue
			}
			last = j + 1
		}
		if last == 0 {
			addLit(lit)
		} else {
			left := *lit
			left.Value = left.Value[last:]
			addLit(&left)
		}
	}
	// open braces that were never closed fall back to non-braces
	for acc != top {
		br := pop()
		addLit(litLeftBrace)
		for i, elem := range br.Elems {
			if i > 0 {
				if br.Sequence {
					addLit(litDots)
				} else {
					addLit(litComma)
				}
			}
			addParts(elem.Parts...)
		}
	}
	return top, any
}

func braceWordLit(word *Word) string {
	return word.Lit()
}

func expandRec(word *Word) []*Word {
	var all []*Word
	var left []WordPart
	for i, wp := range word.Parts {
		br, ok := wp.(*BraceExp)
		if !ok {
			left = append(left, wp.(WordPart))
			continue
		}
		if br.Sequence {
			var from, to int
			if br.Chars {
				from = int(br.Elems[0].Lit()[0])
				to = int(br.Elems[1].Lit()[0])
			} else {
				from, _ = strconv.Atoi(br.Elems[0].Lit())
				to, _ = strconv.Atoi(br.Elems[1].Lit())
			}
			upward := from <= to
			incr := 1
			if !upward {
				incr = -1
			}
			if len(br.Elems) > 2 {
				n, _ := strconv.Atoi(br.Elems[2].Lit())
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
				next := *word
				next.Parts = next.Parts[i+1:]
				lit := &Lit{}
				if br.Chars {
					lit.Value = string(n)
				} else {
					lit.Value = strconv.Itoa(n)
				}
				next.Parts = append([]WordPart{lit}, next.Parts...)
				exp := expandRec(&next)
				for _, w := range exp {
					w.Parts = append(left, w.Parts...)
				}
				all = append(all, exp...)
				n += incr
			}
			return all
		}
		for _, elem := range br.Elems {
			next := *word
			next.Parts = next.Parts[i+1:]
			next.Parts = append(elem.Parts, next.Parts...)
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

// TODO(v3): remove

// ExpandBraces performs Bash brace expansion on a word. For example,
// passing it a single-literal word "foo{bar,baz}" will return two
// single-literal words, "foobar" and "foobaz".
//
// Deprecated: use mvdan.cc/sh/v3/expand.Braces instead.
func ExpandBraces(word *Word) []*Word {
	topBrace, any := splitBraces(word)
	if !any {
		return []*Word{word}
	}
	return expandRec(topBrace)
}
