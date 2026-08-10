// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"io"
	"time"
)

// stdinFile abstracts the runner's standard input. On OSes it is
// backed by [*os.File] (the only type subprocesses can inherit, and
// the one supporting cancellable reads via SetReadDeadline); on
// js/wasm — where there are neither subprocesses nor OS pipes — it is
// backed by in-process pipes and plain readers.
type stdinFile interface {
	io.ReadCloser
	SetReadDeadline(t time.Time) error
}

// pipeWriter is the write end of a pipe created by newOSPipe.
type pipeWriter interface {
	io.WriteCloser
	WriteString(s string) (int, error)
}
