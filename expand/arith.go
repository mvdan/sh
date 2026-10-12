// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package expand

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/internal"
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

// arithmExp evaluates an expression within an expansion like $((expr)) or
// ${x:offset}, where any error is fatal, unlike in an arithmetic command.
func (e *expander) arithmExp(expr syntax.ArithmExpr) (int, error) {
	n, err := e.arithm(expr)
	return n, internal.Fatal(err)
}

// subscript evaluates the subscript of an indexed array element like a[i].
// Like in Bash, an error doing so is fatal, even in an arithmetic command.
func (e *expander) subscript(idx syntax.ArithmExpr) (int, error) {
	n, err := e.arithm(idx)
	return n, internal.Fatal(err)
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
			return 0, internal.ArithmExpandError{Err: err}
		}
		return e.arithmValue(str)
	case *syntax.ParenArithm:
		return e.arithm(expr.X)
	case *syntax.UnaryArithm:
		switch expr.Op {
		case syntax.Inc, syntax.Dec:
			name, idx, oldStr, err := e.arithmVar(expr.X)
			if errors.Is(err, errNotVariable) {
				// Like Bash, --5 negates twice rather than decrementing.
				return e.arithm(expr.X)
			}
			if err != nil {
				return 0, err
			}
			old, err := e.arithmValue(oldStr)
			if err != nil {
				return 0, err
			}
			val := int64(old)
			if expr.Op == syntax.Inc {
				val++
			} else {
				val--
			}
			if err := e.assignElem(name, e.Env.Get(name), idx, strconv.FormatInt(val, 10)); err != nil {
				return 0, err
			}
			if expr.Post {
				return old, nil
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

// errNotVariable is returned by [expander.arithmVar] when an assignment or
// increment target is not a variable, such as in $((5=3)).
var errNotVariable = errors.New("attempted assignment to non-variable")

// maxArithmRecursion is the number of variables whose values may be
// evaluated as nested arithmetic expressions, like Bash's limit.
const maxArithmRecursion = 1024

// arithmValue evaluates a word in an arithmetic expression, which is a
// number or the name of a variable. Like in Bash, a variable's value is
// evaluated as an arithmetic expression in turn, so that `a='1+2'` gives
// `$((a*2))` a value of 6, and an unset or empty variable is zero.
// A quoted word like `let "a = 1 + 2"` is an expression as well.
func (e *expander) arithmValue(str string) (int, error) {
	str = strings.TrimSpace(str)
	if syntax.ValidName(str) {
		return e.arithmString(e.envGet(str), str)
	}
	return e.arithmString(str, str)
}

// arithmString evaluates a string as an arithmetic expression, where token
// names it in errors: the variable holding the value, or the string itself.
func (e *expander) arithmString(str, token string) (int, error) {
	str = strings.TrimSpace(str)
	if str == "" {
		return 0, nil
	}
	if n, ok := parseArithmNum(str); ok {
		return int(n), nil // the common case, without parsing an expression
	}
	if e.arithmRecursion >= maxArithmRecursion {
		return 0, fmt.Errorf("%s: expression recursion level exceeded", token)
	}
	e.arithmRecursion++
	defer func() { e.arithmRecursion-- }()
	if syntax.ValidName(str) {
		return e.arithmString(e.envGet(str), str)
	}
	expr, err := e.parseArithm(str)
	if err != nil {
		return 0, fmt.Errorf("%s: arithmetic syntax error in expression", str)
	}
	if !arithmValueExpr(expr) {
		return 0, fmt.Errorf("%s: arithmetic syntax error: operand expected", str)
	}
	if word, ok := expr.(*syntax.Word); ok && word.Lit() == str {
		// A single token which is neither a number nor a name.
		return 0, fmt.Errorf("%s: value too great for base", str)
	}
	return e.arithm(expr)
}

// arithmValueExpr reports whether expr, parsed from a variable's value,
// contains no expansions like $y or $(cmd), which Bash does not perform
// on values, except within an array subscript like a[i].
func arithmValueExpr(expr syntax.ArithmExpr) bool {
	switch expr := expr.(type) {
	case *syntax.Word:
		for _, part := range expr.Parts {
			switch part := part.(type) {
			case *syntax.Lit:
			case *syntax.ParamExp:
				if !part.Short || part.Index == nil || part.Dollar.IsValid() {
					return false // only a[i], whose subscript may expand
				}
			default:
				return false
			}
		}
		return true
	case *syntax.ParenArithm:
		return arithmValueExpr(expr.X)
	case *syntax.UnaryArithm:
		return arithmValueExpr(expr.X)
	case *syntax.BinaryArithm:
		return arithmValueExpr(expr.X) && arithmValueExpr(expr.Y)
	}
	return false
}

// parseArithmNum parses a number in an arithmetic expression according to
// Bash's rules, such as 12, -0x1f, 010 in octal, or 2#101 in binary.
// Like in Bash, a number too large for 64 bits wraps around.
// ok is false when s is not a valid number.
func parseArithmNum(s string) (n int64, ok bool) {
	neg := false
	switch {
	case strings.HasPrefix(s, "+"):
		s = s[1:]
	case strings.HasPrefix(s, "-"):
		neg = true
		s = s[1:]
	}
	base := uint64(10)
	switch {
	case strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X"):
		base = 16
		s = s[2:]
	case strings.HasPrefix(s, "0") && len(s) > 1:
		base = 8
		s = s[1:]
	default:
		baseStr, intStr, hasSep := strings.Cut(s, "#")
		if hasSep {
			b, err := strconv.ParseUint(baseStr, 10, 8)
			if err != nil || b < 2 || b > 64 {
				return 0, false
			}
			base = b
			s = intStr
		}
	}
	if s == "" {
		return 0, false
	}
	var u uint64
	for i := range len(s) {
		d, ok := arithmDigit(s[i], base)
		if !ok {
			return 0, false
		}
		u = u*base + d
	}
	n = int64(u)
	if neg {
		n = -n
	}
	return n, true
}

// arithmDigit returns the value of a digit in the given base, using Bash's
// digit set: 0-9, a-z, A-Z, "@", and "_". Up to base 36, letters are
// case-insensitive; above it, uppercase letters follow lowercase ones.
func arithmDigit(c byte, base uint64) (uint64, bool) {
	var d uint64
	switch {
	case c >= '0' && c <= '9':
		d = uint64(c - '0')
	case c >= 'a' && c <= 'z':
		d = uint64(c-'a') + 10
	case c >= 'A' && c <= 'Z':
		d = uint64(c-'A') + 10
		if base > 36 {
			d += 26
		}
	case c == '@':
		d = 62
	case c == '_':
		d = 63
	default:
		return 0, false
	}
	return d, d < base
}

// arithmVar resolves an arithmetic assignment target like x or a[i] into
// a variable name with any nameref followed, a subscript which is nil for
// x, and its current value as a string, which [expander.arithmValue] can
// evaluate. The subscript is evaluated once, as it may have side effects
// like a[i++].
func (e *expander) arithmVar(expr syntax.ArithmExpr) (string, syntax.ArithmExpr, string, error) {
	word, _ := expr.(*syntax.Word)
	if word == nil {
		return "", nil, "", errNotVariable
	}
	var pe *syntax.ParamExp
	if len(word.Parts) == 1 {
		pe, _ = word.Parts[0].(*syntax.ParamExp)
	}
	name := word.Lit()
	if pe != nil && pe.Index != nil {
		name = pe.Param.Value
	} else if !syntax.ValidName(name) {
		return "", nil, "", errNotVariable
	}
	vr := e.Env.Get(name)
	if n, v := vr.Resolve(e.Env); n != "" {
		name, vr = n, v
	}
	var idx syntax.ArithmExpr
	if pe != nil && pe.Index != nil {
		switch lit := nodeLit(pe.Index); lit {
		case "@", "*":
			return "", nil, "", fmt.Errorf("%s[%s]: bad array subscript", name, lit)
		}
		var key string
		if vr.Kind == Associative {
			var err error
			if key, err = e.assocKey(pe.Index); err != nil {
				return "", nil, "", internal.ArithmExpandError{Err: err}
			}
		} else {
			i, err := e.subscript(pe.Index)
			if err != nil {
				return "", nil, "", err
			}
			key = strconv.Itoa(i)
		}
		idx = &syntax.Word{Parts: []syntax.WordPart{&syntax.SglQuoted{Value: key}}}
	}
	str, _, err := e.varInd(vr, idx)
	return name, idx, str, err
}

func (e *expander) assgnArit(b *syntax.BinaryArithm) (int, error) {
	name, idx, oldStr, err := e.arithmVar(b.X)
	if err != nil {
		return 0, err
	}
	arg_, err := e.arithm(b.Y)
	if err != nil {
		return 0, err
	}
	arg := int64(arg_)
	var val int64
	if b.Op != syntax.Assgn {
		// A plain assignment does not need the old value.
		old, err := e.arithmValue(oldStr)
		if err != nil {
			return 0, err
		}
		val = int64(old)
	}
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
