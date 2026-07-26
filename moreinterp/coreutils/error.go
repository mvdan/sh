package coreutils

import (
	"fmt"

	"mvdan.cc/sh/v3/interp"
)

// Error wraps any error returned from the core utilities.
// It unwraps to an exit status of 1 so that a failing command results in
// a regular non-zero exit status rather than a fatal error.
type Error struct {
	err error
}

var (
	_ error                         = &Error{}
	_ interface{ Unwrap() []error } = &Error{}
)

func (err *Error) Error() string {
	return fmt.Sprintf("coreutils: %v", err.err)
}

func (err *Error) Unwrap() []error {
	return []error{err.err, interp.NewExitStatus(1)}
}
