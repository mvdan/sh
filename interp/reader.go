package interp

import (
	"context"
	"encoding/binary"
	"io"
	"log"
	"os"
)

const (
	Data = iota
	EOF
)

type EofWriter struct {
	// wr io.Reader
	pw *os.File
	pr *os.File
}

type EofReader struct {
	ctx    context.Context
	cancel context.CancelFunc

	// The wrapped reader
	wr *EofWriter
	// We read from wr and write on this, which is the "write" half of a pipe.
	pw *os.File

	// Caller reads from this, which is the "read" half of a pipe.
	R *os.File

	eof bool
	err error
}

func NewEofWriter() (*EofWriter, error) {
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	eofWriter := &EofWriter{
		// wr: r,
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
	n, err := e.pw.Write(buf[:])
	if err != nil {
		log.Printf("EOFWriter.Write: error writing msg: %v", err)
		return n, err
	}
	if n < len(buf) {
		log.Printf("EOFWriter.Write: error writing msg: wanted to write %d bytes, only wrote %d",
			len(buf), n)
	}
	log.Printf("EOFWriter.run: Wrote data %q, size %d", buf[3:n], n-3)
	return n, nil
}

func (e *EofWriter) EOF() error {
	_, err := e.pw.Write([]byte{EOF, 0, 0})
	log.Printf("EOFWriter.EOF: Wrote EOF, err: %v", err)
	return err
}

func (e *EofWriter) Read(p []byte) (int, error) {
	return e.pr.Read(p)
}

func (e *EofWriter) Close() error {
	err := e.pw.Close()
	err2 := e.pr.Close()
	if err != nil {
		return err
	}
	if err2 != nil {
		return err2
	}
	return nil
}

func NewEofReader(ctx context.Context, r *EofWriter) (*EofReader, error) {
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	// rdCtx, rdCancel := context.WithCancel(ctx)
	ctx, cancel := context.WithCancel(ctx)
	c := &EofReader{
		wr:     r,
		pw:     pw,
		R:      pr,
		ctx:    ctx,
		cancel: cancel,
	}

	log.Printf("Starting new EofReader: %p", c)

	go func() {
		<-ctx.Done()
		log.Printf("EofReader %p: context cancelled: closing pw & pr", c)
		c.Close()
	}()

	go c.run()

	return c, nil
}

func (c *EofReader) run() {
	defer log.Printf("EofReader.run %p: returning", c)
	var header [3]byte
	for c.err == nil {
		// Read the header
		log.Printf("EofReader.run %p: reading header", c)
		_, err := io.ReadFull(c.wr, header[:])
		// n, err := c.wr.Read(header[:])
		if err != nil {
			c.err = err
			c.Close()
			log.Printf("EofReader.run %p: error reading header: %v", c, err)
			return
		}
		// if n != 3 {
		// 	c.cancel()
		// 	log.Printf("EofReader.run %p: read %d bytes, expected 3", c, n)
		// 	return
		// }

		switch header[0] {
		case Data:
			// Read and forward the data block
			size := binary.LittleEndian.Uint16(header[1:])
			log.Printf("EofReader.run %p: read Data header, size: %d", c, size)
			buf := make([]byte, size)
			_, err = io.ReadFull(c.wr, buf)
			if err != nil {
				c.err = err
				c.Close()
				log.Printf("EofReader.run %p: error reading data: %v", c, err)
				return
			}
			log.Printf("EofReader.run %p: Writing %q, %d bytes to pw", c, buf, size)
			_, err = c.pw.Write(buf)
			if err != nil {
				// os.Stderr should probably be runner.stderr
				log.Printf("EofReader.run %p: Error on c.wp.Write: %v", c, err)
				c.err = err
				c.Close()
				return
			}

		case EOF:
			if !c.eof {
				// "send" an EOF by closing the pipe file handles
				c.Close()
				log.Printf("EofReader.run %p: \"sending\" eof by closing output pipe", c)
			}
			return
		}
	}
}

// Read implements io.Reader.
func (c *EofReader) Read(p []byte) (int, error) {
	return c.R.Read(p)
}

func (c *EofReader) Cancel() {
	log.Printf("EofReader %p: cancel", c)
	c.cancel()
}

// TODO: should protect eof with a lock
func (c *EofReader) Close() {
	if c.eof {
		log.Printf("EofReader %p: Close called after eof", c)
	} else {
		c.eof = true
		log.Printf("EofReader %p: Close", c)
		err := c.pw.Close()
		if err != nil {
			log.Printf("EofReader.Close %p: pw.Close error: %v", c, err)
		}
		err = c.R.Close()
		if err != nil {
			log.Printf("EofReader.Close %p: R.Close error: %v", c, err)
		}
		c.cancel()
	}
}
