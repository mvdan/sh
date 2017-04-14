// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"fmt"
	"strconv"

	"github.com/mvdan/sh/syntax"
)

func (r *Runner) arithm(expr syntax.ArithmExpr) int {
	switch x := expr.(type) {
	case *syntax.Word:
		str := r.loneWord(x)
		// recursively fetch vars
		for {
			val := r.getVar(str)
			if val == "" {
				break
			}
			str = val
		}
		// default to 0
		n, _ := strconv.Atoi(str)
		return n
	case *syntax.ParenArithm:
		return r.arithm(x.X)
	case *syntax.UnaryArithm:
		switch x.Op {
		case syntax.Inc, syntax.Dec:
			word, ok := x.X.(*syntax.Word)
			if !ok {
				// TODO: error?
				return 0
			}
			name := r.loneWord(word)
			old, _ := strconv.Atoi(r.getVar(name)) // TODO: error?
			val := old
			if x.Op == syntax.Inc {
				val++
			} else {
				val--
			}
			r.setVar(name, strconv.Itoa(val))
			if x.Post {
				return old
			}
			return val
		}
		val := r.arithm(x.X)
		switch x.Op {
		case syntax.Not:
			return boolArit(val == 0)
		case syntax.Plus:
			return val
		default: // syntax.Minus
			return -val
		}
	case *syntax.BinaryArithm:
		switch x.Op {
		case syntax.Assgn, syntax.AddAssgn, syntax.SubAssgn,
			syntax.MulAssgn, syntax.QuoAssgn, syntax.RemAssgn,
			syntax.AndAssgn, syntax.OrAssgn, syntax.XorAssgn,
			syntax.ShlAssgn, syntax.ShrAssgn:
			return r.assgnArit(x)
		case syntax.Colon:
			// TODO: error
		case syntax.Quest:
			cond := r.arithm(x.X)
			b2, ok := x.Y.(*syntax.BinaryArithm)
			if !ok || b2.Op != syntax.Colon {
				// TODO: error
				return 0
			}
			if cond == 1 {
				return r.arithm(b2.X)
			}
			return r.arithm(b2.Y)
		}
		return binArit(x.Op, r.arithm(x.X), r.arithm(x.Y))
	default:
		panic(fmt.Sprintf("unexpected arithm expr: %T", x))
	}
}

func boolArit(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (r *Runner) assgnArit(b *syntax.BinaryArithm) int {
	word, ok := b.X.(*syntax.Word)
	if !ok {
		// TODO: error?
		return 0
	}
	name := r.loneWord(word)
	val, _ := strconv.Atoi(r.getVar(name)) // TODO: error?
	arg := r.arithm(b.Y)
	switch b.Op {
	case syntax.Assgn:
		val = arg
	case syntax.AddAssgn:
		val += arg
	case syntax.SubAssgn:
		val -= arg
	case syntax.MulAssgn:
		val *= arg
	case syntax.QuoAssgn:
		val /= arg
	case syntax.RemAssgn:
		val %= arg
	case syntax.AndAssgn:
		val &= arg
	case syntax.OrAssgn:
		val |= arg
	case syntax.XorAssgn:
		val ^= arg
	case syntax.ShlAssgn:
		val <<= uint(arg)
	default: // syntax.ShrAssgn
		val >>= uint(arg)
	}
	r.setVar(name, strconv.Itoa(val))
	return val
}

func intPow(a, b int) int {
	p := 1
	for b > 0 {
		if b&1 != 0 {
			p *= a
		}
		b >>= 1
		a *= a
	}
	return p
}

func binArit(op syntax.BinAritOperator, x, y int) int {
	switch op {
	case syntax.Add:
		return x + y
	case syntax.Sub:
		return x - y
	case syntax.Mul:
		return x * y
	case syntax.Quo:
		return x / y
	case syntax.Rem:
		return x % y
	case syntax.Pow:
		return intPow(x, y)
	case syntax.Eql:
		return boolArit(x == y)
	case syntax.Gtr:
		return boolArit(x > y)
	case syntax.Lss:
		return boolArit(x < y)
	case syntax.Neq:
		return boolArit(x != y)
	case syntax.Leq:
		return boolArit(x <= y)
	case syntax.Geq:
		return boolArit(x >= y)
	case syntax.And:
		return x & y
	case syntax.Or:
		return x | y
	case syntax.Xor:
		return x ^ y
	case syntax.Shr:
		return x >> uint(y)
	case syntax.Shl:
		return x << uint(y)
	case syntax.AndArit:
		return boolArit(x != 0 && y != 0)
	case syntax.OrArit:
		return boolArit(x != 0 || y != 0)
	default: // syntax.Comma
		// x is executed but its result discarded
		return y
	}
}
