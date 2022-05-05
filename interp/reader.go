package interp

import (
	"context"
	"io"
	"sync"
)

var _ io.Reader = (*CancelableReader)(nil)
var _ io.Reader = (*CancelableReaderTTY)(nil)
var _ Canceler = (*CancelableReader)(nil)
var _ Canceler = (*CancelableReaderTTY)(nil)
var _ fder = (*CancelableReaderTTY)(nil)

type CancelableReader struct {
	ctx    context.Context
	cancel context.CancelFunc
	in     chan []byte
	out    chan readResult
	err    error
	r      io.Reader
	once   sync.Once
}

type CancelableReaderTTY struct {
	CancelableReader
}

type Canceler interface {
	Cancel()
}

type readResult struct {
	n   int
	err error
}

func NewCancelableReader(ctx context.Context, r io.Reader) io.Reader {
	ctx, cancel := context.WithCancel(ctx)
	c := CancelableReader{
		r:      r,
		ctx:    ctx,
		cancel: cancel,
		in:     make(chan []byte),
		out:    make(chan readResult),
	}
	// Make sure [[ -t 0 ]] still works
	if _, ok := r.(fder); ok {
		return &CancelableReaderTTY{
			CancelableReader: c,
		}
	}
	return &c
}

// Read implements the io.Reader interface
func (c *CancelableReader) Read(p []byte) (int, error) {
	c.once.Do(func() { go c.begin() })

	// Send the buffer over to the reader goroutine
	select {
	case c.in <- p:
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	}

	// Get the output from the reader goroutine.
	select {
	case res, ok := <-c.out:
		if !ok {
			return 0, c.err
		}
		return res.n, res.err
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	}
}

// Close implements the io.Closer interface.
func (c *CancelableReader) Close() error {
	close(c.in)
	if closer, ok := c.r.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func (c *CancelableReader) begin() {
	for c.ctx.Err() == nil {
		select {
		case buf, ok := <-c.in:
			if !ok {
				return
			}
			n, err := c.r.Read(buf)
			select {
			case c.out <- readResult{n: n, err: err}:
			case <-c.ctx.Done():
				return
			}
		case <-c.ctx.Done():
		}
	}
}

func (c *CancelableReader) Cancel() {
	c.cancel()
}

func (ct *CancelableReaderTTY) Fd() uintptr {
	return ct.r.(fder).Fd()
}
