package interp

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"sync"

	"golang.org/x/sys/unix"
)

const (
	Data = iota
	EOF
)

type EofWriter struct {
	mux       sync.RWMutex
	topCtx    context.Context
	rdrCtx    context.Context
	rdrCancel context.CancelFunc
	eof       bool

	// Caller writes on this.  nil == eof
	getReader chan struct{}
	input     chan *[]byte
	once      sync.Once
	err       error

	// EofWriter.run writes on this
	rpw *os.File
	// Caller reads from this, which is the "read" half of a pipe.
	R *os.File
}

func NewEofWriter(topCtx context.Context) (*EofWriter, error) {
	// This cannot be io.Pipe, since we pass pr to os.Exec as stdin, and it
	// needs to be a *os.File.
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(topCtx)
	w := &EofWriter{
		getReader: make(chan struct{}),
		// 4000 character typeahead buffer, 1 key at a time.  Or of course much
		// more if they write / we read multiple bytes at a time.
		input:     make(chan *[]byte, 4000),
		topCtx:    topCtx,
		rdrCtx:    ctx,
		rdrCancel: cancel,
		rpw:       pw,
		R:         pr,
	}

	rdFdSet := &unix.FdSet{}
	fd := int(w.R.Fd())
	rdFdSet.Set(fd)
	n, err := unix.Select(fd+1, rdFdSet, nil, nil, &unix.Timeval{})
	log.Printf("EofWriter.NewEofWriter %p: Select returned %d, %v", w, n, err)

	// log.Printf("Starting new EofWriter: %p", c)

	go func() {
		<-w.topCtx.Done()
		w.mux.Lock()
		log.Printf("EofWriter.run %p: topCtx cancelled, R: %p", w, w.R)
		w.Close()
		w.mux.Unlock()
	}()

	go w.run()

	return w, nil
}

func (w *EofWriter) GetReader() (r io.Reader, _ error) {
	w.mux.Lock() // not sure I need these
	defer w.mux.Unlock()

	if w.err != nil {
		return nil, w.err
	}

	if !w.eof {
		log.Printf("EofWriter.NewReader %p: Returning current reader", w)
		return w.R, nil
	}

	rdFdSet := &unix.FdSet{}
	fd := int(w.R.Fd())
	rdFdSet.Set(fd)
	n, err := unix.Select(fd+1, rdFdSet, nil, nil, &unix.Timeval{})
	log.Printf("EofWriter.NewReader %p: Select returned %d, %v", w, n, err)
	if err != nil {
		return nil, err
	}
	ready := rdFdSet.IsSet(int(fd))
	log.Printf("EofWriter.NewReader %p: ready: %t", w, ready)
	// if n > 0 {
	if ready {
		log.Printf("EofWriter.NewReader %p: Reader is ready for reading: Returning current reader", w)
		buf := make([]byte, 1024)
		n, err := w.R.Read(buf)
		log.Printf("EofWriter.NewReader %p: Read returned %q, %d, %v", w, buf[:n], n, err)
		if err == io.EOF {
			rdFdSet := &unix.FdSet{}
			fd := int(w.R.Fd())
			rdFdSet.Set(fd)
			n, err := unix.Select(fd+1, rdFdSet, nil, nil, &unix.Timeval{})
			log.Printf("EofWriter.NewReader %p: 2nd Select returned %d, %v", w, n, err)
		}
		return w.R, nil
	}

	// open a new pipe
	pr, pw, err := os.Pipe()
	if err != nil {
		w.err = err
		return nil, err
	}

	log.Printf("EofWriter.NewReader %p: Opening and returning a new pipe", w)
	w.eof = false
	w.R, w.rpw = pr, pw
	w.rdrCtx, w.rdrCancel = context.WithCancel(w.topCtx)

	w.getReader <- struct{}{}

	return w.R, nil
}

func (w *EofWriter) run() {
	// defer log.Printf("EofWriter.run %p: returning", c)

	defer func() {
		log.Printf("EofWriter.run %p: R: %p, returning", w, w.R)
		w.rdrCancel()
		w.Close()
	}()

	for {
		log.Printf("EofWriter.run %p: R: %p, waiting for input", w, w.R)
		select {
		case input, ok := <-w.input:
			if input == nil || !ok {
				// Close the pipe in both cases.
				log.Printf("EofWriter.run %p: R: %p, eof or close", w, w.R)
				w.rpw.Close()
				runtime.SetFinalizer(w.R, func(r *os.File) {
					log.Printf("EofWriter.run %p: R: %p, w.R finalizer running", w, r)
					r.Close()
				})
				w.eof = true

				if !ok {
					return
				}

				// EOF
				log.Printf("EofWriter.run %p: R: %p, waiting for getReader", w, w.R)
				<-w.getReader
				log.Printf("EofWriter.run %p: R: %p, done waiting for getReader", w, w.R)
				continue
			}

			_, err := w.rpw.Write(*input)
			if err != nil {
				w.err = err
				return
			}
		case <-w.rdrCtx.Done():
			return
		}
	}

}

func (w *EofWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	w.mux.Lock()
	defer w.mux.Unlock()

	if w.err != nil {
		return 0, w.err
	}

	n := len(p)
	buf := make([]byte, n)
	copy(buf, p)
	select {
	case w.input <- &buf:
	default:
		return 0, fmt.Errorf("EofWriter: Input buffer full")
	}

	return n, nil
}

func (w *EofWriter) SendEof() error {
	w.mux.Lock()
	defer w.mux.Unlock()

	if w.err != nil {
		return w.err
	}

	select {
	case w.input <- nil:
	default:
		return fmt.Errorf("EofWriter Input buffer full")
	}
	return nil
}

func (w *EofWriter) Read(p []byte) (int, error) {
	panic("Nothing should call this")
	// w.mux.Lock()
	// defer w.mux.Unlock()
	// if err := w.err; err != nil {
	// 	return 0, err
	// }
	// return w.R.Read(p)
}

func (w *EofWriter) Close() error {
	w.mux.Lock()
	defer w.mux.Unlock()

	log.Printf("EofWriter.Close %p: R: %p", w, w.R)
	w.once.Do(func() { close(w.input) })
	w.rdrCancel()
	return w.err
}

// func (w *EofWriter) CancelReader() {
// 	w.mux.Lock()
// 	defer w.mux.Unlock()
//
// 	if !w.eof {
// 		w.input <- nil
// 	}
// }
