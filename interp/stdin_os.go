// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

//go:build !js

package interp

import (
	"io"
	"os"
)

// newOSPipe returns an OS pipe; both ends are [*os.File] so they can
// be inherited by subprocesses.
func newOSPipe() (stdinFile, pipeWriter, error) {
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	return pr, pw, nil
}

// newStdinFile converts a reader into the runner's stdin. Readers
// other than [*os.File] require an [os.Pipe] and a copying goroutine,
// as an [os.File] is the only way to share a reader with subprocesses.
func newStdinFile(r io.Reader) (stdinFile, error) {
	switch r := r.(type) {
	case *os.File:
		return r, nil
	case nil:
		return nil, nil
	default:
		pr, pw, err := os.Pipe()
		if err != nil {
			return nil, err
		}
		go func() {
			io.Copy(pw, r)
			pw.Close()
		}()
		return pr, nil
	}
}
