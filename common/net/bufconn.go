package net

import (
	"bufio"
	"io"
	"net"
	"sync"

	"github.com/Miku0139oao/aster-core/common/buf"
)

var _ ExtendedConn = (*BufferedConn)(nil)

const bufioReaderSize = 4096

var bufioReaderPool = sync.Pool{
	New: func() any {
		return bufio.NewReaderSize(nil, bufioReaderSize)
	},
}

func getBufioReader(r io.Reader) *bufio.Reader {
	br := bufioReaderPool.Get().(*bufio.Reader)
	br.Reset(r)
	return br
}

func putBufioReader(br *bufio.Reader) {
	if br == nil || br.Size() > bufioReaderSize {
		return
	}
	br.Reset(nil)
	bufioReaderPool.Put(br)
}

type BufferedConn struct {
	r *bufio.Reader
	ExtendedConn
	peeked bool
	owned  bool
}

func NewBufferedConn(c net.Conn) *BufferedConn {
	if bc, ok := c.(*BufferedConn); ok {
		return bc
	}
	return &BufferedConn{r: getBufioReader(c), ExtendedConn: NewExtendedConn(c), owned: true}
}

func WarpConnWithBioReader(c net.Conn, br *bufio.Reader) net.Conn {
	if br != nil && br.Buffered() > 0 {
		if bc, ok := c.(*BufferedConn); ok && bc.r == br {
			return bc
		}
		return &BufferedConn{r: br, ExtendedConn: NewExtendedConn(c), peeked: true}
	}
	return c
}

// Reader returns the internal bufio.Reader.
func (c *BufferedConn) Reader() *bufio.Reader {
	return c.r
}

func (c *BufferedConn) ResetPeeked() {
	c.peeked = false
}

func (c *BufferedConn) Peeked() bool {
	return c.peeked
}

// Peek returns the next n bytes without advancing the reader.
func (c *BufferedConn) Peek(n int) ([]byte, error) {
	c.peeked = true
	return c.r.Peek(n)
}

func (c *BufferedConn) Discard(n int) (discarded int, err error) {
	return c.r.Discard(n)
}

func (c *BufferedConn) Read(p []byte) (int, error) {
	return c.r.Read(p)
}

func (c *BufferedConn) ReadByte() (byte, error) {
	return c.r.ReadByte()
}

func (c *BufferedConn) UnreadByte() error {
	return c.r.UnreadByte()
}

func (c *BufferedConn) Buffered() int {
	return c.r.Buffered()
}

func (c *BufferedConn) ReadBuffer(buffer *buf.Buffer) (err error) {
	if c.r != nil && c.r.Buffered() > 0 {
		_, err = buffer.ReadOnceFrom(c.r)
		return
	}
	return c.ExtendedConn.ReadBuffer(buffer)
}

func (c *BufferedConn) ReadCached() *buf.Buffer { // call in sing/common/bufio.Copy
	if c.r != nil && c.r.Buffered() > 0 {
		length := c.r.Buffered()
		b, _ := c.r.Peek(length)
		// Independent managed buffer. Do not alias the bufio backing array:
		// pooling reuses that array after dropReader, and Copy writes leftover
		// before the next empty ReadCached. Caller (bufio.Copy) Releases after
		// the synchronous Write.
		cloned := buf.NewSize(length)
		copy(cloned.Extend(length), b)
		_, _ = c.r.Discard(length)
		return cloned
	}
	c.dropReader()
	return nil
}

func (c *BufferedConn) dropReader() {
	r := c.r
	c.r = nil
	if r != nil && c.owned {
		c.owned = false
		putBufioReader(r)
	}
}

func (c *BufferedConn) Upstream() any {
	return c.ExtendedConn
}

func (c *BufferedConn) ReaderReplaceable() bool {
	if c.r != nil && c.r.Buffered() > 0 {
		return false
	}
	return true
}

func (c *BufferedConn) WriterReplaceable() bool {
	return true
}
