package packet

import (
	"net"
	"sync"
	"sync/atomic"

	"github.com/Miku0139oao/aster-core/common/pool"
)

type WaitReadFrom interface {
	WaitReadFrom() (data []byte, put func(), addr net.Addr, err error)
}

type EnhancePacketConn interface {
	net.PacketConn
	WaitReadFrom
}

func NewEnhancePacketConn(pc net.PacketConn) EnhancePacketConn {
	if udpConn, isUDPConn := pc.(*net.UDPConn); isUDPConn {
		return &enhanceUDPConn{UDPConn: udpConn}
	}
	if enhancePC, isEnhancePC := pc.(EnhancePacketConn); isEnhancePC {
		return enhancePC
	}
	if singPC, isSingPC := pc.(SingPacketConn); isSingPC {
		return newEnhanceSingPacketConn(singPC)
	}
	return &enhancePacketConn{PacketConn: pc}
}

type enhancePacketConn struct {
	net.PacketConn
}

func (c *enhancePacketConn) WaitReadFrom() (data []byte, put func(), addr net.Addr, err error) {
	return waitReadFrom(c.PacketConn)
}

func (c *enhancePacketConn) Upstream() any {
	return c.PacketConn
}

func (c *enhancePacketConn) WriterReplaceable() bool {
	return true
}

func (c *enhancePacketConn) ReaderReplaceable() bool {
	return true
}

func (c *enhanceUDPConn) Upstream() any {
	return c.UDPConn
}

func (c *enhanceUDPConn) WriterReplaceable() bool {
	return true
}

func (c *enhanceUDPConn) ReaderReplaceable() bool {
	return true
}

type bufPutter struct {
	buf   []byte
	fn    func()
	armed atomic.Bool
}

func (p *bufPutter) release() {
	if !p.armed.CompareAndSwap(true, false) {
		return
	}
	buf := p.buf
	p.buf = nil
	if buf != nil {
		_ = pool.Put(buf)
	}
	bufPutterPool.Put(p)
}

var bufPutterPool = sync.Pool{
	New: func() any { return new(bufPutter) },
}

func acquireBufPut(buf []byte) func() {
	p := bufPutterPool.Get().(*bufPutter)
	p.buf = buf
	p.armed.Store(true)
	if p.fn == nil {
		p.fn = p.release
	}
	return p.fn
}

type udpWaitSlot struct {
	buf     []byte
	data    []byte
	addr    net.Addr
	readErr error
	readFn  func(uintptr) bool
	putFn   func()
	hasData bool
	armed   atomic.Bool
}

func (s *udpWaitSlot) dropBuf() {
	if s.buf != nil {
		_ = pool.Put(s.buf)
		s.buf = nil
	}
	s.data = nil
}

func (s *udpWaitSlot) reset() {
	s.dropBuf()
	s.addr = nil
	s.readErr = nil
	s.hasData = false
}

func (s *udpWaitSlot) release() {
	if !s.armed.CompareAndSwap(true, false) {
		return
	}
	s.dropBuf()
	s.addr = nil
	s.readErr = nil
	s.hasData = false
	udpWaitSlotPool.Put(s)
}

var udpWaitSlotPool = sync.Pool{
	New: func() any { return new(udpWaitSlot) },
}

func acquireUDPWaitSlot() *udpWaitSlot {
	s := udpWaitSlotPool.Get().(*udpWaitSlot)
	s.reset()
	s.armed.Store(true)
	if s.putFn == nil {
		s.putFn = s.release
		s.readFn = s.rawRead
	}
	return s
}

func (c *enhanceUDPConn) WaitReadFrom() (data []byte, put func(), addr net.Addr, err error) {
	if c.rawConn == nil {
		c.rawConn, _ = c.UDPConn.SyscallConn()
	}
	s := acquireUDPWaitSlot()
	err = c.rawConn.Read(s.readFn)
	if err != nil {
		s.release()
		return
	}
	if s.readErr != nil {
		err = s.readErr
		s.release()
		return
	}
	data, addr = s.data, s.addr
	if s.buf != nil {
		put = s.putFn
	} else {
		s.release()
	}
	return
}

func waitReadFrom(pc net.PacketConn) (data []byte, put func(), addr net.Addr, err error) {
	readBuf := pool.Get(pool.UDPBufferSize)
	var readN int
	readN, addr, err = pc.ReadFrom(readBuf)
	if readN > 0 {
		data = readBuf[:readN]
		put = acquireBufPut(readBuf)
	} else {
		_ = pool.Put(readBuf)
	}
	return
}
