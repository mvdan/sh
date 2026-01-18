package coreutils

import "fmt"

// Error wraps any error returned from the core utilities.
type Error struct {
	err error
}

var (
	_ error                       = &Error{}
	_ interface{ Unwrap() error } = &Error{}
)

func (err *Error) Error() string {
	return fmt.Sprintf("coreutils: %v", err.err)
}

func (err *Error) Unwrap() error {
	return err.err
}
