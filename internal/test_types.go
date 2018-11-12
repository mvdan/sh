// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package internal

import (
	"bytes"
	"fmt"
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

func (c *ConcBuffer) Reset() {
	c.Lock()
	c.buf.Reset()
	c.Unlock()
}

// ReadString will keep reading from a reader until all bytes from the supplied
// string are read.
func ReadString(r io.Reader, want string) error {
	p := make([]byte, len(want))
	_, err := io.ReadFull(r, p)
	if err != nil {
		return err
	}
	got := string(p)
	if got != want {
		return fmt.Errorf("ReadString: read %q, wanted %q", got, want)
	}
	return nil
}
