package transport

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type packetDialerStub struct {
	listenCount int
}

func (d *packetDialerStub) ListenPacket(net.Addr) (net.PacketConn, error) {
	d.listenCount++
	return newPacketConnStub(), nil
}

func (d *packetDialerStub) Context() context.Context {
	return context.Background()
}

func (d *packetDialerStub) RemoteAddr(string) (net.Addr, error) {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 443}, nil
}

type packetConnStub struct {
	closed chan struct{}
	once   sync.Once
}

func newPacketConnStub() *packetConnStub {
	return &packetConnStub{closed: make(chan struct{})}
}

func (c *packetConnStub) ReadFrom([]byte) (int, net.Addr, error) {
	<-c.closed
	return 0, nil, net.ErrClosed
}

func (c *packetConnStub) WriteTo(data []byte, _ net.Addr) (int, error) {
	return len(data), nil
}

func (c *packetConnStub) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func (c *packetConnStub) LocalAddr() net.Addr              { return &net.UDPAddr{} }
func (c *packetConnStub) SetDeadline(time.Time) error      { return nil }
func (c *packetConnStub) SetReadDeadline(time.Time) error  { return nil }
func (c *packetConnStub) SetWriteDeadline(time.Time) error { return nil }

func TestQUICPacketConnPortHoppingOpensOneInitialSocket(t *testing.T) {
	dialer := new(packetDialerStub)
	remote := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 443}

	conn, err := new(ClientTransport).quicPacketConn("udp", remote, "443", nil, time.Hour, dialer)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	require.Equal(t, 1, dialer.listenCount)
}
