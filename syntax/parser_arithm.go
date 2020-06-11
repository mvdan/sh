package syntax

import ()

// compact specifies whether we allow spaces between expressions.
// This is true for let
func (p *Parser) arithmExpr(compact bool) ArithmExpr {
	return p.arithmExprComma(compact)
}

// These function names are inspired by Bash's expr.c

func (p *Parser) arithmExprComma(compact bool) ArithmExpr {
	value := p.arithmExprAssign(compact)
	for BinAritOperator(p.tok) == Comma {
		if compact && p.spaced {
			return value
		}
		pos := p.pos
		tok := p.tok
		p.nextArithOp(compact)
		y := p.arithmExprAssign(compact)
		if y == nil {
			p.followErrExp(pos, tok.String())
		}
		value = &BinaryArithm{
			OpPos: pos,
			Op:    BinAritOperator(tok),
			X:     value,
			Y:     y,
		}
	}
	return value
}

func (p *Parser) arithmExprAssign(compact bool) ArithmExpr {
	value := p.arithmExprCond(compact)
	switch BinAritOperator(p.tok) {
	case AddAssgn, SubAssgn, MulAssgn, QuoAssgn, RemAssgn, AndAssgn,
		OrAssgn, XorAssgn, ShlAssgn, ShrAssgn, Assgn:
		if !isArithName(value) {
			p.posErr(p.pos, "%s must follow a name", p.tok.String())
		}
		pos := p.pos
		tok := p.tok
		p.nextArithOp(compact)
		y := p.arithmExprAssign(compact)
		if y == nil {
			p.followErrExp(pos, tok.String())
		}
		return &BinaryArithm{
			OpPos: pos,
			Op:    BinAritOperator(tok),
			X:     value,
			Y:     y,
		}
	}
	return value
}

func (p *Parser) arithmExprCond(compact bool) ArithmExpr {
	value := p.arithmExprLor(compact)
	if BinAritOperator(p.tok) == TernQuest {
		if compact && p.spaced {
			return value
		}
		questPos := p.pos
		p.nextArithOp(compact)
		if BinAritOperator(p.tok) == TernColon {
			p.followErrExp(questPos, TernQuest.String())
		}
		trueExpr := p.arithmExpr(compact)
		if trueExpr == nil {
			p.followErrExp(questPos, TernQuest.String())
		}
		if BinAritOperator(p.tok) != TernColon {
			p.posErr(p.pos, "ternary operator missing : after ?")
		}
		colonPos := p.pos
		p.nextArithOp(compact)
		falseExpr := p.arithmExprCond(compact)
		if falseExpr == nil {
			p.followErrExp(colonPos, TernColon.String())
		}
		return &BinaryArithm{
			OpPos: questPos,
			Op:    BinAritOperator(TernQuest),
			X:     value,
			Y: &BinaryArithm{
				OpPos: colonPos,
				Op:    BinAritOperator(TernColon),
				X:     trueExpr,
				Y:     falseExpr,
			},
		}
	}

	return value
}

func (p *Parser) arithmExprLor(compact bool) ArithmExpr {
	value := p.arithmExprLand(compact)
	for BinAritOperator(p.tok) == OrArit {
		if compact && p.spaced {
			return value
		}
		op := p.tok
		pos := p.pos
		p.nextArithOp(compact)
		y := p.arithmExprLand(compact)
		if y == nil {
			p.followErrExp(pos, op.String())
		}
		value = &BinaryArithm{
			OpPos: pos,
			Op:    BinAritOperator(op),
			X:     value,
			Y:     y,
		}
	}
	return value
}

func (p *Parser) arithmExprLand(compact bool) ArithmExpr {
	value := p.arithmExprBor(compact)
	for BinAritOperator(p.tok) == AndArit {
		if compact && p.spaced {
			return value
		}
		op := p.tok
		pos := p.pos
		p.nextArithOp(compact)
		y := p.arithmExprBor(compact)
		if y == nil {
			p.followErrExp(pos, op.String())
		}
		value = &BinaryArithm{
			OpPos: pos,
			Op:    BinAritOperator(op),
			X:     value,
			Y:     y,
		}
	}
	return value
}

func (p *Parser) arithmExprBor(compact bool) ArithmExpr {
	value := p.arithmExprBxor(compact)
	for BinAritOperator(p.tok) == Or {
		if compact && p.spaced {
			return value
		}
		op := p.tok
		pos := p.pos
		p.nextArithOp(compact)
		y := p.arithmExprBxor(compact)
		if y == nil {
			p.followErrExp(pos, op.String())
		}
		value = &BinaryArithm{
			OpPos: pos,
			Op:    BinAritOperator(op),
			X:     value,
			Y:     y,
		}
	}
	return value
}

func (p *Parser) arithmExprBxor(compact bool) ArithmExpr {
	value := p.arithmExprBand(compact)
	for BinAritOperator(p.tok) == Xor {
		if compact && p.spaced {
			return value
		}
		op := p.tok
		pos := p.pos
		p.nextArithOp(compact)
		y := p.arithmExprBand(compact)
		if y == nil {
			p.followErrExp(pos, op.String())
		}
		value = &BinaryArithm{
			OpPos: pos,
			Op:    BinAritOperator(op),
			X:     value,
			Y:     y,
		}
	}
	return value
}

func (p *Parser) arithmExprBand(compact bool) ArithmExpr {
	value := p.arithmExpr5(compact)
	for BinAritOperator(p.tok) == And {
		if compact && p.spaced {
			return value
		}
		op := p.tok
		pos := p.pos
		p.nextArithOp(compact)
		y := p.arithmExpr5(compact)
		if y == nil {
			p.followErrExp(pos, op.String())
		}
		value = &BinaryArithm{
			OpPos: pos,
			Op:    BinAritOperator(op),
			X:     value,
			Y:     y,
		}
	}
	return value
}

func (p *Parser) arithmExpr5(compact bool) ArithmExpr {
	value := p.arithmExpr4(compact)
	for BinAritOperator(p.tok) == Eql || BinAritOperator(p.tok) == Neq {
		if compact && p.spaced {
			return value
		}
		op := p.tok
		pos := p.pos
		p.nextArithOp(compact)
		y := p.arithmExpr4(compact)
		if y == nil {
			p.followErrExp(pos, op.String())
		}
		value = &BinaryArithm{
			OpPos: pos,
			Op:    BinAritOperator(op),
			X:     value,
			Y:     y,
		}
	}
	return value
}

func (p *Parser) arithmExpr4(compact bool) ArithmExpr {
	value := p.arithmExprShift(compact)
	for BinAritOperator(p.tok) == Lss ||
		BinAritOperator(p.tok) == Gtr ||
		BinAritOperator(p.tok) == Leq ||
		BinAritOperator(p.tok) == Geq {
		if compact && p.spaced {
			return value
		}
		op := p.tok
		pos := p.pos
		p.nextArithOp(compact)
		y := p.arithmExprShift(compact)
		if y == nil {
			p.followErrExp(pos, op.String())
		}
		value = &BinaryArithm{
			OpPos: pos,
			Op:    BinAritOperator(op),
			X:     value,
			Y:     y,
		}
	}
	return value
}

func (p *Parser) arithmExprShift(compact bool) ArithmExpr {
	value := p.arithmExpr3(compact)
	for BinAritOperator(p.tok) == Shl ||
		BinAritOperator(p.tok) == Shr {
		if compact && p.spaced {
			return value
		}
		op := p.tok
		pos := p.pos
		p.nextArithOp(compact)
		y := p.arithmExpr3(compact)
		if y == nil {
			p.followErrExp(pos, op.String())
		}
		value = &BinaryArithm{
			OpPos: pos,
			Op:    BinAritOperator(op),
			X:     value,
			Y:     y,
		}
	}
	return value
}

func (p *Parser) arithmExpr3(compact bool) ArithmExpr {
	value := p.arithmExpr2(compact)
	for BinAritOperator(p.tok) == Add ||
		BinAritOperator(p.tok) == Sub {
		if compact && p.spaced {
			return value
		}
		op := p.tok
		pos := p.pos
		p.nextArithOp(compact)
		y := p.arithmExpr2(compact)
		if y == nil {
			p.followErrExp(pos, op.String())
		}
		value = &BinaryArithm{
			OpPos: pos,
			Op:    BinAritOperator(op),
			X:     value,
			Y:     y,
		}
	}
	return value
}

func (p *Parser) arithmExpr2(compact bool) ArithmExpr {
	value := p.arithmExprPower(compact)
	for BinAritOperator(p.tok) == Mul ||
		BinAritOperator(p.tok) == Quo ||
		BinAritOperator(p.tok) == Rem {
		if compact && p.spaced {
			return value
		}
		op := p.tok
		pos := p.pos
		p.nextArithOp(compact)
		y := p.arithmExprPower(compact)
		if y == nil {
			p.followErrExp(pos, op.String())
		}
		value = &BinaryArithm{
			OpPos: pos,
			Op:    BinAritOperator(op),
			X:     value,
			Y:     y,
		}
	}
	return value
}

func (p *Parser) arithmExprPower(compact bool) ArithmExpr {
	value := p.arithmExpr1(compact)
	if BinAritOperator(p.tok) == Pow {
		if compact && p.spaced {
			return value
		}
		op := p.tok
		pos := p.pos
		p.nextArithOp(compact)
		y := p.arithmExprPower(compact)
		if y == nil {
			p.followErrExp(pos, op.String())
		}
		return &BinaryArithm{
			OpPos: pos,
			Op:    BinAritOperator(op),
			X:     value,
			Y:     y,
		}
	}

	return value
}

func (p *Parser) arithmExpr1(compact bool) ArithmExpr {
	p.got(_Newl)

	switch UnAritOperator(p.tok) {
	case Not, BitNegation, Plus, Minus:
		ue := &UnaryArithm{OpPos: p.pos, Op: UnAritOperator(p.tok)}
		p.nextArithOp(compact)
		if ue.X = p.arithmExpr1(compact); ue.X == nil {
			p.followErrExp(ue.OpPos, ue.Op.String())
		}
		return ue
	}

	return p.arithmExpr0(compact)
}

func (p *Parser) arithmExpr0(compact bool) ArithmExpr {
	var x ArithmExpr
	switch p.tok {
	case addAdd, subSub:
		ue := &UnaryArithm{OpPos: p.pos, Op: UnAritOperator(p.tok)}
		p.nextArithOp(compact)
		if p.tok != _LitWord {
			p.followErr(ue.OpPos, token(ue.Op).String(), "a literal")
		}
		ue.X = p.arithmExpr(compact)
		return ue
	case leftParen:
		pe := &ParenArithm{Lparen: p.pos}
		p.nextArithOp(compact)
		pe.X = p.followArithm(leftParen, pe.Lparen)
		pe.Rparen = p.matched(pe.Lparen, leftParen, rightParen)
		for p.got(_Newl) {
		}
		x = pe
	case _LitWord:
		l := p.getLit()
		if p.tok != leftBrack {
			x = p.word(p.wps(l))
			break
		}
		pe := &ParamExp{Dollar: l.ValuePos, Short: true, Param: l}
		pe.Index = p.eitherIndex()
		x = p.word(p.wps(pe))
	case bckQuote:
		if p.quote == arithmExprLet && p.openBquotes > 0 {
			return nil
		}
		fallthrough
	default:
		if w := p.getWord(); w != nil {
			x = w
		} else {
			return nil
		}
	}
	if compact && p.spaced {
		return x
	}
	for p.got(_Newl) {
	}

	// we want real nil, not (*Word)(nil) as that
	// sets the type to non-nil and then x != nil
	if p.tok == addAdd || p.tok == subSub {
		if !isArithName(x) {
			p.curErr("%s must follow a name", p.tok.String())
		}
		u := &UnaryArithm{
			Post:  true,
			OpPos: p.pos,
			Op:    UnAritOperator(p.tok),
			X:     x,
		}
		p.nextArith(compact)
		return u
	}

	return x
}

// nextArith consumes a token.
// It returns true if compact and the token was followed by spaces
func (p *Parser) nextArith(compact bool) bool {
	p.next()
	if compact && p.spaced {
		return true
	}
	for p.got(_Newl) {
	}
	return false
}

func (p *Parser) nextArithOp(compact bool) {
	pos := p.pos
	tok := p.tok
	if p.nextArith(compact) {
		p.followErrExp(pos, tok.String())
	}
}

func isArithName(left ArithmExpr) bool {
	w, ok := left.(*Word)
	if !ok || len(w.Parts) != 1 {
		return false
	}
	switch x := w.Parts[0].(type) {
	case *Lit:
		return ValidName(x.Value)
	case *ParamExp:
		return x.nakedIndex()
	default:
		return false
	}
}

func (p *Parser) followArithm(ftok token, fpos Pos) ArithmExpr {
	x := p.arithmExpr(false)
	if x == nil {
		p.followErrExp(fpos, ftok.String())
	}
	return x
}
