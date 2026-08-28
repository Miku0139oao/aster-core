package tuic

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	C "github.com/Miku0139oao/aster-core/constant"
)

type poolClientLifecycleProbe struct {
	closed      atomic.Int64
	lastVisited atomic.Int64
}

func (*poolClientLifecycleProbe) DialContext(context.Context, *C.Metadata) (net.Conn, error) {
	return nil, errors.New("not used")
}

func (*poolClientLifecycleProbe) ListenPacket(context.Context, *C.Metadata) (net.PacketConn, error) {
	return nil, errors.New("not used")
}
func (*poolClientLifecycleProbe) OpenStreams() int64 { return 0 }
func (c *poolClientLifecycleProbe) LastVisited() time.Time {
	return time.Unix(0, c.lastVisited.Load())
}

func (c *poolClientLifecycleProbe) SetLastVisited(value time.Time) {
	c.lastVisited.Store(value.UnixNano())
}
func (c *poolClientLifecycleProbe) Close() { c.closed.Add(1) }

func TestPoolClientCloseDrainsClientsAndRejectsReuse(t *testing.T) {
	pool := &PoolClient{}
	tcpClient := &poolClientLifecycleProbe{}
	udpClient := &poolClientLifecycleProbe{}
	pool.tcpClients.PushFront(tcpClient)
	pool.udpClients.PushFront(udpClient)

	pool.Close()
	pool.Close()
	if tcpClient.closed.Load() != 1 || udpClient.closed.Load() != 1 {
		t.Fatalf("unexpected close counts: tcp=%d udp=%d", tcpClient.closed.Load(), udpClient.closed.Load())
	}
	if pool.tcpClients.Len() != 0 || pool.udpClients.Len() != 0 {
		t.Fatal("closed pool retained clients")
	}
	if _, err := pool.getClient(false); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("closed pool get error = %v", err)
	}
	if _, err := pool.DialContext(context.Background(), &C.Metadata{}); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("closed pool dial error = %v", err)
	}
}
