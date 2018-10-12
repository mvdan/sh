// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package internal

import (
	"bytes"
	"fmt"
	"io"
	"strings"
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

func (c *ConcBuffer) Reset() {
	c.Lock()
	c.buf.Reset()
	c.Unlock()
}

// TODO: just wrap over io.Pipe? is ours simpler, or just buggier?

// ChanPipe is a very simple pipe that uses a single channel to move bytes
// around.
type ChanPipe chan []byte

func (c ChanPipe) Read(p []byte) (n int, err error) {
	bs, ok := <-c
	if !ok { // closed channel
		return 0, io.EOF
	}
	if len(bs) > len(p) {
		panic("TODO: small byte buffers")
	}
	return copy(p, bs), nil
}

// ReadString is an utility function that will keep reading from the pipe until
// the bytes from the supplied string are read.
func (c ChanPipe) ReadString(s string) error {
	for len(s) > 0 {
		bs, ok := <-c
		if !ok { // closed channel
			return fmt.Errorf("ReadString: reached EOF")
		}
		read := string(bs)
		// TODO: support writes with extra bytes?
		if !strings.HasPrefix(s, read) {
			return fmt.Errorf("ReadString: read %q, wanted %q", read, s)
		}
		s = s[len(read):]
	}
	return nil
}

func (c ChanPipe) Write(p []byte) (n int, err error) {
	c <- p
	return len(p), nil
}

func (c ChanPipe) WriteString(s string) (n int, err error) {
	return c.Write([]byte(s))
}
