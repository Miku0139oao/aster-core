package kcptun

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// countingPacketConn is a minimal net.PacketConn whose Close is observable.
type countingPacketConn struct {
	net.PacketConn
	closed atomic.Bool
}

func (c *countingPacketConn) Close() error {
	c.closed.Store(true)
	return c.PacketConn.Close()
}

// TestClientCreateConnClosesSocketOnVerifyConfigError covers the error path of
// createConn where the smux configuration is rejected: the KCP session (and the
// packet conn it owns) must be closed instead of leaked.
func TestClientCreateConnClosesSocketOnVerifyConfigError(t *testing.T) {
	// SmuxVer is clamped only when it exceeds the maximum, so a negative value
	// survives FillDefaults and makes smux.VerifyConfig fail.
	client := NewClient(Config{SmuxVer: -1})

	// A real UDP socket is used so kcp.NewConn4 gets a working PacketConn.
	raw, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen packet: %v", err)
	}
	conn := &countingPacketConn{PacketConn: raw}
	defer conn.Close()

	remote := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = client.createConn(ctx, func(context.Context) (net.PacketConn, net.Addr, error) {
		return conn, remote, nil
	})
	if err == nil {
		t.Fatal("expected smux.VerifyConfig to reject the invalid version")
	}
	if !conn.closed.Load() {
		t.Fatal("packet conn was leaked after the smux config error")
	}
}
