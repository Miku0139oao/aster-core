package vless

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/Miku0139oao/aster-core/common/pool"
)

type PacketConn struct {
	net.Conn
	rAddr        net.Addr
	readMu       sync.Mutex
	writeMu      sync.Mutex
	writeHeader  [2]byte
	writeBuffers [2][]byte
	writeBufs    net.Buffers
}

func (c *PacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	if len(b) > int(^uint16(0)) {
		return 0, fmt.Errorf("VLESS packet exceeds maximum size: %d", len(b))
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	binary.BigEndian.PutUint16(c.writeHeader[:], uint16(len(b)))
	c.writeBuffers[0] = c.writeHeader[:]
	c.writeBuffers[1] = b
	c.writeBufs = c.writeBuffers[:]
	defer func() {
		c.writeBuffers[0] = nil
		c.writeBuffers[1] = nil
		c.writeBufs = nil
	}()
	written, err := c.writeBufs.WriteTo(c.Conn)
	payloadWritten := written - int64(len(c.writeHeader))
	if payloadWritten < 0 {
		payloadWritten = 0
	} else if payloadWritten > int64(len(b)) {
		payloadWritten = int64(len(b))
	}
	if err == nil && written != int64(len(c.writeHeader)+len(b)) {
		err = io.ErrShortWrite
	}
	if err != nil {
		_ = c.Conn.Close()
	}
	return int(payloadWritten), err
}

func (c *PacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()

	length, err := c.readPacketLength()
	if err != nil {
		return 0, nil, err
	}
	if len(b) < int(length) {
		if _, err = io.CopyN(io.Discard, c.Conn, int64(length)); err != nil {
			_ = c.Conn.Close()
			return 0, nil, err
		}
		return 0, nil, io.ErrShortBuffer
	}
	n, err := io.ReadFull(c.Conn, b[:length])
	if err != nil {
		_ = c.Conn.Close()
	}
	return n, c.rAddr, err
}

func (c *PacketConn) WaitReadFrom() (data []byte, put func(), addr net.Addr, err error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()

	length, err := c.readPacketLength()
	if err != nil {
		return
	}
	readBuf := pool.Get(int(length))
	put = func() {
		_ = pool.Put(readBuf)
	}
	n, err := io.ReadFull(c.Conn, readBuf)
	if err != nil {
		_ = c.Conn.Close()
		put()
		put = nil
		return
	}
	data = readBuf[:n]
	addr = c.rAddr
	return
}

func (c *PacketConn) readPacketLength() (uint16, error) {
	var header [2]byte
	n, err := io.ReadFull(c.Conn, header[:])
	if err != nil {
		if n > 0 {
			_ = c.Conn.Close()
		}
		return 0, err
	}
	return binary.BigEndian.Uint16(header[:]), nil
}
