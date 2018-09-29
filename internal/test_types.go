// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package internal

import (
	"bytes"
	"io"
	"sync"
)

// ConcBuffer wraps a bytes.Buffer in a mutex so that concurrent writes
// to it don't upset the race detector.
type ConcBuffer struct {
	buf bytes.Buffer
	sync.Mutex
}

func (c *ConcBuffer) Write(p []byte) (int, error) {
	c.Lock()
	n, err := c.buf.Write(p)
	c.Unlock()
	return n, err
}

func (c *ConcBuffer) WriteString(s string) (int, error) {
	c.Lock()
	n, err := c.buf.WriteString(s)
	c.Unlock()
	return n, err
}

func (c *ConcBuffer) String() string {
	c.Lock()
	s := c.buf.String()
	c.Unlock()
	return s
}

// ChunkedReader is similar to strings.Reader, but one can control how many
// reads take place and when they unblock.
type ChunkedReader chan string

func (c ChunkedReader) Read(p []byte) (n int, err error) {
	s, ok := <-c
	if !ok { // closed channel
		return 0, io.EOF
	}
	if len(s) > len(p) {
		panic("TODO: small byte buffers")
	}
	return copy(p, []byte(s)), nil
}
