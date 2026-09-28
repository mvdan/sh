// Copyright (c) 2026, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

// Package arithm holds helpers for arithmetic expressions shared by the
// expand and interp packages. It is separate from the internal package,
// which cannot import syntax as the syntax tests import it.
package arithm

import "mvdan.cc/sh/v3/syntax"

// Word rebuilds the word which an arithmetic expression was parsed from.
// Bash expands an associative array subscript as a word, but an unquoted
// subscript like foo-bar or $x-1 parses as an arithmetic expression.
//
// Whitespace is not kept, so m[x - y] is treated like m[x-y],
// whereas Bash treats them as different elements.
func Word(expr syntax.ArithmExpr) *syntax.Word {
	if word, ok := expr.(*syntax.Word); ok {
		return word
	}
	return &syntax.Word{Parts: appendParts(nil, expr)}
}

// appendParts appends the word parts for an expression. Operators are
// quoted so that they are not expanded, such as a leading ~ in a key like ~x.
func appendParts(parts []syntax.WordPart, expr syntax.ArithmExpr) []syntax.WordPart {
	switch expr := expr.(type) {
	case *syntax.Word:
		parts = append(parts, expr.Parts...)
	case *syntax.BinaryArithm:
		parts = append(appendParts(parts, expr.X), &syntax.SglQuoted{Value: expr.Op.String()})
		parts = appendParts(parts, expr.Y)
	case *syntax.UnaryArithm:
		op := &syntax.SglQuoted{Value: expr.Op.String()}
		if expr.Post {
			parts = append(appendParts(parts, expr.X), op)
		} else {
			parts = appendParts(append(parts, op), expr.X)
		}
	case *syntax.ParenArithm:
		parts = append(parts, &syntax.SglQuoted{Value: "("})
		parts = append(appendParts(parts, expr.X), &syntax.SglQuoted{Value: ")"})
	}
	return parts
}
