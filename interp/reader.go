package interp

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
)

const (
	Data = iota
	EOF
)

type EofWriter struct {
	mux sync.RWMutex
	// wg  sync.WaitGroup

	wpw *io.PipeWriter // EofWriter.Write writes on this
	wpr *io.PipeReader // EofWriter.run reads from this

	ctx    context.Context
	cancel context.CancelFunc

	rpw *os.File // EofWriter.run writes on this
	// Caller reads from this, which is the "read" half of a pipe.
	R *os.File
}

func NewEofWriter() (*EofWriter, error) {
	ctx, cancel := context.WithCancel(context.Background())
	pr, pw := io.Pipe()
	eofWriter := &EofWriter{
		wpw:    pw,
		wpr:    pr,
		ctx:    ctx,
		cancel: cancel,
	}

	return eofWriter, nil
}

func (w *EofWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	w.mux.Lock()
	defer w.mux.Unlock()

	buf := make([]byte, 3+len(p))
	buf[0] = Data
	binary.LittleEndian.PutUint16(buf[1:], uint16(len(p)))
	copy(buf[3:], p)
	n, err := w.wpw.Write(buf)
	if err != nil {
		log.Printf("EOFWriter.Write: error writing msg: %v", err)
		return n, err
	}
	// FIXME: Should I retry?  Esp. if there's no error?
	if n < len(buf) {
		panic(fmt.Sprintf("EOFWriter.Write: error writing msg: wanted to write %d bytes, only wrote %d",
			len(buf), n))
	}
	// log.Printf("EOFWriter.run: Wrote data %q, size %d", buf[3:n], n-3)
	return n - 3, nil
}

func (w *EofWriter) Read(p []byte) (int, error) {
	// return e.RR.Read(p)
	panic("EofWriter.Read should never be called")
}

func (w *EofWriter) Close() error {
	log.Printf("EofWriter.Close %p: R: %p", w, w.R)
	return w.wpw.Close()
}

func (w *EofWriter) NewReader(ctx context.Context) (io.ReadCloser, error) {
	w.mux.Lock()
	defer w.mux.Unlock()

	if w.rpw != nil {
		panic("EofWriter.NewReader called with active reader")
	}

	// This cannot be io.Pipe, since we pass pr to os.Exec as stdin, and it
	// needs to be a *os.File.
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	w.cancel()
	// w.wg.Wait()

	w.rpw = pw
	w.R = pr
	ctx, cancel := context.WithCancel(ctx)
	w.ctx, w.cancel = ctx, cancel

	log.Printf("EofWriter.NewReader %p: R: %p", w, pr)

	// log.Printf("Starting new EofWriter: %p", c)

	// w.wg.Add(2)
	go func() {
		// defer w.wg.Done()

		<-ctx.Done()
		log.Printf("EofWriter.run %p: context cancelled, R: %p", w, pr)
		w.CloseReader()
	}()

	go w.run()

	return w.R, nil
}

func (w *EofWriter) run() {
	// defer w.wg.Done()

	// defer log.Printf("EofWriter.run %p: returning", c)

	defer func() {
		w.mux.Lock()
		defer w.mux.Unlock()
		log.Printf("EofWriter.run %p: R: %p, returning", w, w.R)

		w.rpw.Close()
		w.rpw = nil
		w.cancel()
	}()

	header := make([]byte, 3)
	for {
		// Read the header
		log.Printf("EofWriter.run %p: reading header", w)
		_, err := io.ReadFull(w.wpr, header)
		if err != nil {
			if err == io.EOF {
				log.Printf("EofWriter.run %p: EOF reading header", w)
				w.wpr.Close()
				return
			}
			log.Printf("EofWriter.run %p: error reading header: %v", w, err)
			return
		}

		switch header[0] {
		case Data:
			// Read and forward the data block
			size := binary.LittleEndian.Uint16(header[1:])
			// log.Printf("EofWriter.run %p: read Data header, size: %d", c, size)
			buf := make([]byte, size)
			_, err = io.ReadFull(w.wpr, buf)
			if err != nil {
				log.Printf("EofWriter.run %p: error reading data: %v", w, err)
				return
			}
			// log.Printf("EofWriter.run %p: Writing %q, %d bytes to pw", c, buf, size)
			_, err = w.rpw.Write(buf)
			if err != nil {
				log.Printf("EofWriter.run %p: Error on c.pw.Write: %v", w, err)
				return
			}

		case EOF:
			// defered function does the work
			log.Printf("EofWriter.run %p: Read an EOF cmd", w)
			return

		default:
			panic(fmt.Sprintf("EofWriter.run %p: Expected Data or EOF, got %d", w, header[0]))
		}
	}
}

func (w *EofWriter) CloseReader() error {
	w.mux.Lock()
	defer w.mux.Unlock()

	log.Printf("EofWriter.CloseReader %p: r: %p", w, w.R)
	if w.rpw != nil {
		log.Printf("EofWriter.CloseReader %p: r: %p, SendEof()", w, w.R)
		w.SendEof()
	}
	return w.R.Close()
}

func (w *EofWriter) SendEof() error {
	log.Printf("EofWriter.SendEof: %p: r: %p", w, w.R)
	_, err := w.wpw.Write([]byte{EOF, 0, 0})
	// log.Printf("EOFWriter.EOF: Wrote EOF, err: %v", err)
	return err
}
