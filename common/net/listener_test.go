package net

import (
	"context"
	"errors"
	stdnet "net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeListener struct {
	connections chan stdnet.Conn
	closed      chan struct{}
	closeOnce   sync.Once
}

func newFakeListener() *fakeListener {
	return &fakeListener{
		connections: make(chan stdnet.Conn, 1),
		closed:      make(chan struct{}),
	}
}

func (l *fakeListener) Accept() (stdnet.Conn, error) {
	select {
	case conn := <-l.connections:
		return conn, nil
	case <-l.closed:
		return nil, stdnet.ErrClosed
	}
}

func (l *fakeListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	return nil
}

func (l *fakeListener) Addr() stdnet.Addr { return fakeAddr("listener") }

type fakeAddr string

func (a fakeAddr) Network() string { return string(a) }
func (a fakeAddr) String() string  { return string(a) }

func TestHandleContextListenerCloseClosesInFlightConnection(t *testing.T) {
	base := newFakeListener()
	started := make(chan struct{})
	handlerDone := make(chan struct{})
	listener := NewHandleContextListener(context.Background(), base, func(_ context.Context, conn stdnet.Conn) (stdnet.Conn, error) {
		close(started)
		defer close(handlerDone)
		_, err := conn.Read(make([]byte, 1))
		return nil, err
	}, nil)

	server, client := stdnet.Pipe()
	defer client.Close()
	base.connections <- server
	acceptResult := make(chan error, 1)
	go func() {
		_, err := listener.Accept()
		acceptResult <- err
	}()

	requireClosed(t, started, "handler did not start")
	require.NoError(t, listener.Close())
	requireClosed(t, handlerDone, "handler was not unblocked by close")
	require.ErrorIs(t, <-acceptResult, stdnet.ErrClosed)
	assertPeerClosed(t, client)
}

func TestHandleContextListenerClosesSuccessfulResultAfterCancellation(t *testing.T) {
	base := newFakeListener()
	started := make(chan struct{})
	release := make(chan struct{})
	resultServer, resultClient := stdnet.Pipe()
	defer resultClient.Close()
	listener := NewHandleContextListener(context.Background(), base, func(_ context.Context, _ stdnet.Conn) (stdnet.Conn, error) {
		close(started)
		<-release
		return resultServer, nil
	}, nil)

	server, client := stdnet.Pipe()
	defer client.Close()
	base.connections <- server
	acceptResult := make(chan error, 1)
	go func() {
		_, err := listener.Accept()
		acceptResult <- err
	}()

	requireClosed(t, started, "handler did not start")
	require.NoError(t, listener.Close())
	close(release)
	require.ErrorIs(t, <-acceptResult, stdnet.ErrClosed)
	assertPeerClosed(t, resultClient)
}

func TestHandleContextListenerCloseUnblocksAcceptWhenHandlerIgnoresCancellation(t *testing.T) {
	base := newFakeListener()
	started := make(chan struct{})
	release := make(chan struct{})
	listener := NewHandleContextListener(context.Background(), base, func(_ context.Context, _ stdnet.Conn) (stdnet.Conn, error) {
		close(started)
		<-release
		return nil, errors.New("released")
	}, nil)

	server, client := stdnet.Pipe()
	defer client.Close()
	base.connections <- server
	acceptResult := make(chan error, 1)
	go func() {
		_, err := listener.Accept()
		acceptResult <- err
	}()

	requireClosed(t, started, "handler did not start")
	require.NoError(t, listener.Close())
	select {
	case err := <-acceptResult:
		require.ErrorIs(t, err, stdnet.ErrClosed)
	case <-time.After(time.Second):
		t.Fatal("accept was not unblocked by close")
	}
	close(release)
}

func TestHandleContextListenerAcceptsNonComparableConnection(t *testing.T) {
	base := newFakeListener()
	listener := NewHandleContextListener(context.Background(), base, func(_ context.Context, conn stdnet.Conn) (stdnet.Conn, error) {
		return conn, nil
	}, nil)
	base.connections <- nonComparableConn{state: []byte{1}}

	conn, err := listener.Accept()
	require.NoError(t, err)
	require.NotNil(t, conn)
	require.NoError(t, listener.Close())
}

type nonComparableConn struct {
	state []byte
}

func (nonComparableConn) Read([]byte) (int, error)         { return 0, errors.New("not implemented") }
func (nonComparableConn) Write(p []byte) (int, error)      { return len(p), nil }
func (nonComparableConn) Close() error                     { return nil }
func (nonComparableConn) LocalAddr() stdnet.Addr           { return fakeAddr("local") }
func (nonComparableConn) RemoteAddr() stdnet.Addr          { return fakeAddr("remote") }
func (nonComparableConn) SetDeadline(time.Time) error      { return nil }
func (nonComparableConn) SetReadDeadline(time.Time) error  { return nil }
func (nonComparableConn) SetWriteDeadline(time.Time) error { return nil }

func TestConnectionTrackingListenerRemovesAndClosesAcceptedConnections(t *testing.T) {
	base := newFakeListener()
	listener := NewConnectionTrackingListener(base)
	server, client := stdnet.Pipe()
	defer client.Close()
	base.connections <- server

	conn, err := listener.Accept()
	require.NoError(t, err)
	require.Len(t, listener.conns, 1)
	require.NoError(t, conn.Close())
	require.Empty(t, listener.conns)

	server, client2 := stdnet.Pipe()
	defer client2.Close()
	base.connections <- server
	_, err = listener.Accept()
	require.NoError(t, err)
	listener.CloseConnections()
	require.Empty(t, listener.conns)
	assertPeerClosed(t, client2)
}

func TestHandleContextListenerReleasesSuccessfulTrackedConnection(t *testing.T) {
	base := newFakeListener()
	tracking := NewConnectionTrackingListener(base)
	listener := NewHandleContextListener(context.Background(), tracking, func(_ context.Context, conn stdnet.Conn) (stdnet.Conn, error) {
		return conn, nil
	}, nil)
	server, client := stdnet.Pipe()
	defer client.Close()
	base.connections <- server

	conn, err := listener.Accept()
	require.NoError(t, err)
	require.Empty(t, tracking.conns)

	tracking.CloseConnections()
	require.NoError(t, conn.SetDeadline(time.Now().Add(time.Second)))
	writeDone := make(chan error, 1)
	go func() {
		_, err := client.Write([]byte{1})
		writeDone <- err
	}()
	var value [1]byte
	_, err = conn.Read(value[:])
	require.NoError(t, err)
	require.NoError(t, <-writeDone)
	require.Equal(t, byte(1), value[0])
	require.NoError(t, conn.Close())
	require.NoError(t, listener.Close())
}

func TestConnectionTrackingListenerClosesAcceptThatLosesCloseRace(t *testing.T) {
	server, client := stdnet.Pipe()
	defer client.Close()
	base := &lateAcceptListener{
		conn:      server,
		accepting: make(chan struct{}),
		release:   make(chan struct{}),
	}
	listener := NewConnectionTrackingListener(base)
	result := make(chan error, 1)
	go func() {
		_, err := listener.Accept()
		result <- err
	}()

	requireClosed(t, base.accepting, "underlying accept did not start")
	require.NoError(t, listener.Close())
	close(base.release)
	require.ErrorIs(t, <-result, stdnet.ErrClosed)
	assertPeerClosed(t, client)
}

type lateAcceptListener struct {
	conn      stdnet.Conn
	accepting chan struct{}
	release   chan struct{}
}

func (l *lateAcceptListener) Accept() (stdnet.Conn, error) {
	close(l.accepting)
	<-l.release
	return l.conn, nil
}

func (l *lateAcceptListener) Close() error      { return nil }
func (l *lateAcceptListener) Addr() stdnet.Addr { return fakeAddr("late-listener") }

func requireClosed(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal(message)
	}
}

func assertPeerClosed(t *testing.T, conn stdnet.Conn) {
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
		t.Fatal("peer connection was not closed")
	}
}
