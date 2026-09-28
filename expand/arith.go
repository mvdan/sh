// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package expand

import (
	"fmt"
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// TODO(v4): the arithmetic APIs should return int64, which is what Bash uses
// via intmax_t even on 32-bit systems, rather than the platform-dependent int.

// Arithm evaluates an arithmetic expression, such as the one in `$((expr))`.
//
// The config specifies shell expansion options; nil behaves the same as an
// empty config.
func Arithm(cfg *Config, expr syntax.ArithmExpr) (int, error) {
	e := newExpander(cfg)
	defer e.release()
	return e.arithm(expr)
}

func (e *expander) arithm(expr syntax.ArithmExpr) (int, error) {
	e.depth++
	n, err := e.arithmExpr(expr)
	e.depth--
	return n, err
}

func (e *expander) arithmExpr(expr syntax.ArithmExpr) (int, error) {
	switch expr := expr.(type) {
	case *syntax.Word:
		str, err := e.literal(expr)
		if err != nil {
			return 0, err
		}
		// recursively fetch vars
		i := 0
		for syntax.ValidName(str) {
			val := e.envGet(str)
			if val == "" {
				break
			}
			if i++; i >= maxNameRefDepth {
				break
			}
			str = val
		}
		// default to 0
		return int(atoi(str)), nil
	case *syntax.ParenArithm:
		return e.arithm(expr.X)
	case *syntax.UnaryArithm:
		switch expr.Op {
		case syntax.Inc, syntax.Dec:
			name, idx, old, err := e.arithmVar(expr.X)
			if err != nil {
				return 0, err
			}
			val := old
			if expr.Op == syntax.Inc {
				val++
			} else {
				val--
			}
			if err := e.assignElem(name, e.Env.Get(name), idx, strconv.FormatInt(val, 10)); err != nil {
				return 0, err
			}
			if expr.Post {
				return int(old), nil
			}
			return int(val), nil
		}
		val, err := e.arithm(expr.X)
		if err != nil {
			return 0, err
		}
		switch expr.Op {
		case syntax.Not:
			return oneIf(val == 0), nil
		case syntax.BitNegation:
			return ^val, nil
		case syntax.Plus:
			return val, nil
		case syntax.Minus:
			return -val, nil
		default:
			return 0, fmt.Errorf("unsupported unary arithmetic operator: %q", expr.Op)
		}
	case *syntax.BinaryArithm:
		switch expr.Op {
		case syntax.Assgn, syntax.AddAssgn, syntax.SubAssgn,
			syntax.MulAssgn, syntax.QuoAssgn, syntax.RemAssgn,
			syntax.AndAssgn, syntax.OrAssgn, syntax.XorAssgn,
			syntax.ShlAssgn, syntax.ShrAssgn:
			return e.assgnArit(expr)
		case syntax.TernQuest: // TernColon can't happen here
			cond, err := e.arithm(expr.X)
			if err != nil {
				return 0, err
			}
			b2 := expr.Y.(*syntax.BinaryArithm) // must have Op==TernColon
			if cond != 0 {
				return e.arithm(b2.X)
			}
			return e.arithm(b2.Y)
		case syntax.AndArit, syntax.OrArit:
			// Like Bash, short-circuit the right operand.
			left, err := e.arithm(expr.X)
			if err != nil {
				return 0, err
			}
			if expr.Op == syntax.AndArit && left == 0 {
				return 0, nil
			}
			if expr.Op == syntax.OrArit && left != 0 {
				return 1, nil
			}
			right, err := e.arithm(expr.Y)
			if err != nil {
				return 0, err
			}
			return oneIf(right != 0), nil
		}
		left, err := e.arithm(expr.X)
		if err != nil {
			return 0, err
		}
		right, err := e.arithm(expr.Y)
		if err != nil {
			return 0, err
		}
		return binArit(expr.Op, left, right)
	case *syntax.FlagsArithm: // e.g. zsh's ${a[(r)b]}
		return 0, fmt.Errorf("unsupported")
	default:
		panic(fmt.Sprintf("unexpected arithm expr: %T", expr))
	}
}

func oneIf(b bool) int {
	if b {
		return 1
	}
	return 0
}

// atoi is like [strconv.ParseInt](s, BASE, 64), but it handles integer
// base prefixes according to bash-shell's rules, ignores errors, and
// trims whitespace.
//
// For more information about bash's integer base handling syntax,
// refer to the bash manual:
// https://www.man7.org/linux/man-pages/man1/bash.1.html
func atoi(s string) int64 {
	s = strings.TrimSpace(s)
	neg := false
	switch {
	case strings.HasPrefix(s, "+"):
		s = s[1:]
	case strings.HasPrefix(s, "-"):
		neg = true
		s = s[1:]
	}
	base := int64(10)
	switch {
	case strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X"):
		base = 16
		s = s[2:]
	case strings.HasPrefix(s, "0"):
		base = 8
		s = s[1:]
	default:
		baseStr, intStr, hasSep := strings.Cut(s, "#")
		if hasSep {
			var err error
			base, err = strconv.ParseInt(baseStr, 10, 8)
			if err != nil || base < 2 || base > 64 {
				return 0
			}
			s = intStr
		}
	}
	var n int64
	if base > 36 {
		n = atoiLargeBase(s, base)
	} else {
		n, _ = strconv.ParseInt(s, int(base), 64)
	}
	if neg {
		n = -n
	}
	return n
}

// atoiLargeBase parses bases 37 to 64, which [strconv.ParseInt] does not
// support, using bash's digit set: 0-9, a-z, A-Z, "@", and "_".
func atoiLargeBase(s string, base int64) int64 {
	var n int64
	for i := range len(s) {
		var d int64
		switch c := s[i]; {
		case c >= '0' && c <= '9':
			d = int64(c - '0')
		case c >= 'a' && c <= 'z':
			d = int64(c-'a') + 10
		case c >= 'A' && c <= 'Z':
			d = int64(c-'A') + 36
		case c == '@':
			d = 62
		case c == '_':
			d = 63
		default:
			return 0
		}
		if d >= base {
			return 0
		}
		n = n*base + d
	}
	return n
}

// arithmVar resolves an arithmetic assignment target like x or a[i] into
// a variable name, a subscript which is nil for x, and its current value.
// The subscript is evaluated once, as it may have side effects like a[i++].
func (e *expander) arithmVar(expr syntax.ArithmExpr) (string, syntax.ArithmExpr, int64, error) {
	word, _ := expr.(*syntax.Word)
	if word == nil {
		return "", nil, 0, fmt.Errorf("attempted assignment to non-variable")
	}
	name := word.Lit()
	var idx syntax.ArithmExpr
	var pe *syntax.ParamExp
	if len(word.Parts) == 1 {
		pe, _ = word.Parts[0].(*syntax.ParamExp)
	}
	if pe != nil && pe.Index != nil {
		name = pe.Param.Value
		switch lit := nodeLit(pe.Index); lit {
		case "@", "*":
			return "", nil, 0, fmt.Errorf("%s[%s]: bad array subscript", name, lit)
		}
		var key string
		if e.Env.Get(name).Kind == Associative {
			var err error
			if key, err = e.assocKey(pe.Index); err != nil {
				return "", nil, 0, err
			}
		} else {
			i, err := e.arithm(pe.Index)
			if err != nil {
				return "", nil, 0, err
			}
			key = strconv.Itoa(i)
		}
		idx = &syntax.Word{Parts: []syntax.WordPart{&syntax.SglQuoted{Value: key}}}
	}
	if name == "" {
		return "", nil, 0, fmt.Errorf("attempted assignment to non-variable")
	}
	str, _, err := e.varInd(e.Env.Get(name), idx)
	return name, idx, atoi(str), err
}

func (e *expander) assgnArit(b *syntax.BinaryArithm) (int, error) {
	name, idx, val, err := e.arithmVar(b.X)
	if err != nil {
		return 0, err
	}
	arg_, err := e.arithm(b.Y)
	if err != nil {
		return 0, err
	}
	arg := int64(arg_)
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
		if arg == 0 {
			return 0, fmt.Errorf("division by zero")
		}
		val /= arg
	case syntax.RemAssgn:
		if arg == 0 {
			return 0, fmt.Errorf("division by zero")
		}
		val %= arg
	case syntax.AndAssgn:
		val &= arg
	case syntax.OrAssgn:
		val |= arg
	case syntax.XorAssgn:
		val ^= arg
	case syntax.ShlAssgn:
		val <<= uint(arg)
	case syntax.ShrAssgn:
		val >>= uint(arg)
	}
	// Get the variable again, as evaluating an expression like
	// a[0] = (a[1] = 5) may have modified other elements.
	if err := e.assignElem(name, e.Env.Get(name), idx, strconv.FormatInt(val, 10)); err != nil {
		return 0, err
	}
	return int(val), nil
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

func binArit(op syntax.BinAritOperator, x, y int) (int, error) {
	switch op {
	case syntax.Add:
		return x + y, nil
	case syntax.Sub:
		return x - y, nil
	case syntax.Mul:
		return x * y, nil
	case syntax.Quo:
		if y == 0 {
			return 0, fmt.Errorf("division by zero")
		}
		return x / y, nil
	case syntax.Rem:
		if y == 0 {
			return 0, fmt.Errorf("division by zero")
		}
		return x % y, nil
	case syntax.Pow:
		if y < 0 {
			return 0, fmt.Errorf("exponent less than 0")
		}
		return intPow(x, y), nil
	case syntax.Eql:
		return oneIf(x == y), nil
	case syntax.Gtr:
		return oneIf(x > y), nil
	case syntax.Lss:
		return oneIf(x < y), nil
	case syntax.Neq:
		return oneIf(x != y), nil
	case syntax.Leq:
		return oneIf(x <= y), nil
	case syntax.Geq:
		return oneIf(x >= y), nil
	case syntax.And:
		return x & y, nil
	case syntax.Or:
		return x | y, nil
	case syntax.Xor:
		return x ^ y, nil
	case syntax.Shr:
		return x >> uint(y), nil
	case syntax.Shl:
		return x << uint(y), nil
	case syntax.Comma:
		// x is executed but its result discarded
		return y, nil
	default:
		return 0, fmt.Errorf("unsupported binary arithmetic operator: %q", op)
	}
}
