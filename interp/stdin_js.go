// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

//go:build js

package interp

import (
	"io"
	"time"
)

// js/wasm has no OS pipes and no subprocesses, so pipes are in-process
// [io.Pipe]s and any reader can be used as stdin directly. Read
// deadlines are not supported: SetReadDeadline is a no-op, so a
// blocked `read` builtin unblocks on the next write rather than on
// context cancellation.

type jsPipeReader struct {
	*io.PipeReader
}

func (jsPipeReader) SetReadDeadline(t time.Time) error { return nil }

type jsPipeWriter struct {
	*io.PipeWriter
}

func (w jsPipeWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func newOSPipe() (stdinFile, pipeWriter, error) {
	pr, pw := io.Pipe()
	return jsPipeReader{pr}, jsPipeWriter{pw}, nil
}

// jsReader adapts a plain reader into a stdinFile.
type jsReader struct {
	io.Reader
}

func (r jsReader) Close() error {
	if c, ok := r.Reader.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

func (jsReader) SetReadDeadline(t time.Time) error { return nil }

// newStdinFile converts a reader into the runner's stdin. Without
// subprocesses there is no need to copy through a pipe.
func newStdinFile(r io.Reader) (stdinFile, error) {
	switch r := r.(type) {
	case stdinFile:
		return r, nil
	case nil:
		return nil, nil
	default:
		return jsReader{r}, nil
	}
}
