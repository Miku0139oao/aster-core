package trojan

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"

	LC "github.com/Miku0139oao/aster-core/listener/config"

	"github.com/stretchr/testify/require"
)

type cleanupListenConfig struct {
	failAt    int
	failErr   error
	closeErr  error
	calls     int
	listeners []*cleanupListener
}

func (c *cleanupListenConfig) Listen(context.Context, string, string) (net.Listener, error) {
	c.calls++
	if c.calls == c.failAt {
		return nil, c.failErr
	}
	l := &cleanupListener{closed: make(chan struct{}), closeErr: c.closeErr}
	c.listeners = append(c.listeners, l)
	return l, nil
}

func (*cleanupListenConfig) ListenPacket(context.Context, string, string) (net.PacketConn, error) {
	return nil, errors.New("unexpected packet listener")
}

type cleanupListener struct {
	closed    chan struct{}
	closeOnce sync.Once
	closes    atomic.Int32
	closeErr  error
}

func (l *cleanupListener) Accept() (net.Conn, error) {
	<-l.closed
	return nil, net.ErrClosed
}

func (l *cleanupListener) Close() error {
	l.closes.Add(1)
	l.closeOnce.Do(func() { close(l.closed) })
	return l.closeErr
}

func (*cleanupListener) Addr() net.Addr { return &net.TCPAddr{} }

func TestNewClosesEarlierListenersWhenLaterBindFails(t *testing.T) {
	bindErr := errors.New("second bind failed")
	closeErr := errors.New("first close failed")
	lc := &cleanupListenConfig{failAt: 2, failErr: bindErr, closeErr: closeErr}

	listener, err := New(LC.TrojanServer{
		Listen:        "first,second",
		AllowInsecure: true,
	}, lc, nil)

	require.Nil(t, listener)
	require.ErrorIs(t, err, bindErr)
	require.ErrorIs(t, err, closeErr)
	require.Len(t, lc.listeners, 1)
	require.Equal(t, int32(1), lc.listeners[0].closes.Load())
}

func TestNewClosesListenerOnPostBindValidationFailure(t *testing.T) {
	lc := &cleanupListenConfig{}

	listener, err := New(LC.TrojanServer{Listen: "first"}, lc, nil)

	require.Nil(t, listener)
	require.ErrorContains(t, err, "disallow using Trojan")
	require.Len(t, lc.listeners, 1)
	require.Equal(t, int32(1), lc.listeners[0].closes.Load())
}

func TestListenerCloseIsIdempotent(t *testing.T) {
	lc := &cleanupListenConfig{}
	listener, err := New(LC.TrojanServer{
		Listen:        "first",
		AllowInsecure: true,
		WsPath:        "/ws",
	}, lc, nil)
	require.NoError(t, err)

	require.NoError(t, listener.Close())
	require.NoError(t, listener.Close())
	require.Equal(t, int32(1), lc.listeners[0].closes.Load())
}
