package interp

import (
	"context"
	"io"
	"log"
	"sync"
)

var _ io.Reader = (*CancelableReader)(nil)
var _ io.Reader = (*CancelableReaderTTY)(nil)
var _ Canceler = (*CancelableReader)(nil)
var _ Canceler = (*CancelableReaderTTY)(nil)
var _ fder = (*CancelableReaderTTY)(nil)

type CancelableReader struct {
	// Don't allow overlapping reads
	readMux sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc

	// run's input queue
	in chan *readReq

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
	buf []byte
	out chan readResult
}

type readResult struct {
	n   int
	err error
}

var (
	mux    sync.RWMutex
	allCRs = map[io.Reader]io.Reader{}
)

func NewCancelableReader(ctx context.Context, r io.Reader) io.Reader {
	// If there's already a CancelableReader for this r, return it.
	mux.RLock()
	c, ok := allCRs[r]
	mux.RUnlock()
	if ok {
		log.Printf("Reusing CancelableReader from %p", r)
		return c
	}
	log.Printf("Starting new CancelableReader for %p", r)

	ctx, cancel := context.WithCancel(ctx)
	c = &CancelableReader{
		R:      r,
		ctx:    ctx,
		cancel: cancel,
		in:     make(chan *readReq),
	}

	// Make sure [[ -t 0 ]] still works
	if _, ok := r.(fder); ok {
		c = &CancelableReaderTTY{
			CancelableReader: c.(*CancelableReader),
		}
	}

	mux.Lock()
	allCRs[r] = c
	mux.Unlock()

	// Clean up allCRs
	go func() {
		<-ctx.Done()
		mux.Lock()
		delete(allCRs, r)
		mux.Unlock()
	}()

	return c
}

// Read implements the io.Reader interface
func (c *CancelableReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}

	c.readMux.Lock()
	defer c.readMux.Unlock()

	id := c.readId
	c.readId++

	log.Printf("CancelableReader.Read: id %v with p %p of cap %d", id, p, cap(p))

	// Sometimes the user cancels the read before even calling it.
	if c.ctx.Err() != nil {
		log.Printf("rdCtx %p err is '%v'", c.ctx, c.ctx.Err())
		return 0, io.EOF
	}

	c.once.Do(func() { go c.run(id) })

	// Allocate a private buffer: if we start a read and then they cancel
	// it, we don't want to be writing on a buffer we don't own any more if the
	// Read does eventually read something.
	buf := make([]byte, len(p))

	req := &readReq{buf: buf, out: make(chan readResult)}

	// Send the request over to the reader goroutine
	select {
	case c.in <- req:
		log.Printf("CancelableReader.Read: (from %v) sent buffer p %p of len %d", id, p, len(buf))
	case <-c.ctx.Done():
		log.Printf("CancelableReader.Read: (from %v) ctx done 1 with p %p of len %d", id, p, len(buf))
		return 0, io.EOF
	}

	// Get the output from the reader goroutine.
	select {
	case res := <-req.out:
		// Hope they don't cancel before we return!  Or if they do, hope they
		// don't do anything with p, 'cause we're about to write on it.

		log.Printf("CancelableReader.Read: (from %v) got results %q, %d, %v, p %p",
			id, buf[:res.n], res.n, res.err, p)
		if res.n > 0 {
			copy(p, buf[:res.n])
		}
		if res.err != nil {
			log.Printf("CancelableReader.Read: (from %v) c.cancel(), err: %v", id, res.err)
			c.cancel()
		}
		return res.n, res.err

	case <-c.ctx.Done():
		log.Printf("CancelableReader.Read: (from %v) ctx done 2, p %p of len %d",
			id, p, len(p))
		return 0, io.EOF
	}
}

// Close implements the io.Closer interface.
func (c *CancelableReader) Close() error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	c.cancel()
	if closer, ok := c.R.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func (c *CancelableReader) run(rn int) {
	log.Printf("CancelableReader.run starting (from %v)", rn)
	defer log.Printf("CancelableReader.run exiting (from %v)", rn)

	// type typeahead struct {
	// 	data []byte // n is implicitly len(data)
	// 	err  error
	// }
	// var ta *typeahead

	for c.ctx.Err() == nil {
		id := c.runId
		c.runId++

		log.Printf("CancelableReader.run: (from %v) select", id)

		var n int
		var err error
		var req *readReq

		select {
		case req = <-c.in:
			// // If we have typeahead info left over from the previous read, return
			// // it.
			// if ta != nil {
			// 	log.Printf("CancelableReader.run: (from %v) using typeahead, len %d",
			// 		id, len(ta.data))
			// 	n = copy(req.buf, ta.data)
			// 	if n < len(ta.data) {
			// 		// We didn't return all the typeahead that we actually have.  err remains nil
			// 		ta.data = ta.data[n:]
			// 		log.Printf("CancelableReader.run: (from %v) %d bytes of typeahead remain",
			// 			id, len(ta.data))
			// 	} else {
			// 		log.Printf("CancelableReader.run: (from %v) clearing typeahead", id)
			// 		err = ta.err
			// 		ta = nil
			// 	}
			// 	break
			// }

			log.Printf("CancelableReader.run: (from %v) calling Read buf %p", id, req.buf)
			n, err = c.R.Read(req.buf)
			log.Printf("CancelableReader.run: (from %v) Read returned on buf %p: %d, %v, active: %t",
				id, req.buf[:n], n, err, c.ctx.Err() == nil)
		case <-c.ctx.Done():
			log.Printf("CancelableReader.run: (from %v) ctx done 3", id)
			return
		}

		select {
		case req.out <- readResult{n: n, err: err}:
			log.Printf("CancelableReader.run: (from %v) sent results in buf %p: %d, %v", id, req.buf[:n], n, err)
		case <-c.ctx.Done():
			// Cancelled before we could send the response.  Save it in the
			// typeahead buffer.
			// log.Printf("CancelableReader.run: (from %v) ctx done 4 (buf %p); storing typeahead", id, req.buf[:n])
			// buf2 := make([]byte, n)
			// copy(buf2, req.buf[:n])
			// ta = &typeahead{
			// 	data: buf2,
			// 	err:  err,
			// }
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
