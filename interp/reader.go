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

	// Context for the whole reader.  Cancelling this cancels the whole reader.
	ctx    context.Context
	cancel context.CancelFunc

	// Protect access to rdCtx/rdCancel
	ctxMux sync.Mutex

	// Context for a single Read.  Cancelling this cancels a single Read.
	rdCtx    context.Context
	rdCancel context.CancelFunc

	// run's input queue
	in chan *readReq

	// The reader we're wrapping
	r io.Reader

	// Only start run() once.
	once sync.Once

	// Counters that help keep track of stuff during debugging.
	readId int
	runId  int
}

// If r has an Fd method, we should too.
type CancelableReaderTTY struct {
	*CancelableReader
}

type Canceler interface {
	Cancel()
}

type readReq struct {
	ctx context.Context
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
	rdCtx, rdCancel := context.WithCancel(ctx)

	log.Printf("Setting rdCtx to %p", rdCtx)
	c = &CancelableReader{
		r:        r,
		ctx:      ctx,
		cancel:   cancel,
		rdCtx:    rdCtx,
		rdCancel: rdCancel,
		in:       make(chan *readReq),
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

	c.ctxMux.Lock()

	// Sometimes the user cancels the read before even calling it.
	if c.rdCtx.Err() != nil {
		log.Printf("rdCtx %p err is '%v'", c.rdCtx, c.rdCtx.Err())
		c.rdCtx, c.rdCancel = context.WithCancel(c.ctx)
		log.Printf("CancelableReader.Read: id %v: rdCtx is already cancelled; setting it to %p",
			id, c.rdCtx)
		c.ctxMux.Unlock()
		return 0, io.EOF
	}

	c.rdCancel()
	rdCtx, rdCancel := context.WithCancel(c.ctx)
	c.rdCtx, c.rdCancel = rdCtx, rdCancel
	log.Printf("starting read: set rdCtx to %p", c.rdCtx)

	c.ctxMux.Unlock()

	defer func() {
		c.ctxMux.Lock()

		// Clean up rdCtx, in case we haven't yet.
		rdCancel()
		// Allow cancelling the next Read before it starts.
		c.rdCtx, c.rdCancel = context.WithCancel(c.ctx)

		log.Printf("ending read: set rdCtx to %p", c.rdCtx)
		c.ctxMux.Unlock()
	}()

	c.once.Do(func() { go c.run(id) })

	// Allocate a private buffer: if we start a read and then they cancel
	// it, we don't want to be writing on a buffer we don't own any more.
	buf := make([]byte, len(p))

	req := &readReq{ctx: rdCtx, buf: buf, out: make(chan readResult)}

	// Send the request over to the reader goroutine
	select {
	case c.in <- req:
		log.Printf("CancelableReader.Read: (from %v) sent buffer p %p of len %d", id, p, len(buf))

	case <-rdCtx.Done():
		log.Printf("CancelableReader.Read: (from %v) ctx done 1 with p %p of len %d", id, p, len(buf))
		// If the outer context has not been cancelled, just return EOF.  If we
		// don't return *some* error, they'll just call Read again.
		if c.ctx.Err() == nil {
			return 0, io.EOF
		}
		return 0, rdCtx.Err()
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
			log.Printf("CancelableReader.Read: (from %v) c.cancel()", id)
			c.cancel()
		}
		return res.n, res.err

	case <-rdCtx.Done():
		log.Printf("CancelableReader.Read: (from %v) ctx done 2, p %p of len %d",
			id, p, len(p))
		if c.ctx.Err() == nil {
			return 0, io.EOF
		}
		return 0, rdCtx.Err()
	}
}

// Close implements the io.Closer interface.
func (c *CancelableReader) Close() error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	c.cancel()
	if closer, ok := c.r.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func (c *CancelableReader) run(rn int) {
	log.Printf("CancelableReader.run starting (from %v)", rn)
	defer log.Printf("CancelableReader.run exiting (from %v)", rn)

	type typeahead struct {
		data []byte // n is implicitly len(data)
		err  error
	}
	var ta *typeahead

	for c.ctx.Err() == nil {
		id := c.runId
		c.runId++

		log.Printf("CancelableReader.run: (from %v) select", id)

		var n int
		var err error
		var req *readReq

		select {
		case req = <-c.in:
			// If we have typeahead info left over from the previous read, return
			// it.
			if ta != nil {
				log.Printf("CancelableReader.run: (from %v) using typeahead, len %d",
					id, len(ta.data))
				n = copy(req.buf, ta.data)
				if n < len(ta.data) {
					// We didn't return all the typeahead that we actually have.  err remains nil
					ta.data = ta.data[n:]
					log.Printf("CancelableReader.run: (from %v) %d bytes of typeahead remain",
						id, len(ta.data))
				} else {
					log.Printf("CancelableReader.run: (from %v) clearing typeahead", id)
					err = ta.err
					ta = nil
				}
				break
			}

			log.Printf("CancelableReader.run: (from %v) calling Read buf %p", id, req.buf)
			n, err = c.r.Read(req.buf)
			log.Printf("CancelableReader.run: (from %v) Read returned on buf %p: %d, %v, active: %t",
				id, req.buf[:n], n, err, req.ctx.Err() == nil)
		case <-c.ctx.Done():
			log.Printf("CancelableReader.run: (from %v) ctx done 3", id)
			return
		}

		select {
		case req.out <- readResult{n: n, err: err}:
			log.Printf("CancelableReader.run: (from %v) sent results in buf %p: %d, %v", id, req.buf[:n], n, err)
		case <-req.ctx.Done():
			// Cancelled before we could send the response.  Save it in the
			// typeahead buffer.
			log.Printf("CancelableReader.run: (from %v) ctx done 4 (buf %p); storing typeahead", id, req.buf[:n])
			buf2 := make([]byte, n)
			copy(buf2, req.buf[:n])
			ta = &typeahead{
				data: buf2,
				err:  err,
			}
		}
	}
}

func (c *CancelableReader) Cancel() {
	c.ctxMux.Lock()
	log.Printf("cancel: ctx is %p, cancel is %p", c.rdCtx, c.rdCancel)
	c.rdCancel()
	c.ctxMux.Unlock()
}

func (ct *CancelableReaderTTY) Fd() uintptr {
	return ct.r.(fder).Fd()
}
