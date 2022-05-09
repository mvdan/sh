package interp

import (
	"context"
	"errors"
	"io"
	"log"
	"sync"
	"sync/atomic"
)

var _ io.Reader = (*CancelableReader)(nil)
var _ io.Reader = (*CancelableReaderTTY)(nil)
var _ Canceler = (*CancelableReader)(nil)
var _ Canceler = (*CancelableReaderTTY)(nil)
var _ fder = (*CancelableReaderTTY)(nil)

type CancelableReader struct {
	// Don't allow overlapping reads
	readMux sync.Mutex
	closed  int32

	ctx    context.Context
	cancel context.CancelFunc
	// rdCtx    context.Context
	// rdCancel context.CancelFunc

	// run's input queue
	in  chan *readReq
	out chan *readResult

	typeahead *readResult

	// The reader we're wrapping
	R io.Reader

	// Only start run() once.
	once sync.Once

	// Counters that help keep track of stuff during debugging.
	readId int
	runId  int
}

// If R has an Fd method, we should too.
type CancelableReaderTTY struct {
	*CancelableReader
}

type Canceler interface {
	Cancel()
}

type readReq struct {
	p []byte
}

type readResult struct {
	p   []byte
	err error
}

var (
	mux sync.RWMutex
)

func NewCancelableReader(ctx context.Context, r io.Reader) io.Reader {
	log.Printf("Starting new CancelableReader for %p", r)

	// rdCtx, rdCancel := context.WithCancel(ctx)
	ctx, cancel := context.WithCancel(ctx)
	c := io.Reader(&CancelableReader{
		R:      r,
		ctx:    ctx,
		cancel: cancel,
		// rdCtx:    rdCtx,
		// rdCancel: rdCancel,
		in:  make(chan *readReq),
		out: make(chan *readResult),
	})

	// Make sure [[ -t 0 ]] still works
	if _, ok := r.(fder); ok {
		c = &CancelableReaderTTY{
			CancelableReader: c.(*CancelableReader),
		}
	}

	return c
}

// Read implements the io.Reader interface
func (c *CancelableReader) Read(p []byte) (int, error) {
	if atomic.LoadInt32(&c.closed) == 1 {
		return 0, errors.New("Read on closed CancelableReader")
	}

	if err := c.ctx.Err(); err != nil {
		return 0, err
	}

	c.readMux.Lock()
	defer c.readMux.Unlock()

	// if c.typeahead != nil {
	// 	n := copy(p, c.typeahead.p)
	// 	err := c.typeahead.err
	// 	if n < len(c.typeahead.p) {
	// 		c.typeahead.p = c.typeahead.p[n:]
	// 		err = nil
	// 	} else {
	// 		c.typeahead = nil
	// 	}
	// 	return n, err
	// }

	// defer func() {
	// 	if c.rdCtx.Err() != nil {
	// 		c.rdCtx, c.rdCancel = context.WithCancel(c.ctx)
	// 	}
	// }()

	id := c.readId
	c.readId++

	log.Printf("CancelableReader.Read: id %v with p %p of cap %d", id, p, cap(p))

	c.once.Do(func() { go c.run(id) })

	// Allocate a private buffer: if we start a read and then they cancel
	// it, we don't want to be writing on a buffer we don't own any more if the
	// Read does eventually read something.
	buf := make([]byte, len(p))
	req := &readReq{p: buf}
	// c.rdCtx, c.rdCancel = context.WithCancel(c.ctx)

	// Send the request over to the reader goroutine.
	select {
	case c.in <- req:
		log.Printf("CancelableReader.Read: (from %v) sent buffer p %p of len %d", id, p, len(buf))
	case <-c.ctx.Done():
		log.Printf("CancelableReader.Read: (from %v) ctx done 1 with p %p of len %d", id, p, len(buf))
		return 0, io.EOF
		// default:
		// 	// If run is stuck in a read, it won't be listening on c.in, and we'll
		// 	// fall through to here.
		// 	log.Printf("CancelableReader.Read: (from %v) c.in is busy; waiting on previous read", id)
	}

	// Get the output from the reader goroutine.
	select {
	case res := <-c.out:
		log.Printf("CancelableReader.Read: (from %v) got results %q, %v, p %p",
			id, res.p, res.err, p)
		copy(p, res.p)
		return len(res.p), res.err

		// n := len(res.p)
		// err := res.err
		// if n > 0 {
		// 	// Might be copying res.p from a previous read and so it might differ
		// 	// in length from p
		// 	n = copy(p, res.p)
		// 	if n < len(res.p) {
		// 		c.typeahead = &readResult{
		// 			p:   res.p[n:],
		// 			err: res.err,
		// 		}
		// 		err = nil
		// 	}
		// }
		// if err != nil {
		// 	log.Printf("CancelableReader.Read: (from %v) err: %v", id, res.err)
		// }
		// return n, err

	case <-c.ctx.Done():
		log.Printf("CancelableReader.Read: (from %v) ctx done 2, p %p of len %d",
			id, p, len(p))
		return 0, io.EOF
	}
}

func (c *CancelableReader) run(rn int) {
	log.Printf("CancelableReader.run starting (from %v)", rn)
	defer log.Printf("CancelableReader.run exiting (from %v)", rn)

	// var typeahead *readResult

	for c.ctx.Err() == nil {
		id := c.runId
		c.runId++

		log.Printf("CancelableReader.run: (from %v) select", id)

		var n int
		var err error
		var req *readReq

		select {
		case req = <-c.in:
			log.Printf("CancelableReader.run: (from %v) calling Read buf %p", id, req.p)

			// if typeahead != nil {
			// 	n := copy(req.buf, typeahead.buf)
			// 	err := typeahead.err
			// 	if n < len(typeahead.buf) {
			// 		typeahead.buf = typeahead.buf[n:]
			// 		err = nil
			// 	} else {
			// 		typeahead = nil
			// 	}
			// 	c.out <- &readResult{buf: req.buf, err: err}
			// 	continue
			// }

			n, err = c.R.Read(req.p)
			req.p = req.p[:n]
			log.Printf("CancelableReader.run: (from %v) Read returned on buf %p: %d, %v, active: %t",
				id, req.p, n, err, c.ctx.Err() == nil)
		case <-c.ctx.Done():
			log.Printf("CancelableReader.run: (from %v) ctx done 3", id)
			return
		}

		select {
		case c.out <- &readResult{p: req.p, err: err}:
			log.Printf("CancelableReader.run: (from %v) sent results in buf %p: %d, %v", id, req.p, n, err)
		case <-c.ctx.Done():
		}
	}
}

func (c *CancelableReader) Cancel() {
	log.Printf("cancel: ctx is %p, cancel is %p", c.ctx, c.cancel)
	c.cancel()
}

func (ct *CancelableReaderTTY) Fd() uintptr {
	return ct.R.(fder).Fd()
}

// Close implements the io.Closer interface.
func (c *CancelableReader) Close() error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	if atomic.CompareAndSwapInt32(&c.closed, 0, 1) {
		c.cancel()
		if closer, ok := c.R.(io.Closer); ok {
			return closer.Close()
		}
	} else {
		return errors.New("Close on already-closed CancelableReader")
	}
	return nil
}
