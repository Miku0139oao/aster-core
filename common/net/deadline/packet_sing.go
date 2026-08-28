package deadline

import (
	"os"
	"runtime"
	"sync"

	"github.com/Miku0139oao/aster-core/common/net/packet"

	"github.com/metacubex/sing/common/buf"
	"github.com/metacubex/sing/common/bufio"
	M "github.com/metacubex/sing/common/metadata"
	N "github.com/metacubex/sing/common/network"
)

type SingPacketConn struct {
	*NetPacketConn
	singPacketConn
}

var _ packet.SingPacketConn = (*SingPacketConn)(nil)

func NewSingPacketConn(pc packet.SingPacketConn) packet.SingPacketConn {
	return NewNetPacketConn(pc).(packet.SingPacketConn)
}

type EnhanceSingPacketConn struct {
	*EnhancePacketConn
	singPacketConn
}

func NewEnhanceSingPacketConn(pc packet.EnhanceSingPacketConn) packet.EnhanceSingPacketConn {
	return NewNetPacketConn(pc).(packet.EnhanceSingPacketConn)
}

var _ packet.EnhanceSingPacketConn = (*EnhanceSingPacketConn)(nil)

type singReadResult struct {
	buffer      *buf.Buffer
	destination M.Socksaddr
	err         error
}

var singReadResultPool = sync.Pool{
	New: func() any { return new(singReadResult) },
}

func acquireSingReadResult() *singReadResult {
	return singReadResultPool.Get().(*singReadResult)
}

func releaseSingReadResult(result *singReadResult) {
	result.buffer = nil
	result.destination = M.Socksaddr{}
	result.err = nil
	singReadResultPool.Put(result)
}

type singPacketConn struct {
	netPacketConn  *NetPacketConn
	singPacketConn packet.SingPacketConn
}

func (c *singPacketConn) ReadPacket(buffer *buf.Buffer) (destination M.Socksaddr, err error) {
FOR:
	for {
		select {
		case result := <-c.netPacketConn.resultCh:
			if result != nil {
				if result, ok := result.(*singReadResult); ok {
					destination = result.destination
					err = result.err
					n, _ := buffer.Write(result.buffer.Bytes())
					result.buffer.Advance(n)
					if result.buffer.IsEmpty() {
						result.buffer.Release()
					}
					releaseSingReadResult(result)
					c.netPacketConn.resultCh <- nil // finish cache read
					return
				}
				c.netPacketConn.resultCh <- result // another type of read
				runtime.Gosched()                  // allowing other goroutines to run
				continue FOR
			} else {
				c.netPacketConn.resultCh <- nil
				break FOR
			}
		case <-c.netPacketConn.pipeDeadline.Wait():
			return M.Socksaddr{}, os.ErrDeadlineExceeded
		}
	}

	if c.netPacketConn.disablePipe.Load() {
		return c.singPacketConn.ReadPacket(buffer)
	} else if c.netPacketConn.deadline.Load().IsZero() {
		c.netPacketConn.inRead.Store(true)
		defer c.netPacketConn.inRead.Store(false)
		destination, err = c.singPacketConn.ReadPacket(buffer)
		return
	}

	<-c.netPacketConn.resultCh
	go c.pipeReadPacket(buffer.FreeLen())

	return c.ReadPacket(buffer)
}

func (c *singPacketConn) pipeReadPacket(pLen int) {
	buffer := buf.NewSize(pLen)
	destination, err := c.singPacketConn.ReadPacket(buffer)
	result := acquireSingReadResult()
	result.buffer = buffer
	result.destination = destination
	result.err = err
	c.netPacketConn.resultCh <- result
}

func (c *singPacketConn) WritePacket(buffer *buf.Buffer, destination M.Socksaddr) error {
	return c.singPacketConn.WritePacket(buffer, destination)
}

func (c *singPacketConn) CreateReadWaiter() (N.PacketReadWaiter, bool) {
	prw, isReadWaiter := bufio.CreatePacketReadWaiter(c.singPacketConn)
	if isReadWaiter {
		return &singPacketReadWaiter{
			netPacketConn:    c.netPacketConn,
			packetReadWaiter: prw,
		}, true
	}
	return nil, false
}

var _ N.PacketReadWaiter = (*singPacketReadWaiter)(nil)

type singPacketReadWaiter struct {
	netPacketConn    *NetPacketConn
	packetReadWaiter N.PacketReadWaiter
}

type singWaitReadResult singReadResult

var singWaitReadResultPool = sync.Pool{
	New: func() any { return new(singWaitReadResult) },
}

func acquireSingWaitReadResult() *singWaitReadResult {
	return singWaitReadResultPool.Get().(*singWaitReadResult)
}

func releaseSingWaitReadResult(result *singWaitReadResult) {
	result.buffer = nil
	result.destination = M.Socksaddr{}
	result.err = nil
	singWaitReadResultPool.Put(result)
}

func (c *singPacketReadWaiter) InitializeReadWaiter(options N.ReadWaitOptions) (needCopy bool) {
	return c.packetReadWaiter.InitializeReadWaiter(options)
}

func (c *singPacketReadWaiter) WaitReadPacket() (buffer *buf.Buffer, destination M.Socksaddr, err error) {
FOR:
	for {
		select {
		case result := <-c.netPacketConn.resultCh:
			if result != nil {
				if result, ok := result.(*singWaitReadResult); ok {
					buffer = result.buffer
					destination = result.destination
					err = result.err
					releaseSingWaitReadResult(result)
					c.netPacketConn.resultCh <- nil // finish cache read
					return
				}
				c.netPacketConn.resultCh <- result // another type of read
				runtime.Gosched()                  // allowing other goroutines to run
				continue FOR
			} else {
				c.netPacketConn.resultCh <- nil
				break FOR
			}
		case <-c.netPacketConn.pipeDeadline.Wait():
			return nil, M.Socksaddr{}, os.ErrDeadlineExceeded
		}
	}

	if c.netPacketConn.disablePipe.Load() {
		return c.packetReadWaiter.WaitReadPacket()
	} else if c.netPacketConn.deadline.Load().IsZero() {
		c.netPacketConn.inRead.Store(true)
		defer c.netPacketConn.inRead.Store(false)
		return c.packetReadWaiter.WaitReadPacket()
	}

	<-c.netPacketConn.resultCh
	go c.pipeWaitReadPacket()

	return c.WaitReadPacket()
}

func (c *singPacketReadWaiter) pipeWaitReadPacket() {
	buffer, destination, err := c.packetReadWaiter.WaitReadPacket()
	result := acquireSingWaitReadResult()
	result.buffer = buffer
	result.destination = destination
	result.err = err
	c.netPacketConn.resultCh <- result
}

func (c *singPacketReadWaiter) Upstream() any {
	return c.packetReadWaiter
}
