package shadowsocks

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	C "github.com/Miku0139oao/aster-core/constant"
	LC "github.com/Miku0139oao/aster-core/listener/config"

	"github.com/stretchr/testify/require"
)

func TestNewCleansPreviouslyOpenedLegacyListeners(t *testing.T) {
	lc := &legacyTestListenConfig{failPacketCall: 2}
	_, err := New(LC.ShadowsocksServer{
		Listen:   "first,second",
		Cipher:   "aes-128-cfb",
		Password: "password",
		Udp:      true,
	}, lc, nil)

	require.ErrorContains(t, err, "packet listen failed")
	require.Len(t, lc.packetConns, 1)
	require.Len(t, lc.listeners, 1)
	require.EqualValues(t, 1, lc.packetConns[0].closeCalls.Load())
	require.EqualValues(t, 1, lc.listeners[0].closeCalls.Load())
}

func TestLegacyListenerCloseStopsAcceptLoopAndIsIdempotent(t *testing.T) {
	lc := &legacyTestListenConfig{}
	listener, err := New(LC.ShadowsocksServer{
		Listen:   "first",
		Cipher:   "aes-128-cfb",
		Password: "password",
	}, lc, nil)
	require.NoError(t, err)
	require.Len(t, lc.listeners, 1)

	select {
	case <-lc.listeners[0].acceptStarted:
	case <-time.After(time.Second):
		t.Fatal("accept loop did not start")
	}
	require.NoError(t, listener.Close())
	require.NoError(t, listener.Close())
	require.EqualValues(t, 1, lc.listeners[0].closeCalls.Load())
	time.Sleep(10 * time.Millisecond)
	require.EqualValues(t, 1, lc.listeners[0].acceptCalls.Load())
}

type legacyTestListenConfig struct {
	mu             sync.Mutex
	packetCalls    int
	failPacketCall int
	listeners      []*legacyTestListener
	packetConns    []*legacyTestPacketConn
}

func (c *legacyTestListenConfig) Listen(_ context.Context, _, _ string) (net.Listener, error) {
	l := &legacyTestListener{closed: make(chan struct{}), acceptStarted: make(chan struct{})}
	c.mu.Lock()
	c.listeners = append(c.listeners, l)
	c.mu.Unlock()
	return l, nil
}

func (c *legacyTestListenConfig) ListenPacket(_ context.Context, _, _ string) (net.PacketConn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.packetCalls++
	if c.packetCalls == c.failPacketCall {
		return nil, errors.New("packet listen failed")
	}
	pc := &legacyTestPacketConn{closed: make(chan struct{})}
	c.packetConns = append(c.packetConns, pc)
	return pc, nil
}

type legacyTestListener struct {
	closed        chan struct{}
	acceptStarted chan struct{}
	acceptOnce    sync.Once
	closeOnce     sync.Once
	acceptCalls   atomic.Int32
	closeCalls    atomic.Int32
}

func (l *legacyTestListener) Accept() (net.Conn, error) {
	l.acceptCalls.Add(1)
	l.acceptOnce.Do(func() { close(l.acceptStarted) })
	<-l.closed
	return nil, net.ErrClosed
}

func (l *legacyTestListener) Close() error {
	l.closeOnce.Do(func() {
		l.closeCalls.Add(1)
		close(l.closed)
	})
	return nil
}

func (*legacyTestListener) Addr() net.Addr { return legacyTestAddr("listener") }

type legacyTestPacketConn struct {
	closed     chan struct{}
	closeOnce  sync.Once
	closeCalls atomic.Int32
}

func (c *legacyTestPacketConn) ReadFrom([]byte) (int, net.Addr, error) {
	<-c.closed
	return 0, nil, net.ErrClosed
}

func (*legacyTestPacketConn) WriteTo(p []byte, _ net.Addr) (int, error) { return len(p), nil }
func (c *legacyTestPacketConn) Close() error {
	c.closeOnce.Do(func() {
		c.closeCalls.Add(1)
		close(c.closed)
	})
	return nil
}
func (*legacyTestPacketConn) LocalAddr() net.Addr              { return legacyTestAddr("packet") }
func (*legacyTestPacketConn) SetDeadline(time.Time) error      { return nil }
func (*legacyTestPacketConn) SetReadDeadline(time.Time) error  { return nil }
func (*legacyTestPacketConn) SetWriteDeadline(time.Time) error { return nil }

type legacyTestAddr string

func (a legacyTestAddr) Network() string { return string(a) }
func (a legacyTestAddr) String() string  { return string(a) }

var _ C.InboundListenConfig = (*legacyTestListenConfig)(nil)
