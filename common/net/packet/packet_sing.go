package packet

import (
	"net"
	"sync"
	"sync/atomic"

	"github.com/metacubex/sing/common/buf"
	"github.com/metacubex/sing/common/bufio"
	M "github.com/metacubex/sing/common/metadata"
	N "github.com/metacubex/sing/common/network"
)

type SingPacketConn = N.NetPacketConn

type EnhanceSingPacketConn interface {
	SingPacketConn
	EnhancePacketConn
}

type enhanceSingPacketConn struct {
	SingPacketConn
	packetReadWaiter N.PacketReadWaiter
}

type bufferReleaser struct {
	buff  *buf.Buffer
	fn    func()
	armed atomic.Bool
}

func (r *bufferReleaser) release() {
	if !r.armed.CompareAndSwap(true, false) {
		return
	}
	if r.buff != nil {
		r.buff.Release()
		r.buff = nil
	}
	bufferReleaserPool.Put(r)
}

var bufferReleaserPool = sync.Pool{
	New: func() any { return new(bufferReleaser) },
}

func acquireBufferRelease(buff *buf.Buffer) func() {
	r := bufferReleaserPool.Get().(*bufferReleaser)
	r.buff = buff
	r.armed.Store(true)
	if r.fn == nil {
		r.fn = r.release
	}
	return r.fn
}

func (c *enhanceSingPacketConn) WaitReadFrom() (data []byte, put func(), addr net.Addr, err error) {
	var buff *buf.Buffer
	var dest M.Socksaddr
	rwOptions := N.ReadWaitOptions{}
	if c.packetReadWaiter != nil {
		buff, dest, err = c.packetReadWaiter.WaitReadPacket()
	} else {
		buff = rwOptions.NewPacketBuffer()
		dest, err = c.SingPacketConn.ReadPacket(buff)
		if buff != nil {
			rwOptions.PostReturn(buff)
		}
	}
	if dest.IsFqdn() {
		addr = dest
	} else {
		addr = dest.UDPAddr()
	}
	if err != nil {
		if buff != nil {
			buff.Release()
		}
		return
	}
	if buff == nil {
		return
	}
	if buff.IsEmpty() {
		buff.Release()
		return
	}
	data = buff.Bytes()
	put = acquireBufferRelease(buff)
	return
}

func (c *enhanceSingPacketConn) Upstream() any {
	return c.SingPacketConn
}

func (c *enhanceSingPacketConn) WriterReplaceable() bool {
	return true
}

func (c *enhanceSingPacketConn) ReaderReplaceable() bool {
	return true
}

func newEnhanceSingPacketConn(conn SingPacketConn) *enhanceSingPacketConn {
	epc := &enhanceSingPacketConn{SingPacketConn: conn}
	if readWaiter, isReadWaiter := bufio.CreatePacketReadWaiter(conn); isReadWaiter {
		epc.packetReadWaiter = readWaiter
		// Initialize once. Recreating the syscall read closure on every packet
		// was a per-packet allocation and could reset waiter state mid-read.
		readWaiter.InitializeReadWaiter(N.ReadWaitOptions{})
	}
	return epc
}
