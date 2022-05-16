package interp

import (
	"context"
	"encoding/binary"
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
	pw   *io.PipeWriter
	pr   *io.PipeReader
	once sync.Once
	err  error
}

type EofReader struct {
	cancel      context.CancelFunc
	closeCancel context.CancelFunc

	// We read from wr and write on pw.
	wr *EofWriter
	pw *os.File

	// Caller reads from this, which is the "read" half of a pipe.
	R *os.File

	mux sync.RWMutex
	eof bool
	err error
}

func NewEofWriter() (*EofWriter, error) {
	pr, pw := io.Pipe()
	eofWriter := &EofWriter{
		pw: pw,
		pr: pr,
	}

	return eofWriter, nil
}

func (e *EofWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	buf := make([]byte, 3+len(p))
	buf[0] = Data
	binary.LittleEndian.PutUint16(buf[1:], uint16(len(p)))
	copy(buf[3:], p)
	n, err := e.pw.Write(buf)
	if err != nil {
		log.Printf("EOFWriter.Write: error writing msg: %v", err)
		return n, err
	}
	// FIXME: Should I retry?  Esp. if there's no error?
	if n < len(buf) {
		log.Printf("EOFWriter.Write: error writing msg: wanted to write %d bytes, only wrote %d",
			len(buf), n)
	}
	// log.Printf("EOFWriter.run: Wrote data %q, size %d", buf[3:n], n-3)
	return n, nil
}

func (e *EofWriter) SendEof() error {
	_, err := e.pw.Write([]byte{EOF, 0, 0})
	// log.Printf("EOFWriter.EOF: Wrote EOF, err: %v", err)
	return err
}

func (e *EofWriter) Read(p []byte) (int, error) {
	return e.pr.Read(p)
}

func (e *EofWriter) Close() error {
	e.once.Do(func() {
		err := e.pw.Close()
		// err2 := e.pr.Close()
		if err != nil {
			e.err = err
			return
		}
		// e.err = err2
	})
	return e.err
}

func (e *EofWriter) CloseReader() error {
	err := e.pr.Close()
	if err != nil && e.err == nil {
		e.err = err
	}
	return err
}

func NewEofReader(ctx context.Context, r *EofWriter) (*EofReader, error) {
	// This cannot be io.Pipe, since we pass pr to os.Exec as stdin, and it
	// needs to be a *os.File.
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	closeCtx, closeCancel := context.WithCancel(context.Background())
	ctx, cancel := context.WithCancel(ctx)
	er := &EofReader{
		wr:          r,
		pw:          pw,
		R:           pr,
		cancel:      cancel,
		closeCancel: closeCancel,
	}

	// log.Printf("Starting new EofReader: %p", c)

	go func() {
		select {
		case <-ctx.Done():
			log.Printf("EofReader %p: context cancelled: closing pw & pr", er)
			er.Close()
		case <-closeCtx.Done():
		}
	}()

	go er.run()

	return er, nil
}

func (er *EofReader) run() {
	// defer log.Printf("EofReader.run %p: returning", c)
	var header [3]byte
	for er.err == nil {
		// Read the header
		// log.Printf("EofReader.run %p: reading header", c)
		_, err := io.ReadFull(er.wr, header[:])
		if err != nil {
			if err == io.EOF {
				// log.Printf("EofReader.run %p: EOF reading header; closing c.pw.pr", c)
				er.wr.pr.Close()
				return
			}
			er.err = err
			er.Close()
			log.Printf("EofReader.run %p: error reading header: %v", er, err)
			return
		}

		switch header[0] {
		case Data:
			// Read and forward the data block
			size := binary.LittleEndian.Uint16(header[1:])
			// log.Printf("EofReader.run %p: read Data header, size: %d", c, size)
			buf := make([]byte, size)
			_, err = io.ReadFull(er.wr, buf)
			if err != nil {
				er.err = err
				er.Close()
				log.Printf("EofReader.run %p: error reading data: %v", er, err)
				return
			}
			// log.Printf("EofReader.run %p: Writing %q, %d bytes to pw", c, buf, size)
			_, err = er.pw.Write(buf)
			if err != nil {
				log.Printf("EofReader.run %p: Error on c.pw.Write: %v", er, err)
				er.err = err
				er.Close()
				return
			}

		case EOF:
			if !er.Eof() {
				// "send" an EOF by closing the pipe file handles
				// log.Printf("EofReader.run %p: \"sending\" eof by closing output pipe", c)
				er.Close()
			}
			return
		}
	}
}

// Read implements io.Reader.
func (er *EofReader) Read(p []byte) (int, error) {
	return er.R.Read(p)
}

func (er *EofReader) Cancel() {
	// log.Printf("EofReader %p: cancel", c)
	er.cancel()
}

func (er *EofReader) Eof() bool {
	er.mux.RLock()
	defer er.mux.RUnlock()
	return er.eof
}

func (er *EofReader) Close() {
	er.mux.Lock()
	defer er.mux.Unlock()

	if er.eof {
		// log.Printf("EofReader %p: Close called after eof", c)
		return
	}

	er.eof = true
	// log.Printf("EofReader %p: Close", c)
	err := er.pw.Close()
	if err != nil {
		log.Printf("EofReader.Close %p: pw.Close error: %v", er, err)
	}
	err = er.R.Close()
	if err != nil {
		log.Printf("EofReader.Close %p: R.Close error: %v", er, err)
	}
	er.closeCancel()
}
