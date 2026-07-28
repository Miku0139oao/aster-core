package sing_vless

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	N "github.com/Miku0139oao/aster-core/common/net"

	"github.com/stretchr/testify/require"
)

type vlessFakeListener struct {
	connections chan net.Conn
	closed      chan struct{}
	closeOnce   sync.Once
}

func newVLESSFakeListener() *vlessFakeListener {
	return &vlessFakeListener{
		connections: make(chan net.Conn, 1),
		closed:      make(chan struct{}),
	}
}

func (l *vlessFakeListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.connections:
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *vlessFakeListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	return nil
}

func (l *vlessFakeListener) Addr() net.Addr { return vlessFakeAddr("listener") }

type vlessFakeAddr string

func (a vlessFakeAddr) Network() string { return string(a) }
func (a vlessFakeAddr) String() string  { return string(a) }

func TestListenerRevokesConnectionsBlockedInTransportHandshake(t *testing.T) {
	tests := map[string]func(*Listener) error{
		"update users":   func(listener *Listener) error { return listener.UpdateUsers(nil) },
		"close listener": func(listener *Listener) error { return listener.Close() },
	}

	for name, revoke := range tests {
		t.Run(name, func(t *testing.T) {
			base := newVLESSFakeListener()
			transport := N.NewConnectionTrackingListener(base)
			started := make(chan struct{})
			handlerDone := make(chan struct{})
			wrapped := N.NewHandleContextListener(context.Background(), transport, func(_ context.Context, conn net.Conn) (net.Conn, error) {
				close(started)
				defer close(handlerDone)
				_, err := conn.Read(make([]byte, 1))
				return nil, err
			}, nil)
			listener := &Listener{
				service:    NewService[string](nil),
				listeners:  []net.Listener{wrapped},
				transports: []*N.ConnectionTrackingListener{transport},
			}

			server, client := net.Pipe()
			defer client.Close()
			base.connections <- server
			acceptResult := make(chan error, 1)
			go func() {
				_, err := wrapped.Accept()
				acceptResult <- err
			}()

			waitVLESSLifecycle(t, started, "transport handshake did not start")
			require.NoError(t, revoke(listener))
			waitVLESSLifecycle(t, handlerDone, "transport handshake was not terminated")
			assertVLESSPeerClosed(t, client)
			require.NoError(t, listener.Close())
			require.ErrorIs(t, <-acceptResult, net.ErrClosed)
		})
	}
}

func TestListenerUserUpdatePreservesHTTPPhysicalConnectionAfterTransportHandoff(t *testing.T) {
	base := newVLESSFakeListener()
	transport := N.NewConnectionTrackingListener(base)
	wrapped := N.NewHandleContextListener(context.Background(), transport, func(_ context.Context, conn net.Conn) (net.Conn, error) {
		return conn, nil
	}, nil)
	listener := &Listener{
		service:    NewService[string](nil),
		listeners:  []net.Listener{wrapped},
		transports: []*N.ConnectionTrackingListener{transport},
	}
	server, client := net.Pipe()
	defer client.Close()
	base.connections <- server
	accepted, err := wrapped.Accept()
	require.NoError(t, err)
	defer accepted.Close()

	require.NoError(t, listener.UpdateUsers(nil))
	require.NoError(t, accepted.SetDeadline(time.Now().Add(time.Second)))
	writeDone := make(chan error, 1)
	go func() {
		_, err := client.Write([]byte{1})
		writeDone <- err
	}()
	var value [1]byte
	_, err = accepted.Read(value[:])
	require.NoError(t, err)
	require.NoError(t, <-writeDone)
	require.Equal(t, byte(1), value[0])
	require.NoError(t, listener.Close())
}

func waitVLESSLifecycle(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal(message)
	}
}

func assertVLESSPeerClosed(t *testing.T, conn net.Conn) {
	t.Helper()
	result := make(chan error, 1)
	go func() {
		_, err := conn.Read(make([]byte, 1))
		result <- err
	}()
	select {
	case err := <-result:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("physical connection was not closed")
	}
}
