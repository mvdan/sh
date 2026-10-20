// Copyright (c) 2026, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package internal

import (
	"errors"
	"fmt"
)

// FatalError is an expansion error which exits a non-interactive shell in
// Bash, such as an invalid indirect expansion or an arithmetic error, so
// that the interpreter can tell it apart from errors which only fail the
// command at hand.
type FatalError struct {
	Err error
}

func (e FatalError) Error() string { return e.Err.Error() }
func (e FatalError) Unwrap() error { return e.Err }

// ArithmExpandError wraps an error expanding a word within an arithmetic
// expression, such as ${x:?msg} in ((y = ${x:?msg})), so that the
// interpreter can handle it like any other expansion error, whereas an
// error evaluating the expression only fails the arithmetic command.
type ArithmExpandError struct {
	Err error
}

func (e ArithmExpandError) Error() string { return e.Err.Error() }
func (e ArithmExpandError) Unwrap() error { return e.Err }

// Fatal wraps a non-nil error as a [FatalError], unless it already is one.
func Fatal(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[FatalError](err); ok {
		return err
	}
	return FatalError{Err: err}
}

// MaxExpandSize and MaxExpandFields limit the size of any string and the
// number of fields that an expansion may produce, as well as the values
// of variables, so that a script cannot grow a string or an array
// exponentially until memory runs out.
// MaxPatternSize limits the patterns and regular expressions which may be
// compiled, as that takes hundreds of bytes per input byte.
const (
	MaxExpandSize   = 16 << 20
	MaxExpandFields = 1 << 20
	MaxPatternSize  = 64 << 10
)

// Fatalf formats a [FatalError].
func Fatalf(format string, args ...any) error {
	return FatalError{Err: fmt.Errorf(format, args...)}
}

// ErrPatternTooLarge is returned when a pattern exceeds [MaxPatternSize].
var ErrPatternTooLarge = Fatalf("pattern would exceed %d bytes", MaxPatternSize)
