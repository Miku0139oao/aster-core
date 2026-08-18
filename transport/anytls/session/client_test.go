package session

import (
	"context"
	"io"
	"math"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type closeTrackingConn struct {
	closeOnce sync.Once
	closed    chan struct{}
}

func newCloseTrackingConn() *closeTrackingConn {
	return &closeTrackingConn{closed: make(chan struct{})}
}

func (c *closeTrackingConn) Read([]byte) (int, error) {
	<-c.closed
	return 0, io.ErrClosedPipe
}

func (c *closeTrackingConn) Write(p []byte) (int, error) {
	return len(p), nil
}

func (c *closeTrackingConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func (c *closeTrackingConn) LocalAddr() net.Addr              { return nil }
func (c *closeTrackingConn) RemoteAddr() net.Addr             { return nil }
func (c *closeTrackingConn) SetDeadline(time.Time) error      { return nil }
func (c *closeTrackingConn) SetReadDeadline(time.Time) error  { return nil }
func (c *closeTrackingConn) SetWriteDeadline(time.Time) error { return nil }

func TestRecycleSessionDoesNotReinsertClosedSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := NewClient(ctx, nil, nil, "", 0, 0, 0, false)
	defer client.Close()

	session := NewClientSession(discardConn{}, nil, "")
	session.seq = 1
	session.Close()

	client.recycleSession(session)
	require.True(t, client.idleSession.IsEmpty())
}

func TestGetIdleSessionSkipsClosedSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := NewClient(ctx, nil, nil, "", 0, 0, 0, false)
	defer client.Close()

	session := NewClientSession(discardConn{}, nil, "")
	session.seq = 1
	session.Close()

	client.idleSessionLock.Lock()
	client.idleSession.Insert(math.MaxUint64-session.seq, session)
	client.idleSessionLock.Unlock()

	require.Nil(t, client.getIdleSession())
	require.True(t, client.idleSession.IsEmpty())
}

func TestCreateSessionAbortsAfterClientClose(t *testing.T) {
	dialStarted := make(chan struct{})
	releaseDial := make(chan struct{})
	underlying := newCloseTrackingConn()
	client := NewClient(context.Background(), func(context.Context) (net.Conn, error) {
		close(dialStarted)
		<-releaseDial
		return underlying, nil
	}, nil, "", 0, 0, 0, true)

	result := make(chan struct {
		session *Session
		err     error
	}, 1)
	go func() {
		session, err := client.createSession(context.Background())
		result <- struct {
			session *Session
			err     error
		}{session, err}
	}()

	<-dialStarted
	require.NoError(t, client.Close())
	close(releaseDial)

	select {
	case got := <-result:
		require.Nil(t, got.session)
		require.ErrorIs(t, got.err, io.ErrClosedPipe)
	case <-time.After(time.Second):
		t.Fatal("createSession did not observe the closed client")
	}

	select {
	case <-underlying.closed:
	case <-time.After(time.Second):
		t.Fatal("underlying connection was not closed")
	}
}
