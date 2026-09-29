// Copyright (c) 2026, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package internal

// FatalError is an expansion error which exits a non-interactive shell in
// Bash, such as an invalid indirect expansion or an arithmetic error, so
// that the interpreter can tell it apart from errors which only fail the
// command at hand.
type FatalError struct {
	Err error
}

func (e FatalError) Error() string { return e.Err.Error() }
func (e FatalError) Unwrap() error { return e.Err }
