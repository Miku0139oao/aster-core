package deadline

import (
	"net"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/Miku0139oao/aster-core/common/atomic"
	"github.com/Miku0139oao/aster-core/common/net/packet"
	"github.com/Miku0139oao/aster-core/common/pool"
)

type readResult struct {
	data   []byte
	addr   net.Addr
	err    error
	pooled []byte
}

var readResultPool = sync.Pool{
	New: func() any { return new(readResult) },
}

func acquireReadResult() *readResult {
	return readResultPool.Get().(*readResult)
}

func releaseReadResult(result *readResult) {
	if result.pooled != nil {
		_ = pool.Put(result.pooled)
	}
	result.data = nil
	result.addr = nil
	result.err = nil
	result.pooled = nil
	readResultPool.Put(result)
}

type NetPacketConn struct {
	net.PacketConn
	deadline     atomic.TypedValue[time.Time]
	pipeDeadline PipeDeadline
	disablePipe  atomic.Bool
	inRead       atomic.Bool
	resultCh     chan any
}

func NewNetPacketConn(pc net.PacketConn) net.PacketConn {
	npc := &NetPacketConn{
		PacketConn:   pc,
		pipeDeadline: MakePipeDeadline(),
		resultCh:     make(chan any, 1),
	}
	npc.resultCh <- nil
	if enhancePC, isEnhance := pc.(packet.EnhancePacketConn); isEnhance {
		epc := &EnhancePacketConn{
			NetPacketConn: npc,
			enhancePacketConn: enhancePacketConn{
				netPacketConn:     npc,
				enhancePacketConn: enhancePC,
			},
		}
		if singPC, isSingPC := pc.(packet.SingPacketConn); isSingPC {
			return &EnhanceSingPacketConn{
				EnhancePacketConn: epc,
				singPacketConn: singPacketConn{
					netPacketConn:  npc,
					singPacketConn: singPC,
				},
			}
		}
		return epc
	}
	if singPC, isSingPC := pc.(packet.SingPacketConn); isSingPC {
		return &SingPacketConn{
			NetPacketConn: npc,
			singPacketConn: singPacketConn{
				netPacketConn:  npc,
				singPacketConn: singPC,
			},
		}
	}
	return npc
}

func (c *NetPacketConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
FOR:
	for {
		select {
		case result := <-c.resultCh:
			if result != nil {
				if result, ok := result.(*readResult); ok {
					n = copy(p, result.data)
					addr = result.addr
					err = result.err
					releaseReadResult(result)
					c.resultCh <- nil // finish cache read
					return
				}
				c.resultCh <- result // another type of read
				runtime.Gosched()    // allowing other goroutines to run
				continue FOR
			} else {
				c.resultCh <- nil
				break FOR
			}
		case <-c.pipeDeadline.Wait():
			return 0, nil, os.ErrDeadlineExceeded
		}
	}

	if c.disablePipe.Load() {
		return c.PacketConn.ReadFrom(p)
	} else if c.deadline.Load().IsZero() {
		c.inRead.Store(true)
		defer c.inRead.Store(false)
		n, addr, err = c.PacketConn.ReadFrom(p)
		return
	}

	<-c.resultCh
	go c.pipeReadFrom(len(p))

	return c.ReadFrom(p)
}

func (c *NetPacketConn) pipeReadFrom(size int) {
	buffer := pool.Get(size)
	n, addr, err := c.PacketConn.ReadFrom(buffer)
	result := acquireReadResult()
	result.addr = addr
	result.err = err
	if n > 0 {
		result.data = buffer[:n]
		result.pooled = buffer
	} else {
		_ = pool.Put(buffer)
		result.data = nil
		result.pooled = nil
	}
	c.resultCh <- result
}

func (c *NetPacketConn) SetReadDeadline(t time.Time) error {
	if c.disablePipe.Load() {
		return c.PacketConn.SetReadDeadline(t)
	} else if c.inRead.Load() {
		c.disablePipe.Store(true)
		return c.PacketConn.SetReadDeadline(t)
	}
	c.deadline.Store(t)
	c.pipeDeadline.Set(t)
	return nil
}

func (c *NetPacketConn) ReaderReplaceable() bool {
	select {
	case result := <-c.resultCh:
		c.resultCh <- result
		if result != nil {
			return false // cache reading
		} else {
			break
		}
	default:
		return false // pipe reading
	}
	return c.disablePipe.Load() || c.deadline.Load().IsZero()
}

func (c *NetPacketConn) WriterReplaceable() bool {
	return true
}

func (c *NetPacketConn) Upstream() any {
	return c.PacketConn
}

func (c *NetPacketConn) NeedAdditionalReadDeadline() bool {
	return false
}
