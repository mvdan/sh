// Copyright (c) 2025, Andrey Nering <andrey@nering.com.br>
// See LICENSE for licensing information

package coreutils

import (
	"fmt"

	"mvdan.cc/sh/v3/interp"
)

// Error wraps an error returned from a core utility, carrying its exit status.
type Error struct {
	err    error
	status int
}

var (
	_ error                         = &Error{}
	_ interface{ Unwrap() []error } = &Error{}
)

func utilityError(err error, status int) error {
	return &Error{err: err, status: status}
}

func (err *Error) Error() string {
	return fmt.Sprintf("coreutils: %v", err.err)
}

func (err *Error) Unwrap() []error {
	return []error{err.err, interp.NewExitStatus(uint8(err.status))}
}
